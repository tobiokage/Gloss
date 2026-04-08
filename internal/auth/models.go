package auth

import (
	"github.com/golang-jwt/jwt/v5"

	"gloss/internal/shared/enums"
	apperrors "gloss/internal/shared/errors"
)

type LoginRequest struct {
	EmailOrPhone string `json:"email_or_phone"`
	Password     string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

type AuthContext struct {
	UserID   string
	TenantID string
	StoreID  string
	Role     string
}

type UserRecord struct {
	ID           string
	TenantID     string
	StoreID      string
	Role         string
	PasswordHash string
	Active       bool
}

type Claims struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	StoreID  string `json:"store_id"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func (c Claims) AuthContext() AuthContext {
	return AuthContext{
		UserID:   c.UserID,
		TenantID: c.TenantID,
		StoreID:  c.StoreID,
		Role:     c.Role,
	}
}

type LoginResult struct {
	Token string
}

func HasRole(actualRole string, expected enums.Role) bool {
	return actualRole == string(expected)
}

func RequireRole(authCtx AuthContext, expected enums.Role) error {
	if !HasRole(authCtx.Role, expected) {
		return apperrors.New(apperrors.CodeUnauthorized, "User role is not allowed")
	}
	return nil
}
