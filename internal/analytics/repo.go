package analytics

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	apperrors "gloss/internal/shared/errors"
)

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) StoreBelongsToTenant(ctx context.Context, tenantID string, storeID string) (bool, error) {
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
		return false, internalError("Failed to validate store scope", err)
	}
	return true, nil
}

func (r *Repo) ListAdminBills(ctx context.Context, tenantID string, filters adminBillFilters) ([]AdminBillRow, error) {
	whereClause, args := buildBillFilterWhereClause(tenantID, filters.StoreID, filters.DateFrom, filters.DateTo, filters.Status)
	limitPlaceholder := len(args) + 1
	offsetPlaceholder := len(args) + 2
	args = append(args, filters.Limit, filters.Offset)

	query := `
SELECT
	b.id::text,
	b.bill_number,
	b.created_at,
	s.id::text,
	s.name,
	b.status,
	b.total_amount,
	b.amount_paid,
	b.amount_due,
	b.payment_mode_summary,
	b.cancellation_reason
FROM bills b
INNER JOIN stores s
	ON s.id = b.store_id
	AND s.tenant_id = b.tenant_id
` + whereClause + `
ORDER BY b.created_at DESC, b.id DESC
LIMIT ` + placeholder(limitPlaceholder) + ` OFFSET ` + placeholder(offsetPlaceholder)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, internalError("Failed to load admin bills", err)
	}
	defer rows.Close()

	bills := make([]AdminBillRow, 0)
	for rows.Next() {
		var (
			bill               AdminBillRow
			cancellationReason sql.NullString
		)
		if err := rows.Scan(
			&bill.BillID,
			&bill.BillNumber,
			&bill.CreatedAt,
			&bill.StoreID,
			&bill.StoreName,
			&bill.Status,
			&bill.TotalAmount,
			&bill.AmountPaid,
			&bill.AmountDue,
			&bill.PaymentModeSummary,
			&cancellationReason,
		); err != nil {
			return nil, internalError("Failed to scan admin bills", err)
		}
		if cancellationReason.Valid {
			reason := cancellationReason.String
			bill.CancellationReason = &reason
		}
		bills = append(bills, bill)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("Failed while reading admin bills", err)
	}

	return bills, nil
}

func (r *Repo) GetAdminAnalyticsSummary(
	ctx context.Context,
	tenantID string,
	filters analyticsSummaryFilters,
) (AnalyticsSummary, error) {
	whereClause, args := buildBillFilterWhereClause(tenantID, filters.StoreID, filters.DateFrom, filters.DateTo, filters.Status)

	query := `
WITH filtered_bills AS (
	SELECT
		b.id,
		b.status,
		b.total_amount,
		b.amount_paid,
		b.tax_amount,
		b.tip_amount
	FROM bills b
	INNER JOIN stores s
		ON s.id = b.store_id
		AND s.tenant_id = b.tenant_id
	` + whereClause + `
),
commission_totals AS (
	SELECT COALESCE(SUM(cl.commission_amount), 0) AS total_commission
	FROM commission_ledger cl
	INNER JOIN filtered_bills fb
		ON fb.id = cl.bill_id
	WHERE fb.status <> 'CANCELLED'
)
SELECT
	COUNT(*)::bigint AS total_bills,
	COALESCE(SUM(CASE WHEN status <> 'CANCELLED' THEN amount_paid ELSE 0 END), 0)::bigint AS total_sales,
	COUNT(*) FILTER (WHERE status = 'CANCELLED')::bigint AS cancelled_bill_count,
	COALESCE(SUM(CASE WHEN status = 'CANCELLED' THEN total_amount ELSE 0 END), 0)::bigint AS cancelled_amount,
	COALESCE(SUM(CASE WHEN status <> 'CANCELLED' THEN tax_amount ELSE 0 END), 0)::bigint AS total_tax,
	(SELECT total_commission FROM commission_totals)::bigint AS total_commission,
	COALESCE(SUM(CASE WHEN status <> 'CANCELLED' THEN tip_amount ELSE 0 END), 0)::bigint AS total_tip
FROM filtered_bills`

	var summary AnalyticsSummary
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.TotalBills,
		&summary.TotalSales,
		&summary.CancelledBillCount,
		&summary.CancelledAmount,
		&summary.TotalTax,
		&summary.TotalCommission,
		&summary.TotalTip,
	)
	if err != nil {
		return AnalyticsSummary{}, internalError("Failed to load analytics summary", err)
	}

	return summary, nil
}

func buildBillFilterWhereClause(
	tenantID string,
	storeID string,
	dateFrom *time.Time,
	dateTo *time.Time,
	status string,
) (string, []any) {
	conditions := []string{"b.tenant_id = $1"}
	args := []any{tenantID}

	if storeID != "" {
		args = append(args, storeID)
		conditions = append(conditions, "b.store_id = "+placeholder(len(args)))
	}
	if dateFrom != nil {
		args = append(args, *dateFrom)
		conditions = append(conditions, "b.created_at >= "+placeholder(len(args)))
	}
	if dateTo != nil {
		args = append(args, *dateTo)
		conditions = append(conditions, "b.created_at < "+placeholder(len(args)))
	}
	if status != "" {
		args = append(args, status)
		conditions = append(conditions, "b.status = "+placeholder(len(args)))
	}

	return "WHERE " + strings.Join(conditions, "\n  AND "), args
}

func placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

func internalError(message string, err error) error {
	return apperrors.NewWithDetails(
		apperrors.CodeInternalError,
		message,
		map[string]any{"reason": err.Error()},
	)
}
