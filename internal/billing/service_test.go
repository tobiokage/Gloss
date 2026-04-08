package billing

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gloss/internal/auth"
	"gloss/internal/shared/enums"
	"gloss/internal/shared/idempotency"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateBillCashAndIdempotencyReplay(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	auditRecorder := &billingTestAuditRecorder{}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		auditRecorder,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	response, err := service.CreateBill(context.Background(), billingTestAuthContext(), billingTestCreateBillRequest("idem-cash"))
	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}

	if response.Bill.Status != string(enums.BillStatusPaid) {
		t.Fatalf("expected bill status %q, got %q", enums.BillStatusPaid, response.Bill.Status)
	}
	if response.Bill.TotalAmount != 10500 {
		t.Fatalf("expected total_amount 10500, got %d", response.Bill.TotalAmount)
	}
	if response.Bill.AmountPaid != 10500 || response.Bill.AmountDue != 0 {
		t.Fatalf("unexpected payment totals: paid=%d due=%d", response.Bill.AmountPaid, response.Bill.AmountDue)
	}
	if len(response.Payments) != 1 {
		t.Fatalf("expected 1 payment row, got %d", len(response.Payments))
	}
	if response.Payments[0].PaymentMethod != string(PaymentModeCash) {
		t.Fatalf("expected payment method %q, got %q", PaymentModeCash, response.Payments[0].PaymentMethod)
	}
	if auditRecorder.calls != 1 {
		t.Fatalf("expected audit recorder to be called once, got %d", auditRecorder.calls)
	}
	if state.billInsertCount != 1 {
		t.Fatalf("expected exactly 1 bill insert, got %d", state.billInsertCount)
	}
	if state.paymentInsertCount != 1 {
		t.Fatalf("expected exactly 1 payment insert, got %d", state.paymentInsertCount)
	}

	replayedResponse, err := service.CreateBill(context.Background(), billingTestAuthContext(), billingTestCreateBillRequest("idem-cash"))
	if err != nil {
		t.Fatalf("CreateBill replay returned error: %v", err)
	}

	if replayedResponse.Bill.ID != response.Bill.ID {
		t.Fatalf("expected replay to return bill %q, got %q", response.Bill.ID, replayedResponse.Bill.ID)
	}
	if state.billInsertCount != 1 {
		t.Fatalf("expected replay to avoid extra bill insert, got %d inserts", state.billInsertCount)
	}
	if state.paymentInsertCount != 1 {
		t.Fatalf("expected replay to avoid extra payment insert, got %d inserts", state.paymentInsertCount)
	}
	if auditRecorder.calls != 1 {
		t.Fatalf("expected replay to avoid extra audit write, got %d audit calls", auditRecorder.calls)
	}
}

func TestCreateBillAuditFailureDoesNotFailCommittedBill(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	auditRecorder := &billingTestAuditRecorder{err: errors.New("audit unavailable")}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		auditRecorder,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	response, err := service.CreateBill(context.Background(), billingTestAuthContext(), billingTestCreateBillRequest("idem-audit"))
	if err != nil {
		t.Fatalf("CreateBill returned error after committed bill: %v", err)
	}
	if response.Bill.ID == "" {
		t.Fatal("expected committed bill response to include bill id")
	}
	if state.billInsertCount != 1 {
		t.Fatalf("expected committed bill insert count 1, got %d", state.billInsertCount)
	}
	if auditRecorder.calls != 1 {
		t.Fatalf("expected audit recorder to be called once, got %d", auditRecorder.calls)
	}
}

func TestCreateBillRequestDecodeRejectsUPIGateway(t *testing.T) {
	payload := `{
		"client_bill_ref":"tablet-1",
		"items":[{"catalogue_item_id":"cat-1","quantity":1,"assigned_staff_id":"staff-1"}],
		"payment":{"mode":"CASH","upi_gateway":"PAYTM"},
		"idempotency_key":"idem-upi"
	}`

	var req CreateBillRequest
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err == nil {
		t.Fatal("expected decode error for unknown upi_gateway field")
	}
	if !strings.Contains(err.Error(), "upi_gateway") {
		t.Fatalf("expected decode error to mention upi_gateway, got %v", err)
	}
}

type billingTestAuditRecorder struct {
	calls int
	err   error
}

func (r *billingTestAuditRecorder) RecordBillCreated(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	_ map[string]any,
) error {
	r.calls++
	return r.err
}

