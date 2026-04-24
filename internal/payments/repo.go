package payments

import (
	"context"
	"database/sql"
	stderrors "errors"
	"strings"
	"time"

	platformdb "gloss/internal/platform/db"
	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) GetStoreTerminalConfig(ctx context.Context, tenantID string, storeID string) (StoreTerminalConfig, error) {
	const query = `
SELECT
	id::text,
	tenant_id::text,
	COALESCE(hdfc_terminal_tid, '')
FROM stores
WHERE id = $1
  AND tenant_id = $2
  AND active = TRUE
LIMIT 1`

	var cfg StoreTerminalConfig
	err := r.db.QueryRowContext(ctx, query, storeID, tenantID).Scan(
		&cfg.StoreID,
		&cfg.TenantID,
		&cfg.HDFCTerminalTID,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return StoreTerminalConfig{}, apperrors.New(apperrors.CodeNotFound, "Store not found")
		}
		return StoreTerminalConfig{}, internalError("Failed to load store HDFC terminal configuration", err)
	}

	return cfg, nil
}

func (r *Repo) GetPaymentForSale(ctx context.Context, tenantID string, storeID string, billID string, paymentID string) (PaymentForSale, error) {
	const query = `
SELECT
	p.id::text,
	p.bill_id::text,
	b.tenant_id::text,
	b.store_id::text,
	b.bill_number,
	b.payment_mode_summary,
	p.amount,
	p.status,
	COALESCE(p.provider_request_id, ''),
	COALESCE(p.provider_txn_id, ''),
	COALESCE(p.terminal_tid, ''),
	p.provider_sale_requested_at
FROM payments p
INNER JOIN bills b
	ON b.id = p.bill_id
WHERE p.id = $1
  AND p.bill_id = $2
  AND b.tenant_id = $3
  AND b.store_id = $4
  AND p.payment_method = 'ONLINE'
LIMIT 1`

	var payment PaymentForSale
	var providerSaleRequestedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, paymentID, billID, tenantID, storeID).Scan(
		&payment.ID,
		&payment.BillID,
		&payment.TenantID,
		&payment.StoreID,
		&payment.BillNumber,
		&payment.PaymentMode,
		&payment.Amount,
		&payment.Status,
		&payment.ProviderRequestID,
		&payment.ProviderTxnID,
		&payment.TerminalTID,
		&providerSaleRequestedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return PaymentForSale{}, apperrors.New(apperrors.CodeNotFound, "Online payment attempt not found")
		}
		return PaymentForSale{}, internalError("Failed to load online payment attempt", err)
	}

	if providerSaleRequestedAt.Valid {
		payment.ProviderSaleRequestedAt = &providerSaleRequestedAt.Time
	}

	return payment, nil
}

func (r *Repo) ClaimSaleRequest(
	ctx context.Context,
	paymentID string,
	providerRequestID string,
	terminalTID string,
	requestedAt time.Time,
) (SaleRequestClaim, error) {
	const query = `
UPDATE payments
SET gateway = 'HDFC',
	provider_request_id = COALESCE(provider_request_id, $2),
	terminal_tid = COALESCE(terminal_tid, $3),
	provider_sale_requested_at = COALESCE(provider_sale_requested_at, $4),
	updated_at = $4
WHERE id = $1
  AND payment_method = 'ONLINE'
  AND status = 'INITIATED'
  AND provider_sale_requested_at IS NULL
RETURNING provider_request_id, terminal_tid, provider_sale_requested_at`

	var claim SaleRequestClaim
	err := r.db.QueryRowContext(ctx, query, paymentID, providerRequestID, terminalTID, requestedAt).Scan(
		&claim.ProviderRequestID,
		&claim.TerminalTID,
		&claim.RequestedAt,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return SaleRequestClaim{}, apperrors.New(apperrors.CodeInvalidRequest, "Online payment attempt is not ready for HDFC sale initiation")
		}
		return SaleRequestClaim{}, internalError("Failed to mark HDFC sale request", err)
	}

	return claim, nil
}

func (r *Repo) UpdatePaymentAfterSale(ctx context.Context, input SaleUpdateInput) error {
	return r.updatePaymentAfterSale(ctx, r.db, input)
}

func (r *Repo) MarkSaleRequestUnresolved(ctx context.Context, paymentID string, updatedAt time.Time) error {
	const query = `
UPDATE payments
SET provider_status_code = COALESCE(provider_status_code, 'SALE_REQUEST_UNRESOLVED'),
	provider_status_message = COALESCE(provider_status_message, 'HDFC sale request outcome is unresolved'),
	response_payload = COALESCE(response_payload, '{"sale_request":"unresolved"}'::jsonb),
	updated_at = $2
WHERE id = $1
  AND payment_method = 'ONLINE'`

	result, err := r.db.ExecContext(ctx, query, paymentID, updatedAt)
	if err != nil {
		return internalError("Failed to mark unresolved HDFC sale request", err)
	}
	if err := requireExactlyOneRow(result, "unresolved HDFC sale request update"); err != nil {
		return err
	}
	return nil
}

