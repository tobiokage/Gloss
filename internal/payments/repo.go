package payments

import (
	"context"
	"database/sql"
	stderrors "errors"
	"strings"
	"time"

	platformdb "gloss/internal/platform/db"
	"gloss/internal/shared/enums"
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

func (r *Repo) GetPaymentAttemptForCancel(
	ctx context.Context,
	tenantID string,
	storeID string,
	billID string,
	paymentID string,
) (PaymentAttemptForCancel, error) {
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
	COALESCE(p.gateway, ''),
	COALESCE(p.provider_request_id, ''),
	COALESCE(p.provider_txn_id, ''),
	COALESCE(p.terminal_tid, '')
FROM payments p
INNER JOIN bills b
	ON b.id = p.bill_id
WHERE p.id = $1
  AND p.bill_id = $2
  AND b.tenant_id = $3
  AND b.store_id = $4
  AND p.payment_method = 'ONLINE'
LIMIT 1`

	var payment PaymentAttemptForCancel
	err := r.db.QueryRowContext(ctx, query, paymentID, billID, tenantID, storeID).Scan(
		&payment.ID,
		&payment.BillID,
		&payment.TenantID,
		&payment.StoreID,
		&payment.BillNumber,
		&payment.PaymentMode,
		&payment.Amount,
		&payment.Status,
		&payment.Gateway,
		&payment.ProviderRequestID,
		&payment.ProviderTxnID,
		&payment.TerminalTID,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return PaymentAttemptForCancel{}, apperrors.New(apperrors.CodeNotFound, "Online payment attempt not found")
		}
		return PaymentAttemptForCancel{}, internalError("Failed to load online payment attempt", err)
	}

	return payment, nil
}

func (r *Repo) FindPendingHDFCPaymentForBill(
	ctx context.Context,
	tenantID string,
	storeID string,
	billID string,
) (PaymentForStatusSync, bool, error) {
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
	COALESCE(p.gateway, ''),
	COALESCE(p.provider_request_id, ''),
	COALESCE(p.provider_txn_id, ''),
	COALESCE(p.terminal_tid, '')
FROM payments p
INNER JOIN bills b
	ON b.id = p.bill_id
WHERE p.bill_id = $1
  AND b.tenant_id = $2
  AND b.store_id = $3
  AND p.payment_method = 'ONLINE'
  AND p.gateway = 'HDFC'
  AND p.status IN ('INITIATED', 'PENDING')
  AND b.status <> 'CANCELLED'
ORDER BY p.created_at DESC, p.id DESC
LIMIT 1`

	var payment PaymentForStatusSync
	err := r.db.QueryRowContext(ctx, query, billID, tenantID, storeID).Scan(
		&payment.ID,
		&payment.BillID,
		&payment.TenantID,
		&payment.StoreID,
		&payment.BillNumber,
		&payment.PaymentMode,
		&payment.Amount,
		&payment.Status,
		&payment.Gateway,
		&payment.ProviderRequestID,
		&payment.ProviderTxnID,
		&payment.TerminalTID,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return PaymentForStatusSync{}, false, nil
		}
		return PaymentForStatusSync{}, false, internalError("Failed to load pending HDFC payment", err)
	}

	return payment, true, nil
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

func (r *Repo) UpdatePaymentAttemptCancellation(ctx context.Context, tenantID string, storeID string, input CancelAttemptUpdateInput) error {
	return platformdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		currentStatus, err := r.lockPaymentAttemptForCancellation(ctx, tx, tenantID, storeID, input.PaymentID, input.BillID)
		if err != nil {
			return err
		}
		if currentStatus != string(enums.PaymentStatusInitiated) && currentStatus != string(enums.PaymentStatusPending) {
			return apperrors.New(apperrors.CodeInvalidRequest, "Payment attempt is not provider-cancellable")
		}
		if err := r.updatePaymentAttemptCancellation(ctx, tx, tenantID, storeID, input); err != nil {
			return err
		}
		return r.recomputeBillPaymentState(ctx, tx, input.BillID, input.UpdatedAt)
	})
}

func (r *Repo) ApplyHDFCStatusSync(
	ctx context.Context,
	tenantID string,
	storeID string,
	input StatusSyncUpdateInput,
) (StatusSyncApplyResult, error) {
	var result StatusSyncApplyResult
	err := platformdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		currentStatus, billStatus, err := r.lockPaymentAndBillForStatusSync(
			ctx,
			tx,
			tenantID,
			storeID,
			input.PaymentID,
			input.BillID,
		)
		if err != nil {
			return err
		}

		result = StatusSyncApplyResult{
			PaymentID:        input.PaymentID,
			OldPaymentStatus: currentStatus,
			NewPaymentStatus: currentStatus,
			BillStatus:       billStatus,
		}
		if billStatus == string(enums.BillStatusCancelled) {
			return nil
		}
		if isTerminalPaymentStatus(currentStatus) {
			return nil
		}

		if err := r.updatePaymentAfterStatusSync(ctx, tx, tenantID, storeID, input); err != nil {
			return err
		}
		if isTerminalPaymentStatus(input.Status) {
			if err := r.recomputeBillPaymentState(ctx, tx, input.BillID, input.UpdatedAt); err != nil {
				return err
			}
			result.TransitionApplied = currentStatus != input.Status
		}
		result.NewPaymentStatus = input.Status
		return nil
	})
	if err != nil {
		return StatusSyncApplyResult{}, err
	}
	return result, nil
}

