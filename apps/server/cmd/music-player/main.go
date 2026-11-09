package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"music-player/server/internal/artwork"
	"music-player/server/internal/config"
	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	appserver "music-player/server/internal/server"
	"music-player/server/internal/store"
)

func main() {
	cfg, err := config.Load(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(strings.ToUpper(cfg.LogLevel)))
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	for _, tool := range []string{cfg.FFmpeg, cfg.FFprobe} {
		if _, err = exec.LookPath(tool); err != nil {
			log.Error("audio tool unavailable", "tool", tool, "error", err)
			os.Exit(1)
		}
	}
	root := cfg.DataDir
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o700); err != nil {
		log.Error("create data dir", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s, err := store.Open(ctx, cfg.DatabasePath, store.WithDataRoot(root))
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer s.Close()
	webDir := cfg.WebDir
	lyricsClient := lyrics.New(cfg.LRCLIBURL)
	processor := media.New(root)
	processor.FFmpeg, processor.FFprobe = cfg.FFmpeg, cfg.FFprobe
	api := &appserver.API{Artwork: artwork.New(), Store: s, Media: processor, Lyrics: lyricsClient, Log: log, WebDir: webDir, QuotaLimit: cfg.QuotaBytes}
	if err = api.RecoverJobs(ctx); err != nil {
		log.Error("recover upload jobs", "error", err)
		return
	}
	jobsDone := make(chan struct{})
	go func() { defer close(jobsDone); api.RunJobs(ctx) }()
	defer func() { stop(); <-jobsDone }()
	maintenanceDone := make(chan struct{})
	defer func() { stop(); <-maintenanceDone }()
	go func() {
		defer close(maintenanceDone)
		api.Maintain(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				api.ScheduleMediaMaintenance(ctx)
				api.Maintain(ctx)
			}
		}
	}()
	httpServer := &http.Server{Addr: cfg.Addr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
		}
	}()
	log.Info("music player started", "address", httpServer.Addr, "data", root, "web", webDir, "database", cfg.DatabasePath, "config", cfg.ConfigFile)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "error", err)
	}
	stop()
	<-shutdownDone
}
