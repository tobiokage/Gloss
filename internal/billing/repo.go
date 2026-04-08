package billing

import (
	"context"
	"crypto/rand"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	apperrors "gloss/internal/shared/errors"
)

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) GetActiveStoreSnapshot(
	ctx context.Context,
	runner queryer,
	tenantID string,
	storeID string,
) (StoreSnapshot, error) {
	const query = `
SELECT
	s.id::text,
	s.tenant_id::text,
	s.name,
	s.code,
	s.location
FROM stores s
WHERE s.id = $1
  AND s.tenant_id = $2
  AND s.active = TRUE
LIMIT 1`

	var store StoreSnapshot
	err := runner.QueryRowContext(ctx, query, storeID, tenantID).Scan(
		&store.ID,
		&store.TenantID,
		&store.Name,
		&store.Code,
		&store.Location,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return StoreSnapshot{}, apperrors.New(apperrors.CodeNotFound, "Store not found")
		}
		return StoreSnapshot{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load store",
			map[string]any{"reason": err.Error()},
		)
	}

	return store, nil
}

func (r *Repo) GetActiveCatalogueLinesByIDs(
	ctx context.Context,
	runner queryer,
	tenantID string,
	catalogueItemIDs []string,
) (map[string]AuthoritativeCatalogueLine, error) {
	uniqueIDs := uniqueStrings(catalogueItemIDs)
	if len(uniqueIDs) == 0 {
		return map[string]AuthoritativeCatalogueLine{}, nil
	}

	inClause, args := buildInClause(2, uniqueIDs)
	query := `
SELECT
	id::text,
	name,
	list_price
FROM catalogue_items
WHERE tenant_id = $1
  AND active = TRUE
  AND id IN (` + inClause + `)
ORDER BY id ASC`

	rows, err := runner.QueryContext(ctx, query, append([]any{tenantID}, args...)...)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load catalogue items for billing",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	lines := make(map[string]AuthoritativeCatalogueLine, len(uniqueIDs))
	for rows.Next() {
		var line AuthoritativeCatalogueLine
		if err := rows.Scan(&line.CatalogueItemID, &line.ServiceName, &line.UnitPrice); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan catalogue items for billing",
				map[string]any{"reason": err.Error()},
			)
		}
		lines[line.CatalogueItemID] = line
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading catalogue items for billing",
			map[string]any{"reason": err.Error()},
		)
	}

	return lines, nil
}

func (r *Repo) GetActiveStoreStaffByIDs(
	ctx context.Context,
	runner queryer,
	tenantID string,
	storeID string,
	staffIDs []string,
) (map[string]AuthoritativeStaffMember, error) {
	uniqueIDs := uniqueStrings(staffIDs)
	if len(uniqueIDs) == 0 {
		return map[string]AuthoritativeStaffMember{}, nil
	}

	inClause, args := buildInClause(3, uniqueIDs)
	query := `
SELECT
	s.id::text
FROM staff s
INNER JOIN staff_store_mapping ssm
	ON ssm.staff_id = s.id
INNER JOIN stores st
	ON st.id = ssm.store_id
WHERE s.tenant_id = $1
  AND ssm.store_id = $2
  AND s.active = TRUE
  AND ssm.active = TRUE
  AND st.tenant_id = $1
  AND s.id IN (` + inClause + `)
ORDER BY s.id ASC`

	rows, err := runner.QueryContext(ctx, query, append([]any{tenantID, storeID}, args...)...)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to validate store staff for billing",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	staffByID := make(map[string]AuthoritativeStaffMember, len(uniqueIDs))
	for rows.Next() {
		var member AuthoritativeStaffMember
		if err := rows.Scan(&member.ID); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan store staff for billing",
				map[string]any{"reason": err.Error()},
			)
		}
		staffByID[member.ID] = member
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading store staff for billing",
			map[string]any{"reason": err.Error()},
		)
	}

	return staffByID, nil
}

func (r *Repo) LockAndIncrementBillCounter(ctx context.Context, tx *sql.Tx, storeID string, updatedAt time.Time) (int64, error) {
	const query = `
UPDATE store_bill_counters
SET last_bill_seq = last_bill_seq + 1,
	updated_at = $2
WHERE store_id = $1
RETURNING last_bill_seq`

	var sequence int64
	err := tx.QueryRowContext(ctx, query, storeID, updatedAt).Scan(&sequence)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return 0, apperrors.New(apperrors.CodeNotFound, "Store bill counter not found")
		}
		return 0, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to generate bill number",
			map[string]any{"reason": err.Error()},
		)
	}

	return sequence, nil
}

