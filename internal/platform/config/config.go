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
	Auth            AuthConfig
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type AuthConfig struct {
	JWTSecret string
	JWTTTL    time.Duration
}

func Load() (Config, error) {
	dbCfg, err := loadDBConfig()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:          getEnvOrDefault("APP_ENV", "development"),
		HTTPPort:        getEnvOrDefault("HTTP_PORT", "8080"),
		ShutdownTimeout: 10 * time.Second,
		DB:              dbCfg,
		Auth: AuthConfig{
			JWTSecret: os.Getenv("JWT_SECRET"),
			JWTTTL:    60 * time.Minute,
		},
	}

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

	jwtTTLRaw := getEnvOrDefault("JWT_TTL_MINUTES", "60")
	jwtTTLMinutes, err := strconv.Atoi(jwtTTLRaw)
	if err != nil || jwtTTLMinutes <= 0 {
		return Config{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"JWT_TTL_MINUTES must be a positive integer",
			map[string]any{"field": "JWT_TTL_MINUTES", "value": jwtTTLRaw},
		)
	}
	cfg.Auth.JWTTTL = time.Duration(jwtTTLMinutes) * time.Minute

	if err := validateRequired(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func LoadDB() (DBConfig, error) {
	return loadDBConfig()
}

func loadDBConfig() (DBConfig, error) {
	cfg := DBConfig{
		Host:     os.Getenv("DB_HOST"),
		User:     os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASSWORD"),
		Name:     os.Getenv("DB_NAME"),
		SSLMode:  os.Getenv("DB_SSLMODE"),
	}
	if cfg.SSLMode == "" {
		return DBConfig{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"DB_SSLMODE is required",
			map[string]any{"field": "DB_SSLMODE"},
		)
	}

	dbPortRaw := os.Getenv("DB_PORT")
	if dbPortRaw == "" {
		return DBConfig{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"DB_PORT is required",
			map[string]any{"field": "DB_PORT"},
		)
	}
	dbPort, err := strconv.Atoi(dbPortRaw)
	if err != nil || dbPort <= 0 {
		return DBConfig{}, apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"DB_PORT must be a positive integer",
			map[string]any{"field": "DB_PORT", "value": dbPortRaw},
		)
	}
	cfg.Port = dbPort

	required := map[string]string{
		"DB_HOST":     cfg.Host,
		"DB_USER":     cfg.User,
		"DB_PASSWORD": cfg.Password,
		"DB_NAME":     cfg.Name,
	}

	for key, value := range required {
		if value == "" {
			return DBConfig{}, apperrors.NewWithDetails(
				apperrors.CodeInvalidConfig,
				fmt.Sprintf("%s is required", key),
				map[string]any{"field": key},
			)
		}
	}

	return cfg, nil
}

func validateRequired(cfg Config) error {
	required := map[string]string{
		"APP_ENV":    cfg.AppEnv,
		"HTTP_PORT":  cfg.HTTPPort,
		"JWT_SECRET": cfg.Auth.JWTSecret,
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
