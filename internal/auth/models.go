package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type LoginRequest struct {
	LoginIdentity string `json:"login_identity"`
	Password      string `json:"password"`
}

type SessionUser struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	StoreID  string `json:"store_id"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

type LoginResponse struct {
	Token   string      `json:"token"`
	Session SessionUser `json:"session"`
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
	Name         string
	EmailOrPhone string
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
	Token     string
	Session   SessionUser
	ExpiresAt time.Time
}