func (r *Repo) InsertBill(ctx context.Context, tx *sql.Tx, input InsertBillInput) error {
	const query = `
INSERT INTO bills (
	id,
	tenant_id,
	store_id,
	bill_number,
	status,
	service_gross_amount,
	discount_amount,
	service_net_amount,
	tip_amount,
	taxable_base_amount,
	tax_amount,
	total_amount,
	amount_paid,
	amount_due,
	payment_mode_summary,
	created_by_user_id,
	created_at,
	paid_at
)
VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15, $16, $17, $18
)`

	if _, err := tx.ExecContext(
		ctx,
		query,
		input.ID,
		input.TenantID,
		input.StoreID,
		input.BillNumber,
		input.Status,
		input.ServiceGrossAmount,
		input.DiscountAmount,
		input.ServiceNetAmount,
		input.TipAmount,
		input.TaxableBaseAmount,
		input.TaxAmount,
		input.TotalAmount,
		input.AmountPaid,
		input.AmountDue,
		input.PaymentModeSummary,
		input.CreatedByUserID,
		input.CreatedAt,
		input.PaidAt,
	); err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create bill",
			map[string]any{"reason": err.Error()},
		)
	}

	return nil
}

func (r *Repo) InsertBillItems(
	ctx context.Context,
	tx *sql.Tx,
	billID string,
	lines []CalculatedBillLine,
	createdAt time.Time,
) ([]PersistedBillItem, error) {
	const query = `
INSERT INTO bill_items (
	id,
	bill_id,
	catalogue_item_id,
	service_name_snapshot,
	unit_price_snapshot,
	quantity,
	line_gross_amount,
	line_discount_amount,
	line_net_amount,
	taxable_base_amount,
	tax_amount,
	assigned_staff_id,
	commission_base_amount,
	commission_amount,
	created_at
)
VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
	$11, $12, $13, $14, $15
)`

	items := make([]PersistedBillItem, 0, len(lines))
	for _, line := range lines {
		itemID, err := newUUIDString()
		if err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create bill item",
				map[string]any{"reason": err.Error()},
			)
		}

		if _, err := tx.ExecContext(
			ctx,
			query,
			itemID,
			billID,
			line.CatalogueItemID,
			line.ServiceName,
			line.UnitPrice,
			line.Quantity,
			line.LineGrossAmount,
			line.LineDiscountAmount,
			line.LineNetAmount,
			line.TaxableBaseAmount,
			line.TaxAmount,
			line.AssignedStaffID,
			line.CommissionBaseAmount,
			line.CommissionAmount,
			createdAt,
		); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create bill item",
				map[string]any{"reason": err.Error()},
			)
		}

		items = append(items, PersistedBillItem{
			ID:                   itemID,
			CatalogueItemID:      line.CatalogueItemID,
			AssignedStaffID:      line.AssignedStaffID,
			ServiceName:          line.ServiceName,
			UnitPrice:            line.UnitPrice,
			Quantity:             line.Quantity,
			LineGrossAmount:      line.LineGrossAmount,
			LineDiscountAmount:   line.LineDiscountAmount,
			LineNetAmount:        line.LineNetAmount,
			TaxableBaseAmount:    line.TaxableBaseAmount,
			TaxAmount:            line.TaxAmount,
			CommissionBaseAmount: line.CommissionBaseAmount,
			CommissionAmount:     line.CommissionAmount,
		})
	}

	return items, nil
}

func (r *Repo) InsertBillTipAllocations(ctx context.Context, tx *sql.Tx, allocations []InsertTipAllocationInput) error {
	if len(allocations) == 0 {
		return nil
	}

	const query = `
INSERT INTO bill_tip_allocations (
	id,
	bill_id,
	staff_id,
	tip_amount,
	created_at
)
VALUES ($1, $2, $3, $4, $5)`

	for _, allocation := range allocations {
		if _, err := tx.ExecContext(
			ctx,
			query,
			allocation.ID,
			allocation.BillID,
			allocation.StaffID,
			allocation.TipAmount,
			allocation.CreatedAt,
		); err != nil {
			return apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create bill tip allocation",
				map[string]any{"reason": err.Error()},
			)
		}
	}

	return nil
}

