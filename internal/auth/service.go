package auth

import (
	"context"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	platformconfig "gloss/internal/platform/config"
	"gloss/internal/shared/enums"
	apperrors "gloss/internal/shared/errors"
)

type Service struct {
	cfg  platformconfig.Config
	repo *Repo
}

func NewService(cfg platformconfig.Config, repo *Repo) *Service {
	return &Service{
		cfg:  cfg,
		repo: repo,
	}
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (LoginResult, error) {
	req.LoginIdentity = strings.TrimSpace(req.LoginIdentity)
	if req.LoginIdentity == "" || req.Password == "" {
		return LoginResult{}, apperrors.New(
			apperrors.CodeInvalidRequest,
			"login_identity and password are required",
		)
	}

	user, err := s.repo.FindByLoginIdentity(ctx, req.LoginIdentity)
	if err != nil {
		return LoginResult{}, err
	}

	if !user.Active {
		return LoginResult{}, apperrors.New(apperrors.CodeUnauthorized, "Inactive user cannot login")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return LoginResult{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid credentials")
	}

	if HasRole(user.Role, enums.RoleStoreManager) && user.StoreID == "" {
		return LoginResult{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid manager store scope")
	}

	token, err := s.signToken(user)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Token: token,
	}, nil
}

func (s *Service) signToken(user UserRecord) (string, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.Auth.JWTTTL)

	claims := Claims{
		UserID:   user.ID,
		TenantID: user.TenantID,
		StoreID:  user.StoreID,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.Auth.JWTSecret))
	if err != nil {
		return "", apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to issue token",
			map[string]any{"reason": err.Error()},
		)
	}

	return signed, nil
}