type billingServiceTestState struct {
	mu                 sync.Mutex
	store              StoreSnapshot
	catalogue          map[string]AuthoritativeCatalogueLine
	staff              map[string]AuthoritativeStaffMember
	nextBillSequence   int64
	idempotency        map[string]billingServiceTestIdempotencyRecord
	bills              map[string]billingServiceStoredBill
	billItems          map[string][]BillItemRecord
	tipAllocations     map[string][]BillTipAllocationRecord
	payments           map[string][]BillPaymentRecord
	billInsertCount    int
	paymentInsertCount int
}

type billingServiceTestIdempotencyRecord struct {
	RequestHash    string
	Status         string
	ResponseBillID string
}

type billingServiceStoredBill struct {
	BillRecord
	TenantID string
	StoreID  string
}

func newBillingServiceTestState() *billingServiceTestState {
	return &billingServiceTestState{
		store: StoreSnapshot{
			ID:       "store-1",
			TenantID: "tenant-1",
			Name:     "Main Street Salon",
			Code:     "MSS",
			Location: "Downtown",
		},
		catalogue: map[string]AuthoritativeCatalogueLine{
			"cat-1": {
				CatalogueItemID: "cat-1",
				ServiceName:     "Haircut",
				UnitPrice:       10500,
			},
		},
		staff: map[string]AuthoritativeStaffMember{
			"staff-1": {ID: "staff-1"},
		},
		nextBillSequence: 41,
		idempotency:      map[string]billingServiceTestIdempotencyRecord{},
		bills:            map[string]billingServiceStoredBill{},
		billItems:        map[string][]BillItemRecord{},
		tipAllocations:   map[string][]BillTipAllocationRecord{},
		payments:         map[string][]BillPaymentRecord{},
	}
}

func billingTestAuthContext() auth.AuthContext {
	return auth.AuthContext{
		UserID:   "user-1",
		TenantID: "tenant-1",
		StoreID:  "store-1",
		Role:     string(enums.RoleStoreManager),
	}
}

func billingTestCreateBillRequest(idempotencyKey string) CreateBillRequest {
	return CreateBillRequest{
		ClientBillRef: "tablet-1",
		Items: []CreateBillItemRequest{
			{
				CatalogueItemID: "cat-1",
				Quantity:        1,
				AssignedStaffID: "staff-1",
			},
		},
		Payment: CreateBillPaymentRequest{
			Mode: string(PaymentModeCash),
		},
		IdempotencyKey: idempotencyKey,
	}
}

var (
	billingServiceTestDriverOnce    sync.Once
	billingServiceTestDriverCounter uint64
	billingServiceTestDriverStates  sync.Map
)

func openBillingServiceTestDB(t *testing.T, state *billingServiceTestState) *sql.DB {
	t.Helper()

	billingServiceTestDriverOnce.Do(func() {
		sql.Register("billing_service_test_driver", billingServiceTestDriver{})
	})

	dsn := fmt.Sprintf("billing-service-test-%d", atomic.AddUint64(&billingServiceTestDriverCounter, 1))
	billingServiceTestDriverStates.Store(dsn, state)

	db, err := sql.Open("billing_service_test_driver", dsn)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	db.SetMaxOpenConns(1)

	t.Cleanup(func() {
		_ = db.Close()
		billingServiceTestDriverStates.Delete(dsn)
	})

	return db
}

type billingServiceTestDriver struct{}

func (billingServiceTestDriver) Open(name string) (driver.Conn, error) {
	stateValue, ok := billingServiceTestDriverStates.Load(name)
	if !ok {
		return nil, fmt.Errorf("unknown billing service test state: %s", name)
	}

	return &billingServiceTestConn{state: stateValue.(*billingServiceTestState)}, nil
}

type billingServiceTestConn struct {
	state *billingServiceTestState
}

func (c *billingServiceTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *billingServiceTestConn) Close() error {
	return nil
}

func (c *billingServiceTestConn) Begin() (driver.Tx, error) {
	return billingServiceTestTx{}, nil
}

func (c *billingServiceTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return billingServiceTestTx{}, nil
}

func (c *billingServiceTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.state.exec(query, billingServiceTestNamedValues(args))
}

func (c *billingServiceTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(query, billingServiceTestNamedValues(args))
}

type billingServiceTestTx struct{}

func (billingServiceTestTx) Commit() error {
	return nil
}

func (billingServiceTestTx) Rollback() error {
	return nil
}

func billingServiceTestNamedValues(values []driver.NamedValue) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value.Value)
	}
	return args
}

