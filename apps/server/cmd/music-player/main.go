package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"music-player/server/internal/lyrics"
	"music-player/server/internal/media"
	appserver "music-player/server/internal/server"
	"music-player/server/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	root := env("MUSIC_DATA_DIR", ".data")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		log.Error("create data dir", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s, err := store.Open(ctx, filepath.Join(root, "data", "music.db"))
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer s.Close()
	webDir := findWebDir()
	lyricsClient := lyrics.New(env("MUSIC_LRCLIB_URL", "https://lrclib.net"))
	api := &appserver.API{Store: s, Media: media.New(root), Lyrics: lyricsClient, Log: log, WebDir: webDir}
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
	httpServer := &http.Server{Addr: env("MUSIC_ADDR", "127.0.0.1:8080"), Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
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
	log.Info("music player started", "address", httpServer.Addr, "data", root, "web", webDir)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server stopped", "error", err)
	}
	stop()
	<-shutdownDone
}

func findWebDir() string {
	if configured := os.Getenv("MUSIC_WEB_DIR"); configured != "" {
		return configured
	}
	for _, candidate := range []string{"apps/web/dist", "../web/dist", "web"} {
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
