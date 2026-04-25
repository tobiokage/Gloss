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
	if response.ActiveOnlinePayment != nil {
		t.Fatal("cash bill must not return active_online_payment")
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

func TestCreateBillCashDoesNotCallOnlinePaymentCoordinator(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	_, err := service.CreateBill(context.Background(), billingTestAuthContext(), billingTestCreateBillRequest("idem-cash-no-hdfc"))
	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}
	if coordinator.ensureCalls != 0 || coordinator.initiateCalls != 0 {
		t.Fatalf("cash bill must not use HDFC coordinator, got ensure=%d initiate=%d", coordinator.ensureCalls, coordinator.initiateCalls)
	}
}

func TestCreateBillOnlinePersistsHDFCPaymentAndInitiatesAfterCommit(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{state: state}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	req := billingTestCreateBillRequest("idem-online")
	req.Payment.Mode = string(PaymentModeOnline)

	response, err := service.CreateBill(context.Background(), billingTestAuthContext(), req)
	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}

	if response.Bill.Status != string(enums.BillStatusPaymentPending) {
		t.Fatalf("expected bill status %q, got %q", enums.BillStatusPaymentPending, response.Bill.Status)
	}
	if response.Bill.AmountPaid != 0 || response.Bill.AmountDue != response.Bill.TotalAmount {
		t.Fatalf("unexpected online totals: paid=%d due=%d total=%d", response.Bill.AmountPaid, response.Bill.AmountDue, response.Bill.TotalAmount)
	}
	if len(response.Payments) != 1 {
		t.Fatalf("expected one HDFC payment row, got %d", len(response.Payments))
	}
	if response.Payments[0].Gateway == nil || *response.Payments[0].Gateway != "HDFC" {
		t.Fatalf("expected HDFC gateway, got %#v", response.Payments[0].Gateway)
	}
	if response.Payments[0].PaymentMethod != string(PaymentModeOnline) || response.Payments[0].Status != string(enums.PaymentStatusInitiated) {
		t.Fatalf("unexpected online payment row: %#v", response.Payments[0])
	}
	if response.ActiveOnlinePayment == nil {
		t.Fatal("expected active_online_payment for online bill")
	}
	if response.ActiveOnlinePayment.TerminalFlow != "HDFC_TERMINAL_OWNED" {
		t.Fatalf("unexpected terminal flow: %s", response.ActiveOnlinePayment.TerminalFlow)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "qr") {
		t.Fatalf("response must not contain QR payload fields: %s", string(encoded))
	}
	if coordinator.ensureCalls != 1 || coordinator.initiateCalls != 1 {
		t.Fatalf("expected ensure and initiate once, got ensure=%d initiate=%d", coordinator.ensureCalls, coordinator.initiateCalls)
	}
	if !coordinator.initiatedAfterCommit {
		t.Fatal("expected HDFC initiation after bill and payment rows were inserted")
	}
}

func TestCreateBillOnlineIdempotencyReplaySkipsMutableReadinessCheck(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{state: state}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	req := billingTestCreateBillRequest("idem-online-replay")
	req.Payment.Mode = string(PaymentModeOnline)

	response, err := service.CreateBill(context.Background(), billingTestAuthContext(), req)
	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}

	coordinator.ensureErr = errors.New("terminal tid changed")
	replayedResponse, err := service.CreateBill(context.Background(), billingTestAuthContext(), req)
	if err != nil {
		t.Fatalf("CreateBill replay returned error despite mutable readiness failure: %v", err)
	}
	if replayedResponse.Bill.ID != response.Bill.ID {
		t.Fatalf("expected replay bill %q, got %q", response.Bill.ID, replayedResponse.Bill.ID)
	}
	if coordinator.ensureCalls != 1 {
		t.Fatalf("expected readiness check only for original create, got %d calls", coordinator.ensureCalls)
	}
	if coordinator.initiateCalls != 1 {
		t.Fatalf("expected HDFC initiation only for original create, got %d calls", coordinator.initiateCalls)
	}
}

