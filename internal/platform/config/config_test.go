package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsWeakProductionJWTSecret(t *testing.T) {
	setValidConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "secret")

	_, err := Load()
	if err == nil {
		t.Fatal("expected weak production JWT secret to fail")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("expected JWT_SECRET validation error, got %v", err)
	}
}

func TestLoadRejectsProductionLocalHDFCBaseURL(t *testing.T) {
	setValidConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("HDFC_BASE_URL", "http://localhost:8081")

	_, err := Load()
	if err == nil {
		t.Fatal("expected production localhost HDFC URL to fail")
	}
	if !strings.Contains(err.Error(), "HDFC_BASE_URL") {
		t.Fatalf("expected HDFC_BASE_URL validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidHDFCCryptoConfig(t *testing.T) {
	setValidConfigEnv(t)
	t.Setenv("HDFC_CLIENT_SECRET_KEY", "not-hex")

	_, err := Load()
	if err == nil {
		t.Fatal("expected invalid HDFC secret key to fail")
	}
	if !strings.Contains(err.Error(), "HDFC_CLIENT_SECRET_KEY") {
		t.Fatalf("expected HDFC_CLIENT_SECRET_KEY validation error, got %v", err)
	}

	setValidConfigEnv(t)
	t.Setenv("HDFC_IV", "short")
	_, err = Load()
	if err == nil {
		t.Fatal("expected invalid HDFC IV to fail")
	}
	if !strings.Contains(err.Error(), "HDFC_IV") {
		t.Fatalf("expected HDFC_IV validation error, got %v", err)
	}
}

func TestLoadRejectsRelativeHDFCBaseURL(t *testing.T) {
	setValidConfigEnv(t)
	t.Setenv("HDFC_BASE_URL", "hdfc-local")

	_, err := Load()
	if err == nil {
		t.Fatal("expected relative HDFC URL to fail")
	}
	if !strings.Contains(err.Error(), "HDFC_BASE_URL") {
		t.Fatalf("expected HDFC_BASE_URL validation error, got %v", err)
	}
}

func TestLoadRejectsProductionDisabledDBSSL(t *testing.T) {
	setValidConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_SSLMODE", "disable")

	_, err := Load()
	if err == nil {
		t.Fatal("expected production DB_SSLMODE=disable to fail")
	}
	if !strings.Contains(err.Error(), "DB_SSLMODE") {
		t.Fatalf("expected DB_SSLMODE validation error, got %v", err)
	}
}

func setValidConfigEnv(t *testing.T) {
	t.Helper()

	t.Setenv("APP_ENV", "development")
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("SHUTDOWN_TIMEOUT_SECONDS", "10")
	t.Setenv("JWT_SECRET", "local-development-secret-that-is-long")
	t.Setenv("JWT_TTL_MINUTES", "60")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "gloss")
	t.Setenv("DB_PASSWORD", "gloss")
	t.Setenv("DB_NAME", "gloss")
	t.Setenv("DB_SSLMODE", "require")
	t.Setenv("HDFC_BASE_URL", "https://payments.hdfcbank.com")
	t.Setenv("HDFC_CLIENT_API_KEY", "local-api-key")
	t.Setenv("HDFC_CLIENT_SECRET_KEY", strings.Repeat("a", 64))
	t.Setenv("HDFC_AUTHORIZATION_TOKEN", "local-auth-token")
	t.Setenv("HDFC_IV", "1234567890123456")
}
