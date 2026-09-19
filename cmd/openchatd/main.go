package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openchat/openchat-backend/internal/api"
	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/auth"
	"github.com/openchat/openchat-backend/internal/store/postgres"
)

func main() {
	cfg := app.LoadConfigFromEnv()
	build := app.CurrentBuildInfo()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	var authService *auth.Service
	if cfg.DatabaseURL != "" {
		startupCtx, startupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		pool, err := postgres.Open(startupCtx, cfg.DatabaseURL)
		startupCancel()
		if err != nil {
			logger.Error("Postgres startup failed", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		authService = auth.New(pool)
	} else if cfg.IsProduction() {
		logger.Error("production requires OPENCHAT_DATABASE_URL for verified sessions")
		os.Exit(1)
	}
	server := api.NewServer(cfg, logger)
	server.SetAuthService(authService)
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		logger.Info(
			"openchat-backend starting",
			"addr", cfg.HTTPAddr,
			"version", build.Version,
			"commit", build.Commit,
			"commit_short", build.CommitShort,
			"build_time", build.BuildTime,
			"vcs_modified", build.VCSModified,
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	<-signalCh

	logger.Info("shutdown requested")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
