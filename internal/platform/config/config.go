package config

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	apperrors "gloss/internal/shared/errors"
)

type Config struct {
	AppEnv          string
	HTTPPort        string
	ShutdownTimeout time.Duration
	DB              DBConfig
	Auth            AuthConfig
	HDFC            HDFCConfig
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

type HDFCConfig struct {
	BaseURL            string
	ClientAPIKey       string
	ClientSecretKeyHex string
	AuthorizationToken string
	IV                 string
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
		HDFC: HDFCConfig{
			BaseURL:            os.Getenv("HDFC_BASE_URL"),
			ClientAPIKey:       os.Getenv("HDFC_CLIENT_API_KEY"),
			ClientSecretKeyHex: os.Getenv("HDFC_CLIENT_SECRET_KEY"),
			AuthorizationToken: os.Getenv("HDFC_AUTHORIZATION_TOKEN"),
			IV:                 os.Getenv("HDFC_IV"),
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
		"APP_ENV":                  cfg.AppEnv,
		"HTTP_PORT":                cfg.HTTPPort,
		"JWT_SECRET":               cfg.Auth.JWTSecret,
		"HDFC_BASE_URL":            cfg.HDFC.BaseURL,
		"HDFC_CLIENT_API_KEY":      cfg.HDFC.ClientAPIKey,
		"HDFC_CLIENT_SECRET_KEY":   cfg.HDFC.ClientSecretKeyHex,
		"HDFC_AUTHORIZATION_TOKEN": cfg.HDFC.AuthorizationToken,
		"HDFC_IV":                  cfg.HDFC.IV,
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

	if err := validateAuthConfig(cfg); err != nil {
		return err
	}
	if err := validateHDFCConfig(cfg); err != nil {
		return err
	}
	if err := validateProductionDBConfig(cfg); err != nil {
		return err
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

func validateAuthConfig(cfg Config) error {
	if !isProductionEnv(cfg.AppEnv) {
		return nil
	}

	secret := strings.TrimSpace(cfg.Auth.JWTSecret)
	if len(secret) < 32 {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"JWT_SECRET must be at least 32 characters in production",
			map[string]any{"field": "JWT_SECRET"},
		)
	}

	if isKnownWeakSecret(secret) {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"JWT_SECRET must not use a weak or placeholder value in production",
			map[string]any{"field": "JWT_SECRET"},
		)
	}

	return nil
}

func validateHDFCConfig(cfg Config) error {
	parsed, err := url.ParseRequestURI(cfg.HDFC.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_BASE_URL must be a valid absolute URL",
			map[string]any{"field": "HDFC_BASE_URL"},
		)
	}

	secretBytes, err := hex.DecodeString(cfg.HDFC.ClientSecretKeyHex)
	if err != nil || len(secretBytes) != 32 {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_CLIENT_SECRET_KEY must be 64 hex characters",
			map[string]any{"field": "HDFC_CLIENT_SECRET_KEY"},
		)
	}

	if len([]byte(cfg.HDFC.IV)) != 16 {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_IV must be 16 bytes",
			map[string]any{"field": "HDFC_IV"},
		)
	}

	if !isProductionEnv(cfg.AppEnv) {
		return nil
	}

	parsed, err = url.Parse(cfg.HDFC.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_BASE_URL must be a valid absolute URL",
			map[string]any{"field": "HDFC_BASE_URL"},
		)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost") {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_BASE_URL must not point to localhost in production",
			map[string]any{"field": "HDFC_BASE_URL"},
		)
	}
	if host == "example.com" || strings.HasSuffix(host, ".example.com") {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"HDFC_BASE_URL must not use an example host in production",
			map[string]any{"field": "HDFC_BASE_URL"},
		)
	}

	return nil
}

func validateProductionDBConfig(cfg Config) error {
	if !isProductionEnv(cfg.AppEnv) {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(cfg.DB.SSLMode), "disable") {
		return apperrors.NewWithDetails(
			apperrors.CodeInvalidConfig,
			"DB_SSLMODE must not be disable in production",
			map[string]any{"field": "DB_SSLMODE"},
		)
	}
	return nil
}

func isProductionEnv(appEnv string) bool {
	normalized := strings.ToLower(strings.TrimSpace(appEnv))
	return normalized == "production" || normalized == "prod"
}

func isKnownWeakSecret(secret string) bool {
	normalized := strings.ToLower(strings.TrimSpace(secret))
	switch normalized {
	case "secret", "changeme", "change-me", "password", "jwt_secret", "jwt-secret",
		"dev-secret", "test-secret", "development-secret":
		return true
	default:
		return false
	}
}