func (s *billingServiceTestState) exec(query string, args []any) (driver.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch normalized := billingServiceTestNormalizeQuery(query); {
	case strings.Contains(normalized, "insert into idempotency_keys"):
		return s.execInsertIdempotency(args)
	case strings.Contains(normalized, "insert into bills"):
		return s.execInsertBill(args)
	case strings.Contains(normalized, "insert into bill_items"):
		return s.execInsertBillItem(args)
	case strings.Contains(normalized, "insert into commission_ledger"):
		return driver.RowsAffected(1), nil
	case strings.Contains(normalized, "insert into payments"):
		return s.execInsertPayment(args)
	case strings.Contains(normalized, "update idempotency_keys"):
		return s.execCompleteIdempotency(args)
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (s *billingServiceTestState) query(query string, args []any) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch normalized := billingServiceTestNormalizeQuery(query); {
	case strings.Contains(normalized, "from bills b"):
		return s.queryBillHeader(args)
	case strings.Contains(normalized, "from idempotency_keys"):
		return s.queryIdempotency(args)
	case strings.Contains(normalized, "from stores s"):
		return s.queryStoreSnapshot(args)
	case strings.Contains(normalized, "from catalogue_items"):
		return s.queryCatalogue(args)
	case strings.Contains(normalized, "from staff s"):
		return s.queryStaff(args)
	case strings.Contains(normalized, "update store_bill_counters"):
		return s.queryBillCounter()
	case strings.Contains(normalized, "from bill_items"):
		return s.queryBillItems(args)
	case strings.Contains(normalized, "from bill_tip_allocations"):
		return s.queryTipAllocations(args)
	case strings.Contains(normalized, "from payments"):
		return s.queryPayments(args)
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (s *billingServiceTestState) execInsertIdempotency(args []any) (driver.Result, error) {
	key := billingServiceTestCompositeKey(args[1], args[2], args[3])
	if _, exists := s.idempotency[key]; exists {
		return nil, &pgconn.PgError{Code: "23505"}
	}

	s.idempotency[key] = billingServiceTestIdempotencyRecord{
		RequestHash: billingServiceTestString(args[4]),
		Status:      "IN_PROGRESS",
	}
	return driver.RowsAffected(1), nil
}

func (s *billingServiceTestState) execInsertBill(args []any) (driver.Result, error) {
	billID := billingServiceTestString(args[0])
	bill := billingServiceStoredBill{
		BillRecord: BillRecord{
			ID:                 billID,
			BillNumber:         billingServiceTestString(args[3]),
			Status:             billingServiceTestString(args[4]),
			ServiceGrossAmount: billingServiceTestInt64(args[5]),
			DiscountAmount:     billingServiceTestInt64(args[6]),
			ServiceNetAmount:   billingServiceTestInt64(args[7]),
			TipAmount:          billingServiceTestInt64(args[8]),
			TaxableBaseAmount:  billingServiceTestInt64(args[9]),
			TaxAmount:          billingServiceTestInt64(args[10]),
			TotalAmount:        billingServiceTestInt64(args[11]),
			AmountPaid:         billingServiceTestInt64(args[12]),
			AmountDue:          billingServiceTestInt64(args[13]),
			PaymentModeSummary: billingServiceTestString(args[14]),
			CreatedAt:          billingServiceTestTime(args[16]),
			PaidAt:             billingServiceTestOptionalTime(args[17]),
		},
		TenantID: billingServiceTestString(args[1]),
		StoreID:  billingServiceTestString(args[2]),
	}

	s.bills[billID] = bill
	s.billInsertCount++
	return driver.RowsAffected(1), nil
}

func (s *billingServiceTestState) execInsertBillItem(args []any) (driver.Result, error) {
	billID := billingServiceTestString(args[1])
	s.billItems[billID] = append(s.billItems[billID], BillItemRecord{
		ID:                   billingServiceTestString(args[0]),
		CatalogueItemID:      billingServiceTestString(args[2]),
		ServiceName:          billingServiceTestString(args[3]),
		UnitPrice:            billingServiceTestInt64(args[4]),
		Quantity:             billingServiceTestInt64(args[5]),
		LineGrossAmount:      billingServiceTestInt64(args[6]),
		LineDiscountAmount:   billingServiceTestInt64(args[7]),
		LineNetAmount:        billingServiceTestInt64(args[8]),
		TaxableBaseAmount:    billingServiceTestInt64(args[9]),
		TaxAmount:            billingServiceTestInt64(args[10]),
		AssignedStaffID:      billingServiceTestString(args[11]),
		CommissionBaseAmount: billingServiceTestInt64(args[12]),
		CommissionAmount:     billingServiceTestInt64(args[13]),
	})
	return driver.RowsAffected(1), nil
}

func (s *billingServiceTestState) execInsertPayment(args []any) (driver.Result, error) {
	billID := billingServiceTestString(args[1])
	s.payments[billID] = append(s.payments[billID], BillPaymentRecord{
		ID:            billingServiceTestString(args[0]),
		PaymentMethod: billingServiceTestString(args[2]),
		Amount:        billingServiceTestInt64(args[3]),
		Status:        billingServiceTestString(args[4]),
		VerifiedAt:    billingServiceTestOptionalTime(args[5]),
		CreatedAt:     billingServiceTestTime(args[6]),
		UpdatedAt:     billingServiceTestTime(args[7]),
	})
	s.paymentInsertCount++
	return driver.RowsAffected(1), nil
}

func (s *billingServiceTestState) execCompleteIdempotency(args []any) (driver.Result, error) {
	key := billingServiceTestCompositeKey(args[0], args[1], args[2])
	record, exists := s.idempotency[key]
	if !exists || record.Status != "IN_PROGRESS" {
		return driver.RowsAffected(0), nil
	}

	record.Status = "COMPLETED"
	record.ResponseBillID = billingServiceTestString(args[3])
	s.idempotency[key] = record
	return driver.RowsAffected(1), nil
}

func (s *billingServiceTestState) queryIdempotency(args []any) (driver.Rows, error) {
	key := billingServiceTestCompositeKey(args[0], args[1], args[2])
	record, exists := s.idempotency[key]
	if !exists {
		return &billingServiceTestRows{
			columns: []string{"request_hash", "status", "response_bill_id"},
		}, nil
	}

	var responseBillID any
	if record.ResponseBillID != "" {
		responseBillID = record.ResponseBillID
	}

	return &billingServiceTestRows{
		columns: []string{"request_hash", "status", "response_bill_id"},
		values: [][]driver.Value{{
			record.RequestHash,
			record.Status,
			responseBillID,
		}},
	}, nil
}

func (s *billingServiceTestState) queryStoreSnapshot(args []any) (driver.Rows, error) {
	if billingServiceTestString(args[0]) != s.store.ID || billingServiceTestString(args[1]) != s.store.TenantID {
		return &billingServiceTestRows{columns: []string{"id", "tenant_id", "name", "code", "location"}}, nil
	}

	return &billingServiceTestRows{
		columns: []string{"id", "tenant_id", "name", "code", "location"},
		values: [][]driver.Value{{
			s.store.ID,
			s.store.TenantID,
			s.store.Name,
			s.store.Code,
			s.store.Location,
		}},
	}, nil
}

func (s *billingServiceTestState) queryCatalogue(args []any) (driver.Rows, error) {
	rows := make([][]driver.Value, 0, max(len(args)-1, 0))
	for _, rawID := range args[1:] {
		item, exists := s.catalogue[billingServiceTestString(rawID)]
		if !exists {
			continue
		}
		rows = append(rows, []driver.Value{item.CatalogueItemID, item.ServiceName, item.UnitPrice})
	}

	return &billingServiceTestRows{
		columns: []string{"id", "name", "list_price"},
		values:  rows,
	}, nil
}

func (s *billingServiceTestState) queryStaff(args []any) (driver.Rows, error) {
	rows := make([][]driver.Value, 0, max(len(args)-2, 0))
	for _, rawID := range args[2:] {
		staffID := billingServiceTestString(rawID)
		if _, exists := s.staff[staffID]; !exists {
			continue
		}
		rows = append(rows, []driver.Value{staffID})
	}

	return &billingServiceTestRows{
		columns: []string{"id"},
		values:  rows,
	}, nil
}

func (s *billingServiceTestState) queryBillCounter() (driver.Rows, error) {
	s.nextBillSequence++
	return &billingServiceTestRows{
		columns: []string{"last_bill_seq"},
		values:  [][]driver.Value{{s.nextBillSequence}},
	}, nil
}

func (s *billingServiceTestState) queryBillHeader(args []any) (driver.Rows, error) {
	billID := billingServiceTestString(args[0])
	bill, exists := s.bills[billID]
	if !exists || bill.TenantID != billingServiceTestString(args[1]) || bill.StoreID != billingServiceTestString(args[2]) {
		return &billingServiceTestRows{
			columns: []string{
				"id", "bill_number", "status", "payment_mode_summary", "service_gross_amount",
				"discount_amount", "service_net_amount", "tip_amount", "taxable_base_amount",
				"tax_amount", "total_amount", "amount_paid", "amount_due", "created_at", "paid_at",
				"store_id", "tenant_id", "store_name", "store_code", "store_location",
			},
		}, nil
	}

	var paidAt any
	if bill.PaidAt != nil {
		paidAt = *bill.PaidAt
	}

	return &billingServiceTestRows{
		columns: []string{
			"id", "bill_number", "status", "payment_mode_summary", "service_gross_amount",
			"discount_amount", "service_net_amount", "tip_amount", "taxable_base_amount",
			"tax_amount", "total_amount", "amount_paid", "amount_due", "created_at", "paid_at",
			"store_id", "tenant_id", "store_name", "store_code", "store_location",
		},
		values: [][]driver.Value{{
			bill.ID,
			bill.BillNumber,
			bill.Status,
			bill.PaymentModeSummary,
			bill.ServiceGrossAmount,
			bill.DiscountAmount,
			bill.ServiceNetAmount,
			bill.TipAmount,
			bill.TaxableBaseAmount,
			bill.TaxAmount,
			bill.TotalAmount,
			bill.AmountPaid,
			bill.AmountDue,
			bill.CreatedAt,
			paidAt,
			s.store.ID,
			s.store.TenantID,
			s.store.Name,
			s.store.Code,
			s.store.Location,
		}},
	}, nil
}

func (s *billingServiceTestState) queryBillItems(args []any) (driver.Rows, error) {
	items := s.billItems[billingServiceTestString(args[0])]
	rows := make([][]driver.Value, 0, len(items))
	for _, item := range items {
		rows = append(rows, []driver.Value{
			item.ID,
			item.CatalogueItemID,
			item.ServiceName,
			item.AssignedStaffID,
			item.UnitPrice,
			item.Quantity,
			item.LineGrossAmount,
			item.LineDiscountAmount,
			item.LineNetAmount,
			item.TaxableBaseAmount,
			item.TaxAmount,
			item.CommissionBaseAmount,
			item.CommissionAmount,
		})
	}

	return &billingServiceTestRows{
		columns: []string{
			"id", "catalogue_item_id", "service_name_snapshot", "assigned_staff_id",
			"unit_price_snapshot", "quantity", "line_gross_amount", "line_discount_amount",
			"line_net_amount", "taxable_base_amount", "tax_amount", "commission_base_amount",
			"commission_amount",
		},
		values: rows,
	}, nil
}

func (s *billingServiceTestState) queryTipAllocations(args []any) (driver.Rows, error) {
	allocations := s.tipAllocations[billingServiceTestString(args[0])]
	rows := make([][]driver.Value, 0, len(allocations))
	for _, allocation := range allocations {
		rows = append(rows, []driver.Value{allocation.ID, allocation.StaffID, allocation.TipAmount})
	}

	return &billingServiceTestRows{
		columns: []string{"id", "staff_id", "tip_amount"},
		values:  rows,
	}, nil
}

func (s *billingServiceTestState) queryPayments(args []any) (driver.Rows, error) {
	payments := s.payments[billingServiceTestString(args[0])]
	rows := make([][]driver.Value, 0, len(payments))
	for _, payment := range payments {
		var gateway any
		if payment.Gateway != nil {
			gateway = *payment.Gateway
		}

		var verifiedAt any
		if payment.VerifiedAt != nil {
			verifiedAt = *payment.VerifiedAt
		}

		rows = append(rows, []driver.Value{
			payment.ID,
			gateway,
			payment.PaymentMethod,
			payment.Amount,
			payment.Status,
			payment.CreatedAt,
			payment.UpdatedAt,
			verifiedAt,
		})
	}

	return &billingServiceTestRows{
		columns: []string{"id", "gateway", "payment_method", "amount", "status", "created_at", "updated_at", "verified_at"},
		values:  rows,
	}, nil
}

type billingServiceTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *billingServiceTestRows) Columns() []string {
	return r.columns
}

func (r *billingServiceTestRows) Close() error {
	return nil
}

func (r *billingServiceTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}

	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func billingServiceTestNormalizeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func billingServiceTestCompositeKey(tenantID any, storeID any, idempotencyKey any) string {
	return billingServiceTestString(tenantID) + "|" + billingServiceTestString(storeID) + "|" + billingServiceTestString(idempotencyKey)
}

func billingServiceTestString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func billingServiceTestInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int32:
		return int64(typed)
	case int:
		return int64(typed)
	default:
		panic(fmt.Sprintf("unexpected integer type %T", value))
	}
}

func billingServiceTestTime(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case *time.Time:
		return *typed
	default:
		panic(fmt.Sprintf("unexpected time type %T", value))
	}
}

func billingServiceTestOptionalTime(value any) *time.Time {
	switch typed := value.(type) {
	case nil:
		return nil
	case time.Time:
		timeValue := typed
		return &timeValue
	case *time.Time:
		return typed
	default:
		panic(fmt.Sprintf("unexpected optional time type %T", value))
	}
}