func TestCreateBillSplitPersistsCashAndOnlineRows(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{state: state}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	cashAmount := int64(5000)
	req := billingTestCreateBillRequest("idem-split")
	req.Payment.Mode = string(PaymentModeSplit)
	req.Payment.CashAmount = &cashAmount

	response, err := service.CreateBill(context.Background(), billingTestAuthContext(), req)
	if err != nil {
		t.Fatalf("CreateBill returned error: %v", err)
	}

	if response.Bill.Status != string(enums.BillStatusPartiallyPaid) {
		t.Fatalf("expected bill status %q, got %q", enums.BillStatusPartiallyPaid, response.Bill.Status)
	}
	if len(response.Payments) != 2 {
		t.Fatalf("expected cash + online payment rows, got %d", len(response.Payments))
	}
	if response.Payments[0].PaymentMethod != string(PaymentModeCash) || response.Payments[0].Status != string(enums.PaymentStatusSuccess) {
		t.Fatalf("unexpected cash row: %#v", response.Payments[0])
	}
	if response.Payments[1].Gateway == nil || *response.Payments[1].Gateway != "HDFC" {
		t.Fatalf("expected HDFC online row, got %#v", response.Payments[1])
	}
	if response.Payments[1].Amount != response.Bill.AmountDue {
		t.Fatalf("expected online row amount %d, got %d", response.Bill.AmountDue, response.Payments[1].Amount)
	}
	if coordinator.initiateCalls != 1 {
		t.Fatalf("expected one HDFC initiation, got %d", coordinator.initiateCalls)
	}
}

