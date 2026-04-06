package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	apperrors "gloss/internal/shared/errors"
)

type Config struct {
	AppEnv          string
	HTTPPort        string
	ShutdownTimeout time.Duration
	DB              DBConfig
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

func Load() (Config, error) {
	cfg := Config{
		AppEnv:          getEnvOrDefault("APP_ENV", "development"),
		HTTPPort:        getEnvOrDefault("HTTP_PORT", "8080"),
		ShutdownTimeout: 10 * time.Second,
		DB: DBConfig{
			Host:     os.Getenv("DB_HOST"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
			Name:     os.Getenv("DB_NAME"),
			SSLMode:  getEnvOrDefault("DB_SSLMODE", "disable"),
		},
	}

	dbPortRaw := getEnvOrDefault("DB_PORT", "5432")
	dbPort, err := strconv.Atoi(dbPortRaw)
	if err != nil || dbPort <= 0 {
		return Config{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"DB_PORT must be a positive integer",
			map[string]any{"field": "DB_PORT", "value": dbPortRaw},
		)
	}
	cfg.DB.Port = dbPort

	shutdownSecondsRaw := getEnvOrDefault("SHUTDOWN_TIMEOUT_SECONDS", "10")
	shutdownSeconds, err := strconv.Atoi(shutdownSecondsRaw)
	if err != nil || shutdownSeconds <= 0 {
		return Config{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"SHUTDOWN_TIMEOUT_SECONDS must be a positive integer",
			map[string]any{"field": "SHUTDOWN_TIMEOUT_SECONDS", "value": shutdownSecondsRaw},
		)
	}
	cfg.ShutdownTimeout = time.Duration(shutdownSeconds) * time.Second

	if err := validateRequired(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateRequired(cfg Config) error {
	required := map[string]string{
		"APP_ENV":     cfg.AppEnv,
		"HTTP_PORT":   cfg.HTTPPort,
		"DB_HOST":     cfg.DB.Host,
		"DB_USER":     cfg.DB.User,
		"DB_PASSWORD": cfg.DB.Password,
		"DB_NAME":     cfg.DB.Name,
		"DB_SSLMODE":  cfg.DB.SSLMode,
	}

	for key, value := range required {
		if value == "" {
			return apperrors.NewWithDetails(
				apperrors.CodeInvalidConfig,
				fmt.Sprintf("%s is required", key),
				map[string]any{"field": key},
			)
		}
	}

	return nil
}

func getEnvOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
