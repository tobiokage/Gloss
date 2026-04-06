package main

import (
	"context"
	"errors"
	"log/slog"
	nethttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	platformconfig "gloss/internal/platform/config"
	platformdb "gloss/internal/platform/db"
	platformhttp "gloss/internal/platform/http"
	platformlogger "gloss/internal/platform/logger"
)

func main() {
	cfg, err := platformconfig.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := platformlogger.New(cfg)

	db, err := platformdb.NewPostgres(cfg)
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		logger.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	app := platformhttp.App{
		Config: cfg,
		Logger: logger,
		DB:     db,
	}

	router := platformhttp.NewRouter(app)

	server := &nethttp.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", server.Addr, "env", cfg.AppEnv)
		serverErrCh <- server.ListenAndServe()
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}