func (r *Repo) lockPaymentAttemptForCancellation(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	paymentID string,
	billID string,
) (string, error) {
	const query = `
SELECT p.status
FROM payments p
INNER JOIN bills b
	ON b.id = p.bill_id
WHERE p.id = $1
  AND p.bill_id = $2
  AND b.tenant_id = $3
  AND b.store_id = $4
  AND p.payment_method = 'ONLINE'
FOR UPDATE OF p, b`

	var status string
	err := tx.QueryRowContext(ctx, query, paymentID, billID, tenantID, storeID).Scan(&status)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return "", apperrors.New(apperrors.CodeNotFound, "Online payment attempt not found")
		}
		return "", internalError("Failed to lock online payment attempt", err)
	}
	return status, nil
}

func (r *Repo) lockPaymentAndBillForStatusSync(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	paymentID string,
	billID string,
) (string, string, error) {
	const query = `
SELECT p.status, b.status
FROM payments p
INNER JOIN bills b
	ON b.id = p.bill_id
WHERE p.id = $1
  AND p.bill_id = $2
  AND b.tenant_id = $3
  AND b.store_id = $4
  AND p.payment_method = 'ONLINE'
  AND p.gateway = 'HDFC'
FOR UPDATE OF p, b`

	var paymentStatus string
	var billStatus string
	err := tx.QueryRowContext(ctx, query, paymentID, billID, tenantID, storeID).Scan(&paymentStatus, &billStatus)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return "", "", apperrors.New(apperrors.CodeNotFound, "HDFC payment attempt not found")
		}
		return "", "", internalError("Failed to lock HDFC payment attempt", err)
	}
	return paymentStatus, billStatus, nil
}

func (r *Repo) updatePaymentAttemptCancellation(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	input CancelAttemptUpdateInput,
) error {
	cancelResponsePayload := input.CancelResponsePayload
	if len(cancelResponsePayload) == 0 {
		cancelResponsePayload = []byte("{}")
	}

	const query = `
UPDATE payments
SET status = $2,
	provider_request_id = NULLIF($3, ''),
	provider_txn_id = NULLIF($4, ''),
	terminal_tid = $5,
	provider_status_code = NULLIF($6, ''),
	provider_status_message = NULLIF($7, ''),
	provider_txn_status = NULLIF($8, ''),
	provider_txn_message = NULLIF($9, ''),
	cancel_response_payload = $10::jsonb,
	updated_at = $11
FROM bills b
WHERE payments.id = $1
  AND payments.bill_id = $12
  AND b.id = payments.bill_id
  AND b.tenant_id = $13
  AND b.store_id = $14
  AND payments.payment_method = 'ONLINE'`

	result, err := tx.ExecContext(
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
		string(cancelResponsePayload),
		input.UpdatedAt,
		input.BillID,
		tenantID,
		storeID,
	)
	if err != nil {
		return internalError("Failed to update HDFC cancellation result", err)
	}
	if err := requireExactlyOneRow(result, "HDFC cancellation result update"); err != nil {
		return err
	}
	return nil
}

func (r *Repo) updatePaymentAfterStatusSync(
	ctx context.Context,
	tx *sql.Tx,
	tenantID string,
	storeID string,
	input StatusSyncUpdateInput,
) error {
	statusResponsePayload := input.StatusResponsePayload
	if len(statusResponsePayload) == 0 {
		statusResponsePayload = []byte("{}")
	}

	const query = `
UPDATE payments
SET status = $2,
	provider_request_id = COALESCE(NULLIF($3, ''), provider_request_id),
	provider_txn_id = COALESCE(NULLIF($4, ''), provider_txn_id),
	terminal_tid = COALESCE(NULLIF($5, ''), terminal_tid),
	provider_status_code = NULLIF($6, ''),
	provider_status_message = NULLIF($7, ''),
	provider_txn_status = NULLIF($8, ''),
	provider_txn_message = NULLIF($9, ''),
	actual_completion_mode = COALESCE(NULLIF($10, ''), actual_completion_mode),
	status_details_payload = $11::jsonb,
	last_status_checked_at = $12,
	provider_confirmed_at = COALESCE(provider_confirmed_at, $13),
	verified_at = COALESCE(verified_at, $14),
	updated_at = $15
FROM bills b
WHERE payments.id = $1
  AND payments.bill_id = $16
  AND b.id = payments.bill_id
  AND b.tenant_id = $17
  AND b.store_id = $18
  AND payments.payment_method = 'ONLINE'
  AND payments.gateway = 'HDFC'`

	result, err := tx.ExecContext(
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
		input.ActualCompletionMode,
		string(statusResponsePayload),
		input.LastStatusCheckedAt,
		input.ProviderConfirmedAt,
		input.VerifiedAt,
		input.UpdatedAt,
		input.BillID,
		tenantID,
		storeID,
	)
	if err != nil {
		return internalError("Failed to update HDFC status result", err)
	}
	if err := requireExactlyOneRow(result, "HDFC status result update"); err != nil {
		return err
	}
	return nil
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
		WHEN b.status = 'CANCELLED' THEN b.status
		WHEN payment_totals.paid_amount >= b.total_amount THEN 'PAID'
		WHEN b.payment_mode_summary = 'ONLINE' AND payment_totals.unresolved_count = 0 THEN 'PAYMENT_FAILED'
		WHEN b.payment_mode_summary = 'ONLINE' THEN 'PAYMENT_PENDING'
		WHEN b.payment_mode_summary = 'SPLIT' THEN 'PARTIALLY_PAID'
		ELSE b.status
	END,
	paid_at = CASE
		WHEN b.status = 'CANCELLED' THEN b.paid_at
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
