package bootstrap

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

func (r *Repo) GetActiveStore(ctx context.Context, tenantID string, storeID string) (StoreDTO, error) {
	const query = `
SELECT
	id::text,
	tenant_id::text,
	name,
	code,
	location,
	COALESCE(hdfc_terminal_tid, '')
FROM stores
WHERE id = $1
  AND tenant_id = $2
  AND active = TRUE
LIMIT 1`

	var store StoreDTO
	err := r.db.QueryRowContext(ctx, query, storeID, tenantID).Scan(
		&store.ID,
		&store.TenantID,
		&store.Name,
		&store.Code,
		&store.Location,
		&store.HDFCTerminalTID,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return StoreDTO{}, apperrors.New(apperrors.CodeNotFound, "Store not found")
		}
		return StoreDTO{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load store",
			map[string]any{"reason": err.Error()},
		)
	}

	return store, nil
}

func (r *Repo) GetActiveCatalogueItems(ctx context.Context, tenantID string) ([]CatalogueItemDTO, error) {
	const query = `
SELECT
	id::text,
	name,
	category,
	list_price
FROM catalogue_items
WHERE tenant_id = $1
  AND active = TRUE
ORDER BY name ASC, id ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load catalogue items",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	items := make([]CatalogueItemDTO, 0)
	for rows.Next() {
		var item CatalogueItemDTO
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.ListPrice); err != nil {
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

func (r *Repo) GetActiveStoreStaff(ctx context.Context, tenantID string, storeID string) ([]StaffDTO, error) {
	const query = `
SELECT
	s.id::text,
	s.name
FROM staff s
INNER JOIN staff_store_mapping ssm
	ON ssm.staff_id = s.id
WHERE s.tenant_id = $1
  AND s.active = TRUE
  AND ssm.store_id = $2
  AND ssm.active = TRUE
ORDER BY s.name ASC, s.id ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantID, storeID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load store staff",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	staffList := make([]StaffDTO, 0)
	for rows.Next() {
		var member StaffDTO
		if err := rows.Scan(&member.ID, &member.Name); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan store staff",
				map[string]any{"reason": err.Error()},
			)
		}
		staffList = append(staffList, member)
	}

	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading store staff",
			map[string]any{"reason": err.Error()},
		)
	}

	return staffList, nil
}
