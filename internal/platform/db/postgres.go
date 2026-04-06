package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	platformconfig "gloss/internal/platform/config"
	apperrors "gloss/internal/shared/errors"
)

func NewPostgres(cfg platformconfig.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.DB.Host,
		cfg.DB.Port,
		cfg.DB.User,
		cfg.DB.Password,
		cfg.DB.Name,
		cfg.DB.SSLMode,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeDBUnavailable,
			"failed to open postgres connection",
			map[string]any{"reason": err.Error()},
		)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	return db, nil
}
