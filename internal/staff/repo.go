package staff

import (
	"context"
	"crypto/rand"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) ListByTenant(ctx context.Context, tenantID string) ([]Staff, error) {
	const query = `
SELECT
	id::text,
	name,
	active,
	created_at,
	updated_at
FROM staff
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load staff",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	staffList := make([]Staff, 0)
	for rows.Next() {
		var member Staff
		if err := rows.Scan(
			&member.ID,
			&member.Name,
			&member.Active,
			&member.CreatedAt,
			&member.UpdatedAt,
		); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan staff",
				map[string]any{"reason": err.Error()},
			)
		}
		staffList = append(staffList, member)
	}

	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading staff",
			map[string]any{"reason": err.Error()},
		)
	}

	return staffList, nil
}

func (r *Repo) Create(ctx context.Context, input CreateStaffInput) (Staff, error) {
	staffID, err := newUUIDString()
	if err != nil {
		return Staff{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create staff",
			map[string]any{"reason": err.Error()},
		)
	}

	const query = `
INSERT INTO staff (
	id,
	tenant_id,
	name,
	active,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, TRUE, NOW(), NOW())
RETURNING
	id::text,
	name,
	active,
	created_at,
	updated_at`

	var member Staff
	err = r.db.QueryRowContext(ctx, query, staffID, input.TenantID, input.Name).Scan(
		&member.ID,
		&member.Name,
		&member.Active,
		&member.CreatedAt,
		&member.UpdatedAt,
	)
	if err != nil {
		return Staff{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create staff",
			map[string]any{"reason": err.Error()},
		)
	}

	return member, nil
}

func (r *Repo) GetByIDAndTenant(ctx context.Context, staffID string, tenantID string) (Staff, error) {
	const query = `
SELECT
	id::text,
	name,
	active,
	created_at,
	updated_at
FROM staff
WHERE id = $1
  AND tenant_id = $2
LIMIT 1`

	var member Staff
	err := r.db.QueryRowContext(ctx, query, staffID, tenantID).Scan(
		&member.ID,
		&member.Name,
		&member.Active,
		&member.CreatedAt,
		&member.UpdatedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return Staff{}, apperrors.New(apperrors.CodeNotFound, "Staff not found")
		}
		return Staff{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load staff",
			map[string]any{"reason": err.Error()},
		)
	}

	return member, nil
}

func (r *Repo) Deactivate(ctx context.Context, staffID string, tenantID string) (Staff, error) {
	const query = `
UPDATE staff
SET active = FALSE,
	updated_at = NOW()
WHERE id = $1
  AND tenant_id = $2
RETURNING
	id::text,
	name,
	active,
	created_at,
	updated_at`

	var member Staff
	err := r.db.QueryRowContext(ctx, query, staffID, tenantID).Scan(
		&member.ID,
		&member.Name,
		&member.Active,
		&member.CreatedAt,
		&member.UpdatedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return Staff{}, apperrors.New(apperrors.CodeNotFound, "Staff not found")
		}
		return Staff{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to deactivate staff",
			map[string]any{"reason": err.Error()},
		)
	}

	return member, nil
}

func (r *Repo) StoreExistsForTenant(ctx context.Context, storeID string, tenantID string) (bool, error) {
	const query = `
SELECT 1
FROM stores
WHERE id = $1
  AND tenant_id = $2
LIMIT 1`

	var marker int
	err := r.db.QueryRowContext(ctx, query, storeID, tenantID).Scan(&marker)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load store",
			map[string]any{"reason": err.Error()},
		)
	}

	return true, nil
}

func (r *Repo) CreateMapping(ctx context.Context, input CreateStaffStoreMappingInput) (StaffStoreMapping, error) {
	mappingID, err := newUUIDString()
	if err != nil {
		return StaffStoreMapping{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to map staff to store",
			map[string]any{"reason": err.Error()},
		)
	}

	const query = `
INSERT INTO staff_store_mapping (
	id,
	staff_id,
	store_id,
	active,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, TRUE, NOW(), NOW())
RETURNING
	id::text,
	staff_id::text,
	store_id::text,
	active,
	created_at,
	updated_at`

	var mapping StaffStoreMapping
	err = r.db.QueryRowContext(
		ctx,
		query,
		mappingID,
		input.StaffID,
		input.StoreID,
	).Scan(
		&mapping.ID,
		&mapping.StaffID,
		&mapping.StoreID,
		&mapping.Active,
		&mapping.CreatedAt,
		&mapping.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if stderrors.As(err, &pgErr) && pgErr.Code == "23505" {
			return StaffStoreMapping{}, apperrors.New(apperrors.CodeInvalidRequest, "Staff is already mapped to store")
		}
		return StaffStoreMapping{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to map staff to store",
			map[string]any{"reason": err.Error()},
		)
	}

	return mapping, nil
}

func (r *Repo) IsActiveMappedStaff(ctx context.Context, tenantID string, storeID string, staffID string) (bool, error) {
	const query = `
SELECT 1
FROM staff s
INNER JOIN staff_store_mapping ssm
	ON ssm.staff_id = s.id
INNER JOIN stores st
	ON st.id = ssm.store_id
WHERE s.id = $1
  AND s.tenant_id = $2
  AND s.active = TRUE
  AND ssm.store_id = $3
  AND ssm.active = TRUE
  AND st.tenant_id = $2
LIMIT 1`

	var marker int
	err := r.db.QueryRowContext(ctx, query, staffID, tenantID, storeID).Scan(&marker)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to validate staff store mapping",
			map[string]any{"reason": err.Error()},
		)
	}

	return true, nil
}

func newUUIDString() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("failed to read random bytes: %w", err)
	}

	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		buffer[0:4],
		buffer[4:6],
		buffer[6:8],
		buffer[8:10],
		buffer[10:16],
	), nil
}
