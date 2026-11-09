package store

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeleteCancelledJobRequiresCompletedCleanup(t *testing.T) {
	for _, state := range []string{"receiving", "queued", "processing", "succeeded", "failed"} {
		t.Run(state, func(t *testing.T) {
			ctx := testContext()
			s := openTestStore(t)
			if err := s.CreateJob(ctx, UploadJob{ID: "job", FileName: "file", SourcePath: "source", OutputPath: "output"}); err != nil {
				t.Fatal(err)
			}
			if err := s.ReserveJob(ctx, "job", 10, 100); err != nil {
				t.Fatal(err)
			}
			if state != "receiving" {
				if err := s.SetJobState(ctx, "job", "receiving", state, "已取消"); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.DeleteCancelledJob(ctx, "job"); err != nil {
				t.Fatal(err)
			}
			jobs, err := s.Jobs(ctx)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("deleted before cleanup %#v %v", jobs, err)
			}
			if err = s.SetJobReservation(ctx, "job", 0); err != nil {
				t.Fatal(err)
			}
			if err = s.DeleteCancelledJob(ctx, "job"); err != nil {
				t.Fatal(err)
			}
			jobs, err = s.Jobs(ctx)
			want := 1
			if state == "failed" {
				want = 0
			}
			if err != nil || len(jobs) != want {
				t.Fatalf("jobs %#v want %d err %v", jobs, want, err)
			}
		})
	}
}

func TestConcurrentReservationsNeverExceedQuota(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	var accepted atomic.Int64
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		if err := s.CreateJob(ctx, UploadJob{ID: id, FileName: id, SourcePath: id, OutputPath: id + ".mp3"}); err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := s.ReserveJob(ctx, id, 11, 100)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrQuotaExceeded) {
				t.Errorf("reservation: %v", err)
			}
		}()
	}
	workers.Wait()
	used, err := s.Usage(ctx)
	if err != nil || accepted.Load() != 9 || used != 99 {
		t.Fatalf("accepted=%d usage=%d err=%v", accepted.Load(), used, err)
	}
}

func TestUploadBatchConcurrencyAndTailReservation(t *testing.T) {
	ctx := testContext()
	s := openTestStore(t)
	var total atomic.Int64
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		if err := s.CreateJob(ctx, UploadJob{ID: id, FileName: id, SourcePath: id, OutputPath: id + ".mp3"}); err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			grant, err := s.ReserveUploadBatch(ctx, id, 11, 40, 100)
			if err == nil {
				total.Add(grant)
			} else if !errors.Is(err, ErrQuotaExceeded) {
				t.Errorf("reserve: %v", err)
			}
		}()
	}
	workers.Wait()
	used, err := s.Usage(ctx)
	if err != nil || used != 100 || total.Load() != 100 {
		t.Fatalf("used=%d granted=%d err=%v", used, total.Load(), err)
	}
}

func TestQueueUploadSettlesHeadroomAtomicallyAndPersists(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "batch.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	job := UploadJob{ID: "batch", FileName: "file", SourcePath: "source", OutputPath: "output"}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	// Less than the preferred batch remains: grant enough for this file.
	grant, err := s.ReserveUploadBatch(ctx, job.ID, 3, 100, 10)
	if err != nil || grant != 10 {
		t.Fatalf("grant=%d err=%v", grant, err)
	}
	if err = s.QueueUpload(ctx, job.ID, 11); err == nil {
		t.Fatal("queued more than reserved")
	}
	jobs, _ := s.Jobs(ctx)
	if jobs[0].State != "receiving" || jobs[0].ReservedBytes != 10 {
		t.Fatalf("failed settlement changed state: %#v", jobs)
	}
	if err = s.QueueUpload(ctx, job.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReserveUploadBatch(ctx, job.ID, 1, 4, 10); err == nil {
		t.Fatal("modified queued job reservation")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(ctx)
	if jobs[0].State != "queued" || jobs[0].ReservedBytes != 3 {
		t.Fatalf("restart lost settlement: %#v", jobs)
	}
}

func TestJobActivationIsAtomicAndSurvivesRestart(t *testing.T) {
	ctx := testContext()
	path := filepath.Join(t.TempDir(), "jobs.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	job := UploadJob{ID: "job", FileName: "song.wav", SourcePath: "source", OutputPath: "output"}
	if err = s.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, job.ID, 30, 100); err != nil {
		t.Fatal(err)
	}
	for _, transition := range [][2]string{{"receiving", "queued"}, {"queued", "processing"}} {
		if err = s.SetJobState(ctx, job.ID, transition[0], transition[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	candidate := NewTrack{ID: job.ID, Title: "song", OriginalPath: "source", PlayablePath: "output", FileName: job.FileName, SizeBytes: 20, DurationSeconds: 0}
	if _, err = s.CompleteJob(ctx, job, candidate, nil); err == nil {
		t.Fatal("invalid activation must fail")
	}
	jobs, _ := s.Jobs(ctx)
	if jobs[0].State != "processing" || jobs[0].ReservedBytes != 30 {
		t.Fatalf("failed activation lost reservation: %#v", jobs)
	}
	candidate.DurationSeconds = 10
	if _, err = s.CompleteJob(ctx, job, candidate, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteJob(ctx, job, candidate, nil); err == nil {
		t.Fatal("duplicate activation accepted")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(ctx)
	used, reserved, err := s.Quota(ctx)
	if err != nil || jobs[0].State != "succeeded" || used != 20 || reserved != 0 {
		t.Fatalf("jobs=%#v usage=%d reserved=%d err=%v", jobs, used, reserved, err)
	}
	// Replacement candidates must compete with existing files and queued jobs.
	if err = s.CreateJob(ctx, UploadJob{ID: "replacement", TargetTrackID: job.ID, FileName: "new", SourcePath: "new", OutputPath: "new.mp3"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveJob(ctx, "replacement", 81, 100); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("replacement bypassed quota: %v", err)
	}
}
