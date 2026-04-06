package auth

import (
	"context"
	"database/sql"
	stderrors "errors"

	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) FindByLoginIdentity(ctx context.Context, loginIdentity string) (UserRecord, error) {
	const query = `
SELECT
	id::text,
	COALESCE(tenant_id::text, ''),
	COALESCE(store_id::text, ''),
	role,
	name,
	email_or_phone,
	password_hash,
	active
FROM users
WHERE email_or_phone = $1
LIMIT 1`

	var user UserRecord
	err := r.db.QueryRowContext(ctx, query, loginIdentity).Scan(
		&user.ID,
		&user.TenantID,
		&user.StoreID,
		&user.Role,
		&user.Name,
		&user.EmailOrPhone,
		&user.PasswordHash,
		&user.Active,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return UserRecord{}, apperrors.New(apperrors.CodeUnauthorized, "Invalid credentials")
		}
		return UserRecord{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to fetch user",
			map[string]any{"reason": err.Error()},
		)
	}

	return user, nil
}