func (r *Repo) UpdatePaymentAfterSaleAndRecomputeBill(ctx context.Context, input SaleUpdateInput) error {
	return platformdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.updatePaymentAfterSale(ctx, tx, input); err != nil {
			return err
		}
		return r.recomputeBillPaymentState(ctx, tx, input.BillID, input.UpdatedAt)
	})
}

func (r *Repo) updatePaymentAfterSale(ctx context.Context, runner queryer, input SaleUpdateInput) error {
	responsePayload := input.ResponsePayload
	if len(responsePayload) == 0 {
		responsePayload = []byte("{}")
	}

	const query = `
UPDATE payments
SET gateway = 'HDFC',
	status = $2,
	provider_request_id = $3,
	provider_txn_id = NULLIF($4, ''),
	terminal_tid = $5,
	provider_status_code = NULLIF($6, ''),
	provider_status_message = NULLIF($7, ''),
	provider_txn_status = NULLIF($8, ''),
	provider_txn_message = NULLIF($9, ''),
	response_payload = $10::jsonb,
	provider_confirmed_at = $11,
	verified_at = $12,
	updated_at = $13
WHERE id = $1`

	result, err := runner.ExecContext(
		ctx,
		query,
		input.PaymentID,
		input.Status,
		input.ProviderRequestID,
		input.ProviderTxnID,
		input.TerminalTID,
		input.ProviderStatusCode,
		input.ProviderStatusMessage,
		input.ProviderTxnStatus,
		input.ProviderTxnMessage,
		string(responsePayload),
		input.ConfirmedAt,
		input.VerifiedAt,
		input.UpdatedAt,
	)
	if err != nil {
		return internalError("Failed to update HDFC sale result", err)
	}
	if err := requireExactlyOneRow(result, "HDFC sale result update"); err != nil {
		return err
	}

	return nil
}

func (r *Repo) RecomputeBillPaymentState(ctx context.Context, billID string, updatedAt time.Time) error {
	return r.recomputeBillPaymentState(ctx, r.db, billID, updatedAt)
}

func (r *Repo) recomputeBillPaymentState(ctx context.Context, runner queryer, billID string, updatedAt time.Time) error {
	const query = `
WITH payment_totals AS (
	SELECT
		b.id,
		COALESCE(SUM(CASE WHEN p.status = 'SUCCESS' THEN p.amount ELSE 0 END), 0) AS paid_amount,
		COUNT(*) FILTER (WHERE p.status IN ('INITIATED', 'PENDING')) AS unresolved_count
	FROM bills b
	LEFT JOIN payments p
		ON p.bill_id = b.id
	WHERE b.id = $1
	GROUP BY b.id
)
UPDATE bills b
SET amount_paid = LEAST(payment_totals.paid_amount, b.total_amount),
	amount_due = GREATEST(b.total_amount - payment_totals.paid_amount, 0),
	status = CASE
		WHEN payment_totals.paid_amount >= b.total_amount THEN 'PAID'
		WHEN b.payment_mode_summary = 'ONLINE' AND payment_totals.unresolved_count = 0 THEN 'PAYMENT_FAILED'
		WHEN b.payment_mode_summary = 'ONLINE' THEN 'PAYMENT_PENDING'
		WHEN b.payment_mode_summary = 'SPLIT' THEN 'PARTIALLY_PAID'
		ELSE b.status
	END,
	paid_at = CASE
		WHEN payment_totals.paid_amount >= b.total_amount THEN COALESCE(b.paid_at, $2)
		ELSE b.paid_at
	END
FROM payment_totals
WHERE b.id = payment_totals.id`

	result, err := runner.ExecContext(ctx, query, billID, updatedAt)
	if err != nil {
		return internalError("Failed to recompute bill payment state", err)
	}
	if err := requireExactlyOneRow(result, "bill payment state recompute"); err != nil {
		return err
	}

	return nil
}

func validateTerminalTID(tid string) error {
	tid = strings.TrimSpace(tid)
	if len(tid) != 8 {
		return apperrors.New(apperrors.CodeInvalidRequest, "Store is missing valid HDFC terminal configuration")
	}
	return nil
}

func internalError(message string, err error) error {
	return apperrors.NewWithDetails(
		apperrors.CodeInternalError,
		message,
		map[string]any{"reason": err.Error()},
	)
}

func requireExactlyOneRow(result sql.Result, operation string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return internalError("Failed to verify "+operation, err)
	}
	if rowsAffected != 1 {
		return apperrors.New(apperrors.CodeInternalError, operation+" did not affect exactly one row")
	}
	return nil
}
