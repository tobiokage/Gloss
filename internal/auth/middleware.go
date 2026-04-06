package auth

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	platformconfig "gloss/internal/platform/config"
	platformhttp "gloss/internal/platform/http"
	apperrors "gloss/internal/shared/errors"
)

type contextKey string

const authContextKey contextKey = "auth_context"

func Middleware(cfg platformconfig.Config) func(next nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			tokenValue := extractBearerToken(r.Header.Get("Authorization"))
			if tokenValue == "" {
				platformhttp.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "Missing bearer token"))
				return
			}

			claims := &Claims{}
			parsedToken, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
				if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
					return nil, apperrors.New(apperrors.CodeUnauthorized, "Invalid token")
				}
				return []byte(cfg.Auth.JWTSecret), nil
			})
			if err != nil || !parsedToken.Valid {
				platformhttp.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "Invalid token"))
				return
			}

			if claims.UserID == "" || claims.Role == "" {
				platformhttp.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "Invalid token claims"))
				return
			}
			if claims.TenantID == "" {
				platformhttp.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "Invalid token claims"))
				return
			}
			if claims.Role == "STORE_MANAGER" && claims.StoreID == "" {
				platformhttp.WriteError(w, apperrors.New(apperrors.CodeUnauthorized, "Invalid token claims"))
				return
			}

			ctx := context.WithValue(r.Context(), authContextKey, claims.AuthContext())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AuthContextFromContext(ctx context.Context) (AuthContext, error) {
	value := ctx.Value(authContextKey)
	authCtx, ok := value.(AuthContext)
	if !ok {
		return AuthContext{}, apperrors.New(apperrors.CodeUnauthorized, "Auth context missing")
	}
	return authCtx, nil
}

func extractBearerToken(headerValue string) string {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(headerValue, bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(headerValue, bearerPrefix))
}