func (r *Repo) InsertCommissionLedgerRows(ctx context.Context, tx *sql.Tx, rows []InsertCommissionLedgerInput) error {
	if len(rows) == 0 {
		return nil
	}

	const query = `
INSERT INTO commission_ledger (
	id,
	bill_id,
	bill_item_id,
	staff_id,
	base_amount,
	commission_percent_bps,
	commission_amount,
	created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	for _, row := range rows {
		if _, err := tx.ExecContext(
			ctx,
			query,
			row.ID,
			row.BillID,
			row.BillItemID,
			row.StaffID,
			row.BaseAmount,
			row.CommissionPercentBPS,
			row.CommissionAmount,
			row.CreatedAt,
		); err != nil {
			return apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to create commission ledger row",
				map[string]any{"reason": err.Error()},
			)
		}
	}

	return nil
}

func (r *Repo) InsertPayment(ctx context.Context, tx *sql.Tx, input InsertPaymentInput) error {
	const query = `
INSERT INTO payments (
	id,
	bill_id,
	gateway,
	payment_method,
	amount,
	status,
	gateway_order_id,
	gateway_txn_id,
	gateway_reference,
	request_payload,
	response_payload,
	verified_at,
	created_at,
	updated_at
)
VALUES (
	$1, $2, NULL, $3, $4, $5, NULL, NULL, NULL, NULL, NULL, $6, $7, $8
)`

	if _, err := tx.ExecContext(
		ctx,
		query,
		input.ID,
		input.BillID,
		input.PaymentMethod,
		input.Amount,
		input.Status,
		input.VerifiedAt,
		input.CreatedAt,
		input.UpdatedAt,
	); err != nil {
		return apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to create payment row",
			map[string]any{"reason": err.Error()},
		)
	}

	return nil
}

func (r *Repo) GetBillGraph(ctx context.Context, billID string, tenantID string, storeID string) (BillGraph, error) {
	graph, err := r.getBillHeader(ctx, billID, tenantID, storeID)
	if err != nil {
		return BillGraph{}, err
	}

	items, err := r.getBillItems(ctx, billID)
	if err != nil {
		return BillGraph{}, err
	}
	graph.Items = items

	tipAllocations, err := r.getBillTipAllocations(ctx, billID)
	if err != nil {
		return BillGraph{}, err
	}
	graph.TipAllocations = tipAllocations

	payments, err := r.getBillPayments(ctx, billID)
	if err != nil {
		return BillGraph{}, err
	}
	graph.Payments = payments

	return graph, nil
}

func (r *Repo) getBillHeader(ctx context.Context, billID string, tenantID string, storeID string) (BillGraph, error) {
	const query = `
SELECT
	b.id::text,
	b.bill_number,
	b.status,
	b.payment_mode_summary,
	b.service_gross_amount,
	b.discount_amount,
	b.service_net_amount,
	b.tip_amount,
	b.taxable_base_amount,
	b.tax_amount,
	b.total_amount,
	b.amount_paid,
	b.amount_due,
	b.created_at,
	b.paid_at,
	s.id::text,
	s.tenant_id::text,
	s.name,
	s.code,
	s.location
FROM bills b
INNER JOIN stores s
	ON s.id = b.store_id
WHERE b.id = $1
  AND b.tenant_id = $2
  AND b.store_id = $3
LIMIT 1`

	var (
		graph  BillGraph
		paidAt sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, query, billID, tenantID, storeID).Scan(
		&graph.Bill.ID,
		&graph.Bill.BillNumber,
		&graph.Bill.Status,
		&graph.Bill.PaymentModeSummary,
		&graph.Bill.ServiceGrossAmount,
		&graph.Bill.DiscountAmount,
		&graph.Bill.ServiceNetAmount,
		&graph.Bill.TipAmount,
		&graph.Bill.TaxableBaseAmount,
		&graph.Bill.TaxAmount,
		&graph.Bill.TotalAmount,
		&graph.Bill.AmountPaid,
		&graph.Bill.AmountDue,
		&graph.Bill.CreatedAt,
		&paidAt,
		&graph.Store.ID,
		&graph.Store.TenantID,
		&graph.Store.Name,
		&graph.Store.Code,
		&graph.Store.Location,
	)
	if err != nil {
		if stderrors.Is(err, sql.ErrNoRows) {
			return BillGraph{}, apperrors.New(apperrors.CodeNotFound, "Bill not found")
		}
		return BillGraph{}, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load bill",
			map[string]any{"reason": err.Error()},
		)
	}

	if paidAt.Valid {
		paidAtValue := paidAt.Time
		graph.Bill.PaidAt = &paidAtValue
	}

	return graph, nil
}

func (r *Repo) getBillItems(ctx context.Context, billID string) ([]BillItemRecord, error) {
	const query = `
SELECT
	id::text,
	catalogue_item_id::text,
	service_name_snapshot,
	assigned_staff_id::text,
	unit_price_snapshot,
	quantity,
	line_gross_amount,
	line_discount_amount,
	line_net_amount,
	taxable_base_amount,
	tax_amount,
	commission_base_amount,
	commission_amount
FROM bill_items
WHERE bill_id = $1
ORDER BY created_at ASC, id ASC`

	rows, err := r.db.QueryContext(ctx, query, billID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load bill items",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	items := make([]BillItemRecord, 0)
	for rows.Next() {
		var item BillItemRecord
		if err := rows.Scan(
			&item.ID,
			&item.CatalogueItemID,
			&item.ServiceName,
			&item.AssignedStaffID,
			&item.UnitPrice,
			&item.Quantity,
			&item.LineGrossAmount,
			&item.LineDiscountAmount,
			&item.LineNetAmount,
			&item.TaxableBaseAmount,
			&item.TaxAmount,
			&item.CommissionBaseAmount,
			&item.CommissionAmount,
		); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan bill items",
				map[string]any{"reason": err.Error()},
			)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading bill items",
			map[string]any{"reason": err.Error()},
		)
	}

	return items, nil
}

func (r *Repo) getBillTipAllocations(ctx context.Context, billID string) ([]BillTipAllocationRecord, error) {
	const query = `
SELECT
	id::text,
	staff_id::text,
	tip_amount
FROM bill_tip_allocations
WHERE bill_id = $1
ORDER BY created_at ASC, id ASC`

	rows, err := r.db.QueryContext(ctx, query, billID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load bill tip allocations",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	allocations := make([]BillTipAllocationRecord, 0)
	for rows.Next() {
		var allocation BillTipAllocationRecord
		if err := rows.Scan(&allocation.ID, &allocation.StaffID, &allocation.TipAmount); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan bill tip allocations",
				map[string]any{"reason": err.Error()},
			)
		}
		allocations = append(allocations, allocation)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading bill tip allocations",
			map[string]any{"reason": err.Error()},
		)
	}

	return allocations, nil
}

func (r *Repo) getBillPayments(ctx context.Context, billID string) ([]BillPaymentRecord, error) {
	const query = `
SELECT
	id::text,
	gateway,
	payment_method,
	amount,
	status,
	created_at,
	updated_at,
	verified_at
FROM payments
WHERE bill_id = $1
ORDER BY created_at ASC, id ASC`

	rows, err := r.db.QueryContext(ctx, query, billID)
	if err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed to load payments",
			map[string]any{"reason": err.Error()},
		)
	}
	defer rows.Close()

	payments := make([]BillPaymentRecord, 0)
	for rows.Next() {
		var (
			payment    BillPaymentRecord
			gateway    sql.NullString
			verifiedAt sql.NullTime
		)
		if err := rows.Scan(
			&payment.ID,
			&gateway,
			&payment.PaymentMethod,
			&payment.Amount,
			&payment.Status,
			&payment.CreatedAt,
			&payment.UpdatedAt,
			&verifiedAt,
		); err != nil {
			return nil, apperrors.NewWithDetails(
				apperrors.CodeInternalError,
				"Failed to scan payments",
				map[string]any{"reason": err.Error()},
			)
		}
		if gateway.Valid {
			gatewayValue := gateway.String
			payment.Gateway = &gatewayValue
		}
		if verifiedAt.Valid {
			verifiedAtValue := verifiedAt.Time
			payment.VerifiedAt = &verifiedAtValue
		}
		payments = append(payments, payment)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.NewWithDetails(
			apperrors.CodeInternalError,
			"Failed while reading payments",
			map[string]any{"reason": err.Error()},
		)
	}

	return payments, nil
}

func buildInClause(startIndex int, values []string) (string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for i, value := range values {
		placeholders = append(placeholders, fmt.Sprintf("$%d", startIndex+i))
		args = append(args, value)
	}
	return strings.Join(placeholders, ", "), args
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
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
