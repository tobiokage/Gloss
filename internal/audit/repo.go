package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"

	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) Insert(ctx context.Context, input RecordInput) error {
	auditID, err := newUUIDString()
	if err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create audit log",
			map[string]any{"reason": err.Error()},
		)
	}

	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create audit log",
			map[string]any{"reason": err.Error()},
		)
	}

	const query = `
INSERT INTO audit_logs (
	id,
	tenant_id,
	store_id,
	entity_type,
	entity_id,
	action,
	performed_by_user_id,
	metadata,
	created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW())`

	if _, err := r.db.ExecContext(
		ctx,
		query,
		auditID,
		input.TenantID,
		nullIfEmpty(input.StoreID),
		input.EntityType,
		input.EntityID,
		input.Action,
		nullIfEmpty(input.PerformedByUserID),
		string(encodedMetadata),
	); err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create audit log",
			map[string]any{"reason": err.Error()},
		)
	}

	return nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
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