func TestCreateBillOnlineMissingTerminalConfigBlocksBeforeProviderCall(t *testing.T) {
	state := newBillingServiceTestState()
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{ensureErr: errors.New("missing tid")}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	req := billingTestCreateBillRequest("idem-online-missing-tid")
	req.Payment.Mode = string(PaymentModeOnline)

	_, err := service.CreateBill(context.Background(), billingTestAuthContext(), req)
	if err == nil {
		t.Fatal("expected missing terminal config to block online create")
	}
	if coordinator.initiateCalls != 0 {
		t.Fatalf("provider call must not be attempted, got %d calls", coordinator.initiateCalls)
	}
	if state.billInsertCount != 0 || state.paymentInsertCount != 0 {
		t.Fatalf("expected no committed rows, got bill inserts=%d payment inserts=%d", state.billInsertCount, state.paymentInsertCount)
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

func TestCreateBillRequestDecodeRejectsGatewayField(t *testing.T) {
	payload := `{
		"client_bill_ref":"tablet-1",
		"items":[{"catalogue_item_id":"cat-1","quantity":1,"assigned_staff_id":"staff-1"}],
		"payment":{"mode":"CASH","gateway":"HDFC"},
		"idempotency_key":"idem-gateway"
	}`

	var req CreateBillRequest
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&req)
	if err == nil {
		t.Fatal("expected decode error for unknown gateway field")
	}
	if !strings.Contains(err.Error(), "gateway") {
		t.Fatalf("expected decode error to mention gateway, got %v", err)
	}
}

func TestRetryOnlinePaymentRejectsCancelledBillWithoutHDFCCall(t *testing.T) {
	state := newBillingServiceTestState()
	billID := "44444444-4444-4444-8444-444444444444"
	state.addStoredBill(billID, string(enums.BillStatusCancelled), string(PaymentModeOnline), 0, 10500)
	db := openBillingServiceTestDB(t, state)
	coordinator := &billingTestOnlinePaymentCoordinator{state: state}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		&billingTestAuditRecorder{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	_, err := service.RetryOnlinePayment(context.Background(), billingTestAuthContext(), billID, RetryOnlinePaymentRequest{
		IdempotencyKey: "retry-cancelled",
	})
	if err == nil {
		t.Fatal("expected cancelled bill retry to fail")
	}
	if coordinator.initiateCalls != 0 {
		t.Fatalf("cancelled bill retry must not call HDFC initiation, got %d calls", coordinator.initiateCalls)
	}
	if state.paymentInsertCount != 0 {
		t.Fatalf("cancelled bill retry must not insert payment rows, got %d", state.paymentInsertCount)
	}
	if state.bills[billID].Status != string(enums.BillStatusCancelled) {
		t.Fatalf("cancelled bill status changed to %q", state.bills[billID].Status)
	}
}

func TestMarkBillOnlineRetryInitiatedRejectsCancelledBill(t *testing.T) {
	state := newBillingServiceTestState()
	billID := "55555555-5555-4555-8555-555555555555"
	state.addStoredBill(billID, string(enums.BillStatusCancelled), string(PaymentModeOnline), 0, 10500)
	db := openBillingServiceTestDB(t, state)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer tx.Rollback()

	err = NewRepo(db).MarkBillOnlineRetryInitiated(context.Background(), tx, billID)
	if err == nil {
		t.Fatal("expected cancelled bill retry state update to fail")
	}
	if state.bills[billID].Status != string(enums.BillStatusCancelled) {
		t.Fatalf("cancelled bill status changed to %q", state.bills[billID].Status)
	}
}

func TestRetryOnlinePaymentAuditsCommittedRetryBeforeHDFCFailure(t *testing.T) {
	state := newBillingServiceTestState()
	billID := "66666666-6666-4666-8666-666666666666"
	state.addStoredBill(billID, string(enums.BillStatusPaymentFailed), string(PaymentModeOnline), 0, 10500)
	db := openBillingServiceTestDB(t, state)
	auditRecorder := &billingTestAuditRecorder{}
	coordinator := &billingTestOnlinePaymentCoordinator{
		state:       state,
		initiateErr: errors.New("hdfc unavailable"),
	}
	service := NewService(
		db,
		NewRepo(db),
		idempotency.NewStore(),
		auditRecorder,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		coordinator,
	)

	_, err := service.RetryOnlinePayment(context.Background(), billingTestAuthContext(), billID, RetryOnlinePaymentRequest{
		IdempotencyKey: "retry-audit-before-hdfc-failure",
	})
	if err == nil {
		t.Fatal("expected HDFC initiation failure")
	}
	if state.paymentInsertCount != 1 {
		t.Fatalf("expected one committed retry payment row, got %d", state.paymentInsertCount)
	}
	if coordinator.initiateCalls != 1 {
		t.Fatalf("expected one HDFC initiation attempt, got %d", coordinator.initiateCalls)
	}
	if auditRecorder.paymentEventCalls != 1 {
		t.Fatalf("expected retry audit after commit, got %d calls", auditRecorder.paymentEventCalls)
	}
	if auditRecorder.lastPaymentAction != "PAYMENT_RETRY_INITIATED" {
		t.Fatalf("unexpected payment audit action %q", auditRecorder.lastPaymentAction)
	}
}

type billingTestAuditRecorder struct {
	calls             int
	paymentEventCalls int
	lastPaymentAction string
	err               error
}

type billingTestOnlinePaymentCoordinator struct {
	state                *billingServiceTestState
	ensureCalls          int
	initiateCalls        int
	initiatedAfterCommit bool
	ensureErr            error
	initiateErr          error
}

func (c *billingTestOnlinePaymentCoordinator) EnsureStoreReadyForOnline(context.Context, string, string) error {
	c.ensureCalls++
	return c.ensureErr
}

func (c *billingTestOnlinePaymentCoordinator) InitiateBillOnlinePayment(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	_ string,
) error {
	c.initiateCalls++
	if c.state != nil {
		c.initiatedAfterCommit = c.state.billInsertCount > 0 && c.state.paymentInsertCount > 0
	}
	return c.initiateErr
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

func (r *billingTestAuditRecorder) RecordPaymentEvent(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	action string,
	_ map[string]any,
) error {
	r.paymentEventCalls++
	r.lastPaymentAction = action
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
			ID:       "33333333-3333-4333-8333-333333333333",
			TenantID: "tenant-1",
			Name:     "Main Street Salon",
			Code:     "MSS",
			Location: "Downtown",
		},
		catalogue: map[string]AuthoritativeCatalogueLine{
			"11111111-1111-4111-8111-111111111111": {
				CatalogueItemID: "11111111-1111-4111-8111-111111111111",
				ServiceName:     "Haircut",
				UnitPrice:       10500,
			},
		},
		staff: map[string]AuthoritativeStaffMember{
			"22222222-2222-4222-8222-222222222222": {ID: "22222222-2222-4222-8222-222222222222"},
		},
		nextBillSequence: 41,
		idempotency:      map[string]billingServiceTestIdempotencyRecord{},
		bills:            map[string]billingServiceStoredBill{},
		billItems:        map[string][]BillItemRecord{},
		tipAllocations:   map[string][]BillTipAllocationRecord{},
		payments:         map[string][]BillPaymentRecord{},
	}
}

func (s *billingServiceTestState) addStoredBill(
	billID string,
	status string,
	paymentMode string,
	amountPaid int64,
	amountDue int64,
) {
	totalAmount := amountPaid + amountDue
	s.bills[billID] = billingServiceStoredBill{
		BillRecord: BillRecord{
			ID:                 billID,
			BillNumber:         "MSS-000999",
			Status:             status,
			PaymentModeSummary: paymentMode,
			ServiceGrossAmount: totalAmount,
			ServiceNetAmount:   totalAmount,
			TotalAmount:        totalAmount,
			AmountPaid:         amountPaid,
			AmountDue:          amountDue,
			CreatedAt:          time.Now().UTC(),
		},
		TenantID: s.store.TenantID,
		StoreID:  s.store.ID,
	}
}

func billingTestAuthContext() auth.AuthContext {
	return auth.AuthContext{
		UserID:   "user-1",
		TenantID: "tenant-1",
		StoreID:  "33333333-3333-4333-8333-333333333333",
		Role:     string(enums.RoleStoreManager),
	}
}

func billingTestCreateBillRequest(idempotencyKey string) CreateBillRequest {
	return CreateBillRequest{
		ClientBillRef: "tablet-1",
		Items: []CreateBillItemRequest{
			{
				CatalogueItemID: "11111111-1111-4111-8111-111111111111",
				Quantity:        1,
				AssignedStaffID: "22222222-2222-4222-8222-222222222222",
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
	case strings.HasPrefix(normalized, "update bills set status = case"):
		return s.execMarkBillOnlineRetryInitiated(args)
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
	case strings.HasPrefix(normalized, "select exists"):
		return s.queryHasActivePendingOnlinePayment(args)
	case strings.HasPrefix(normalized, "select id::text, bill_number, status"):
		return s.queryBillLock(args)
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
		Gateway:       billingServiceTestOptionalString(args[2]),
		PaymentMethod: billingServiceTestString(args[3]),
		Amount:        billingServiceTestInt64(args[4]),
		Status:        billingServiceTestString(args[5]),
		VerifiedAt:    billingServiceTestOptionalTime(args[6]),
		CreatedAt:     billingServiceTestTime(args[7]),
		UpdatedAt:     billingServiceTestTime(args[8]),
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

func (s *billingServiceTestState) execMarkBillOnlineRetryInitiated(args []any) (driver.Result, error) {
	billID := billingServiceTestString(args[0])
	bill, exists := s.bills[billID]
	if !exists {
		return driver.RowsAffected(0), nil
	}
	switch {
	case bill.PaymentModeSummary == string(PaymentModeOnline) &&
		(bill.Status == string(enums.BillStatusPaymentFailed) || bill.Status == string(enums.BillStatusPaymentPending) || bill.Status == string(enums.BillStatusPartiallyPaid)):
		bill.Status = string(enums.BillStatusPaymentPending)
	case bill.PaymentModeSummary == string(PaymentModeSplit) &&
		(bill.Status == string(enums.BillStatusPaymentFailed) || bill.Status == string(enums.BillStatusPaymentPending) || bill.Status == string(enums.BillStatusPartiallyPaid)):
		bill.Status = string(enums.BillStatusPartiallyPaid)
	default:
		return driver.RowsAffected(0), nil
	}
	s.bills[billID] = bill
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

func (s *billingServiceTestState) queryHasActivePendingOnlinePayment(args []any) (driver.Rows, error) {
	billID := billingServiceTestString(args[0])
	active := false
	for _, payment := range s.payments[billID] {
		if payment.PaymentMethod != string(PaymentModeOnline) || payment.Gateway == nil || *payment.Gateway != "HDFC" {
			continue
		}
		if payment.Status == string(enums.PaymentStatusInitiated) || payment.Status == string(enums.PaymentStatusPending) {
			active = true
			break
		}
	}
	return &billingServiceTestRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{active}},
	}, nil
}

func (s *billingServiceTestState) queryBillLock(args []any) (driver.Rows, error) {
	billID := billingServiceTestString(args[0])
	bill, exists := s.bills[billID]
	if !exists || bill.TenantID != billingServiceTestString(args[1]) || bill.StoreID != billingServiceTestString(args[2]) {
		return &billingServiceTestRows{
			columns: []string{
				"id", "bill_number", "status", "payment_mode_summary", "service_gross_amount",
				"discount_amount", "service_net_amount", "tip_amount", "taxable_base_amount",
				"tax_amount", "total_amount", "amount_paid", "amount_due", "created_at", "paid_at",
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
			nil,
			nil,
			nil,
			payment.CreatedAt,
			payment.UpdatedAt,
			verifiedAt,
		})
	}

	return &billingServiceTestRows{
		columns: []string{"id", "gateway", "payment_method", "amount", "status", "provider_request_id", "provider_txn_id", "terminal_tid", "created_at", "updated_at", "verified_at"},
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

func billingServiceTestOptionalString(value any) *string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		value := typed
		return &value
	case *string:
		return typed
	default:
		value := fmt.Sprint(typed)
		return &value
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
