package logger

import (
	"log/slog"
	"os"

	platformconfig "gloss/internal/platform/config"
)

func New(cfg platformconfig.Config) *slog.Logger {
	level := slog.LevelInfo
	if cfg.AppEnv == "development" || cfg.AppEnv == "local" {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler).With("service", "gloss-api")
}
