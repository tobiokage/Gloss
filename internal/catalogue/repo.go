package catalogue

import (
	"context"
	"crypto/rand"
	"database/sql"
	stderrors "errors"
	"fmt"

	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) ListByTenant(ctx context.Context, tenantID string) ([]CatalogueItem, error) {
	const query = `
SELECT
	id::text,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at
FROM catalogue_items
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load catalogue items",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	items := make([]CatalogueItem, 0)
	for rows.Next() {
		var item CatalogueItem
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.Category,
			&item.ListPrice,
			&item.Active,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan catalogue items",
				map[string]any{"reason": err.Error()},
			)
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading catalogue items",
			map[string]any{"reason": err.Error()},
		)
	}

	return items, nil
}

func (r *Repo) Create(ctx context.Context, input CreateCatalogueItemInput) (CatalogueItem, error) {
	itemID, err := newUUIDString()
	if err != nil {
		return CatalogueItem{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create catalogue item",
			map[string]any{"reason": err.Error()},
		)
	}

	const query = `
INSERT INTO catalogue_items (
	id,
	tenant_id,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, TRUE, NOW(), NOW())
RETURNING
	id::text,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at`

	var item CatalogueItem
	err = r.db.QueryRowContext(
		ctx,
		query,
		itemID,
		input.TenantID,
		input.Name,
		input.Category,
		input.ListPrice,
	).Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.ListPrice,
		&item.Active,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return CatalogueItem{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create catalogue item",
			map[string]any{"reason": err.Error()},
		)
	}

	return item, nil
}

func (r *Repo) GetByIDAndTenant(ctx context.Context, itemID string, tenantID string) (CatalogueItem, error) {
	const query = `
SELECT
	id::text,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at
FROM catalogue_items
WHERE id = $1
  AND tenant_id = $2
LIMIT 1`

	var item CatalogueItem
	err := r.db.QueryRowContext(ctx, query, itemID, tenantID).Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.ListPrice,
		&item.Active,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return CatalogueItem{}, apperrors.New(apperrors.CodeNotFound, "Catalogue item not found")
		}
		return CatalogueItem{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load catalogue item",
			map[string]any{"reason": err.Error()},
		)
	}

	return item, nil
}

func (r *Repo) Update(ctx context.Context, input UpdateCatalogueItemInput) (CatalogueItem, error) {
	const query = `
UPDATE catalogue_items
SET name = $1,
	category = $2,
	list_price = $3,
	updated_at = NOW()
WHERE id = $4
  AND tenant_id = $5
RETURNING
	id::text,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at`

	var item CatalogueItem
	err := r.db.QueryRowContext(
		ctx,
		query,
		input.Name,
		input.Category,
		input.ListPrice,
		input.ItemID,
		input.TenantID,
	).Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.ListPrice,
		&item.Active,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return CatalogueItem{}, apperrors.New(apperrors.CodeNotFound, "Catalogue item not found")
		}
		return CatalogueItem{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to update catalogue item",
			map[string]any{"reason": err.Error()},
		)
	}

	return item, nil
}

func (r *Repo) Deactivate(ctx context.Context, itemID string, tenantID string) (CatalogueItem, error) {
	const query = `
UPDATE catalogue_items
SET active = FALSE,
	updated_at = NOW()
WHERE id = $1
  AND tenant_id = $2
RETURNING
	id::text,
	name,
	category,
	list_price,
	active,
	created_at,
	updated_at`

	var item CatalogueItem
	err := r.db.QueryRowContext(ctx, query, itemID, tenantID).Scan(
		&item.ID,
		&item.Name,
		&item.Category,
		&item.ListPrice,
		&item.Active,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return CatalogueItem{}, apperrors.New(apperrors.CodeNotFound, "Catalogue item not found")
		}
		return CatalogueItem{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to deactivate catalogue item",
			map[string]any{"reason": err.Error()},
		)
	}

	return item, nil
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
