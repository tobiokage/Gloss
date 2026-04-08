package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	apperrors "gloss/internal/shared/errors"
)

type Store struct{}

type ClaimResult struct {
	Completed      bool
	ResponseBillID string
}

func NewStore() *Store {
	return &Store{}
}

func CanonicalRequestHash(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) ClaimCreateBill(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	idempotencyKey string,
	requestHash string,
) (ClaimResult, error) {
	rowID, err := newUUIDString()
	if err != nil {
		return ClaimResult{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to claim idempotency key",
			map[string]any{"reason": err.Error()},
		)
	}

	const insertQuery = `
INSERT INTO idempotency_keys (
	id,
	tenant_id,
	store_id,
	idempotency_key,
	request_hash,
	status,
	response_bill_id,
	created_at,
	updated_at
)
VALUES ($1, $2, $3, $4, $5, 'IN_PROGRESS', NULL, NOW(), NOW())`

	if _, err := tx.ExecContext(ctx, insertQuery, rowID, tenantID, storeID, idempotencyKey, requestHash); err == nil {
		return ClaimResult{}, nil
	} else {
		var pgErr *pgconn.PgError
		if !stderrors.As(err, &pgErr) || pgErr.Code != "23505" {
			return ClaimResult{}, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to claim idempotency key",
				map[string]any{"reason": err.Error()},
			)
		}
	}

	const selectQuery = `
SELECT
	request_hash,
	status,
	response_bill_id::text
FROM idempotency_keys
WHERE tenant_id = $1
  AND store_id = $2
  AND idempotency_key = $3
LIMIT 1`

	var (
		storedHash     string
		status         string
		responseBillID sql.NullString
	)
	if err := tx.QueryRowContext(ctx, selectQuery, tenantID, storeID, idempotencyKey).Scan(
		&storedHash,
		&status,
		&responseBillID,
	); err != nil {
		return ClaimResult{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load idempotency key",
			map[string]any{"reason": err.Error()},
		)
	}

	if storedHash != requestHash {
		return ClaimResult{}, apperrors.New(
			apperrors.CodeInvalidRequest,
			"idempotency_key is already used for a different create bill request",
		)
	}

	switch status {
	case "COMPLETED":
		if !responseBillID.Valid || responseBillID.String == "" {
			return ClaimResult{}, apperrors.New(
				apperrors.CodeInternalError,
				"Completed idempotency record is missing bill reference",
			)
		}
		return ClaimResult{
			Completed:      true,
			ResponseBillID: responseBillID.String,
		}, nil
	case "IN_PROGRESS":
		return ClaimResult{}, apperrors.New(
			apperrors.CodeInvalidRequest,
			"idempotency request is already in progress",
		)
	default:
		return ClaimResult{}, apperrors.New(
			apperrors.CodeInternalError,
			"Invalid idempotency record state",
		)
	}
}

func (s *Store) CompleteCreateBill(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	idempotencyKey string,
	billID string,
) error {
	const query = `
UPDATE idempotency_keys
SET status = 'COMPLETED',
	response_bill_id = $4,
	updated_at = NOW()
WHERE tenant_id = $1
  AND store_id = $2
  AND idempotency_key = $3
  AND status = 'IN_PROGRESS'`

	result, err := tx.ExecContext(ctx, query, tenantID, storeID, idempotencyKey, billID)
	if err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to complete idempotency key",
			map[string]any{"reason": err.Error()},
		)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to complete idempotency key",
			map[string]any{"reason": err.Error()},
		)
	}
	if rowsAffected != 1 {
		return apperrors.New(
			apperrors.CodeInternalError,
			"Idempotency key was not claimed for completion",
		)
	}

	return nil
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
