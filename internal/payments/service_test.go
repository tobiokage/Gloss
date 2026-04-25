package payments

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gloss/internal/payments/hdfc"
	"gloss/internal/shared/enums"
)

func TestInitiateBillOnlinePaymentSaleSuccessStaysPending(t *testing.T) {
	state := newPaymentsServiceTestState()
	db := openPaymentsServiceTestDB(t, state)
	client := &paymentsTestHDFCClient{response: hdfc.TransactionResponse{
		StatusCode:    "00",
		StatusMessage: "Success",
		SaleTxnID:     "SALE-RETURNED",
		TxnStatus:     hdfc.TxnStatusSuccess,
		TxnMessage:    "Sale initiated",
		BHTxnID:       "BH123",
		RawPayload:    []byte(`{"txnStatus":"Success"}`),
	}}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.InitiateBillOnlinePayment(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err != nil {
		t.Fatalf("InitiateBillOnlinePayment returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("sale initiation Success must leave payment pending, got %q", state.paymentStatus)
	}
	if state.billStatus != "PAYMENT_PENDING" {
		t.Fatalf("sale initiation must not settle bill, got bill status %q", state.billStatus)
	}
	if state.recomputeCount != 0 {
		t.Fatalf("sale initiation success must not recompute bill state, got %d recomputes", state.recomputeCount)
	}
	if state.providerRequestID != "SALE-RETURNED" {
		t.Fatalf("expected provider_request_id from response, got %q", state.providerRequestID)
	}
	if state.providerTxnID != "BH123" {
		t.Fatalf("expected provider_txn_id from response, got %q", state.providerTxnID)
	}
	if state.terminalTID != state.terminalTIDConfig {
		t.Fatalf("expected terminal_tid %q, got %q", state.terminalTIDConfig, state.terminalTID)
	}
	if state.providerSaleRequestedAt.IsZero() {
		t.Fatal("expected provider_sale_requested_at to be set")
	}
	if state.providerConfirmedAt != nil || state.verifiedAt != nil {
		t.Fatalf("sale initiation must not set settlement timestamps, confirmed=%v verified=%v", state.providerConfirmedAt, state.verifiedAt)
	}
}

func TestInitiateBillOnlinePaymentUsableSaleResponseIsPending(t *testing.T) {
	state := newPaymentsServiceTestState()
	db := openPaymentsServiceTestDB(t, state)
	client := &paymentsTestHDFCClient{response: hdfc.TransactionResponse{
		StatusCode:    "00",
		StatusMessage: "Accepted",
		SaleTxnID:     "SALE-ACCEPTED",
		TxnStatus:     hdfc.TxnStatusInProgress,
		TxnMessage:    "In progress",
		BHTxnID:       "BH456",
		RawPayload:    []byte(`{"txnStatus":"InProgress"}`),
	}}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.InitiateBillOnlinePayment(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err != nil {
		t.Fatalf("InitiateBillOnlinePayment returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("usable sale response must leave payment pending, got %q", state.paymentStatus)
	}
	if state.providerTxnStatus != hdfc.TxnStatusInProgress {
		t.Fatalf("expected raw provider txn status to be stored, got %q", state.providerTxnStatus)
	}
	if client.callCount != 1 {
		t.Fatalf("expected one HDFC sale call, got %d", client.callCount)
	}
}

func TestInitiateBillOnlinePaymentTransportErrorDoesNotDuplicateSale(t *testing.T) {
	state := newPaymentsServiceTestState()
	db := openPaymentsServiceTestDB(t, state)
	client := &paymentsTestHDFCClient{err: errors.New("timeout")}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.InitiateBillOnlinePayment(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err == nil {
		t.Fatal("expected first sale attempt to return transport error")
	}
	if client.callCount != 1 {
		t.Fatalf("expected one HDFC call, got %d", client.callCount)
	}
	firstSaleTxnID := state.providerRequestID
	if firstSaleTxnID == "" || state.providerSaleRequestedAt.IsZero() {
		t.Fatalf("expected attempted sale metadata to be stored, saleTxnID=%q requestedAt=%v", firstSaleTxnID, state.providerSaleRequestedAt)
	}
	if state.providerStatusCode != "SALE_REQUEST_UNRESOLVED" {
		t.Fatalf("expected unresolved sale metadata, got %q", state.providerStatusCode)
	}

	err = service.InitiateBillOnlinePayment(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err != nil {
		t.Fatalf("expected repeated initiation to be skipped cleanly, got %v", err)
	}
	if client.callCount != 1 {
		t.Fatalf("repeated initiation must not call HDFC again, got %d calls", client.callCount)
	}
	if state.providerRequestID != firstSaleTxnID {
		t.Fatalf("repeated initiation regenerated saleTxnId: first=%q now=%q", firstSaleTxnID, state.providerRequestID)
	}
}

func TestInitiateBillOnlinePaymentSplitFailureKeepsBillPartiallyPaid(t *testing.T) {
	state := newPaymentsServiceTestState()
	state.paymentMode = "SPLIT"
	state.billStatus = "PARTIALLY_PAID"
	db := openPaymentsServiceTestDB(t, state)
	client := &paymentsTestHDFCClient{response: hdfc.TransactionResponse{
		StatusCode:    "99",
		StatusMessage: "Declined",
		SaleTxnID:     "SALE-FAILED",
		TxnStatus:     hdfc.TxnStatusFailed,
		TxnMessage:    "Failed",
		RawPayload:    []byte(`{"txnStatus":"Failed"}`),
	}}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.InitiateBillOnlinePayment(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err != nil {
		t.Fatalf("InitiateBillOnlinePayment returned error: %v", err)
	}
	if state.paymentStatus != string(enums.PaymentStatusFailed) {
		t.Fatalf("expected failed payment status, got %q", state.paymentStatus)
	}
	if state.billStatus != "PARTIALLY_PAID" {
		t.Fatalf("split bill must remain PARTIALLY_PAID, got %q", state.billStatus)
	}
	if state.recomputeCount != 1 {
		t.Fatalf("expected one bill recompute, got %d", state.recomputeCount)
	}
}

func TestUpdatePaymentAfterSaleRequiresOneAffectedRow(t *testing.T) {
	state := newPaymentsServiceTestState()
	state.paymentUpdateRowsAffected = 0
	db := openPaymentsServiceTestDB(t, state)

	err := NewRepo(db).UpdatePaymentAfterSale(context.Background(), SaleUpdateInput{
		PaymentID:         state.paymentID,
		BillID:            state.billID,
		Status:            string(enums.PaymentStatusPending),
		ProviderRequestID: "SALE-1",
		TerminalTID:       state.terminalTIDConfig,
		ResponsePayload:   []byte(`{}`),
		UpdatedAt:         time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected zero-row update to fail")
	}
	if !strings.Contains(err.Error(), "did not affect exactly one row") {
		t.Fatalf("expected rows affected error, got %v", err)
	}
}

func TestCancelBillOnlinePaymentAttemptNonConfirmedResponseDoesNotMutateState(t *testing.T) {
	state := newPaymentsServiceTestState()
	state.paymentStatus = string(enums.PaymentStatusPending)
	state.providerRequestID = "SALE-1"
	state.providerTxnID = "BH-1"
	state.terminalTID = state.terminalTIDConfig
	db := openPaymentsServiceTestDB(t, state)
	client := &paymentsTestHDFCClient{cancelResponse: hdfc.TransactionResponse{
		StatusCode:    "00",
		StatusMessage: "Accepted",
		SaleTxnID:     "SALE-1",
		TxnStatus:     hdfc.TxnStatusInProgress,
		TxnMessage:    "Cancel in progress",
		BHTxnID:       "BH-1",
		RawPayload:    []byte(`{"txnStatus":"InProgress"}`),
	}}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.CancelBillOnlinePaymentAttempt(context.Background(), state.tenantID, state.storeID, state.billID, state.paymentID, state.userID)
	if err == nil {
		t.Fatal("expected non-confirmed HDFC cancellation to fail")
	}
	if client.cancelCallCount != 1 {
		t.Fatalf("expected one HDFC cancel call, got %d", client.cancelCallCount)
	}
	if state.cancelUpdateCount != 0 {
		t.Fatalf("non-confirmed cancellation must not update payment row, got %d updates", state.cancelUpdateCount)
	}
	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("payment status changed on non-confirmed cancellation: %q", state.paymentStatus)
	}
	if state.recomputeCount != 0 {
		t.Fatalf("non-confirmed cancellation must not recompute bill state, got %d recomputes", state.recomputeCount)
	}
}

func TestUpdatePaymentAttemptCancellationRequiresTenantStoreScope(t *testing.T) {
	state := newPaymentsServiceTestState()
	state.paymentStatus = string(enums.PaymentStatusPending)
	state.providerRequestID = "SALE-1"
	state.providerTxnID = "BH-1"
	state.terminalTID = state.terminalTIDConfig
	db := openPaymentsServiceTestDB(t, state)

	err := NewRepo(db).UpdatePaymentAttemptCancellation(context.Background(), "wrong-tenant", state.storeID, CancelAttemptUpdateInput{
		PaymentID:             state.paymentID,
		BillID:                state.billID,
		Status:                string(enums.PaymentStatusCancelled),
		ProviderRequestID:     state.providerRequestID,
		ProviderTxnID:         state.providerTxnID,
		TerminalTID:           state.terminalTID,
		ProviderStatusCode:    "00",
		ProviderStatusMessage: "Cancelled",
		ProviderTxnStatus:     hdfc.TxnStatusCanceled,
		ProviderTxnMessage:    "Cancelled",
		CancelResponsePayload: []byte(`{"txnStatus":"Canceled"}`),
		UpdatedAt:             time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected wrong tenant scope to fail")
	}
	if state.cancelUpdateCount != 0 {
		t.Fatalf("wrong tenant scope must not update cancellation row, got %d updates", state.cancelUpdateCount)
	}
	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("payment status changed despite wrong tenant scope: %q", state.paymentStatus)
	}
}

type paymentsTestHDFCClient struct {
	response        hdfc.TransactionResponse
	cancelResponse  hdfc.TransactionResponse
	err             error
	cancelErr       error
	callCount       int
	cancelCallCount int
	requests        []hdfc.CreateSaleRequest
	cancelRequests  []hdfc.CancelSaleRequest
}

func (c *paymentsTestHDFCClient) CreateSale(_ context.Context, req hdfc.CreateSaleRequest) (hdfc.TransactionResponse, error) {
	c.callCount++
	c.requests = append(c.requests, req)
	return c.response, c.err
}

func (c *paymentsTestHDFCClient) CancelSale(_ context.Context, req hdfc.CancelSaleRequest) (hdfc.TransactionResponse, error) {
	c.cancelCallCount++
	c.cancelRequests = append(c.cancelRequests, req)
	return c.cancelResponse, c.cancelErr
}

type paymentsServiceTestState struct {
	mu                        sync.Mutex
	tenantID                  string
	storeID                   string
	billID                    string
	paymentID                 string
	userID                    string
	billNumber                string
	billStatus                string
	paymentMode               string
	paymentAmount             int64
	paymentStatus             string
	providerRequestID         string
	providerTxnID             string
	terminalTID               string
	terminalTIDConfig         string
	providerStatusCode        string
	providerStatusMessage     string
	providerTxnStatus         string
	providerTxnMessage        string
	providerSaleRequestedAt   time.Time
	providerConfirmedAt       *time.Time
	verifiedAt                *time.Time
	recomputeCount            int
	cancelUpdateCount         int
	paymentUpdateRowsAffected int64
	recomputeRowsAffected     int64
}

func newPaymentsServiceTestState() *paymentsServiceTestState {
	return &paymentsServiceTestState{
		tenantID:                  "tenant-1",
		storeID:                   "store-1",
		billID:                    "bill-1",
		paymentID:                 "payment-1",
		userID:                    "user-1",
		billNumber:                "MSS-000001",
		billStatus:                "PAYMENT_PENDING",
		paymentMode:               "ONLINE",
		paymentAmount:             10500,
		paymentStatus:             string(enums.PaymentStatusInitiated),
		terminalTIDConfig:         "63000019",
		paymentUpdateRowsAffected: 1,
		recomputeRowsAffected:     1,
	}
}

var (
	paymentsServiceTestDriverOnce    sync.Once
	paymentsServiceTestDriverCounter uint64
	paymentsServiceTestDriverStates  sync.Map
)

func openPaymentsServiceTestDB(t *testing.T, state *paymentsServiceTestState) *sql.DB {
	t.Helper()

	paymentsServiceTestDriverOnce.Do(func() {
		sql.Register("payments_service_test_driver", paymentsServiceTestDriver{})
	})

	dsn := fmt.Sprintf("payments-service-test-%d", atomic.AddUint64(&paymentsServiceTestDriverCounter, 1))
	paymentsServiceTestDriverStates.Store(dsn, state)

	db, err := sql.Open("payments_service_test_driver", dsn)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	db.SetMaxOpenConns(1)

	t.Cleanup(func() {
		_ = db.Close()
		paymentsServiceTestDriverStates.Delete(dsn)
	})

	return db
}

type paymentsServiceTestDriver struct{}

func (paymentsServiceTestDriver) Open(name string) (driver.Conn, error) {
	stateValue, ok := paymentsServiceTestDriverStates.Load(name)
	if !ok {
		return nil, fmt.Errorf("unknown payments service test state: %s", name)
	}
	return &paymentsServiceTestConn{state: stateValue.(*paymentsServiceTestState)}, nil
}

type paymentsServiceTestConn struct {
	state *paymentsServiceTestState
}

func (c *paymentsServiceTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

func (c *paymentsServiceTestConn) Close() error {
	return nil
}

func (c *paymentsServiceTestConn) Begin() (driver.Tx, error) {
	return paymentsServiceTestTx{}, nil
}

func (c *paymentsServiceTestConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return paymentsServiceTestTx{}, nil
}

func (c *paymentsServiceTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.state.exec(query, paymentsServiceTestNamedValues(args))
}

func (c *paymentsServiceTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(query, paymentsServiceTestNamedValues(args))
}

type paymentsServiceTestTx struct{}

func (paymentsServiceTestTx) Commit() error {
	return nil
}

func (paymentsServiceTestTx) Rollback() error {
	return nil
}

func paymentsServiceTestNamedValues(values []driver.NamedValue) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value.Value)
	}
	return args
}

func (s *paymentsServiceTestState) exec(query string, args []any) (driver.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch normalized := paymentsServiceTestNormalizeQuery(query); {
	case strings.HasPrefix(normalized, "update payments set provider_status_code = coalesce"):
		s.providerStatusCode = "SALE_REQUEST_UNRESOLVED"
		s.providerStatusMessage = "HDFC sale request outcome is unresolved"
		return driver.RowsAffected(s.paymentUpdateRowsAffected), nil
	case strings.HasPrefix(normalized, "update payments set status ="):
		if paymentsServiceTestString(args[0]) != s.paymentID ||
			paymentsServiceTestString(args[11]) != s.billID ||
			paymentsServiceTestString(args[12]) != s.tenantID ||
			paymentsServiceTestString(args[13]) != s.storeID {
			return driver.RowsAffected(0), nil
		}
		s.paymentStatus = paymentsServiceTestString(args[1])
		s.providerRequestID = paymentsServiceTestString(args[2])
		s.providerTxnID = paymentsServiceTestString(args[3])
		s.terminalTID = paymentsServiceTestString(args[4])
		s.providerStatusCode = paymentsServiceTestString(args[5])
		s.providerStatusMessage = paymentsServiceTestString(args[6])
		s.providerTxnStatus = paymentsServiceTestString(args[7])
		s.providerTxnMessage = paymentsServiceTestString(args[8])
		s.cancelUpdateCount++
		return driver.RowsAffected(s.paymentUpdateRowsAffected), nil
	case strings.HasPrefix(normalized, "update payments set gateway = 'hdfc', status ="):
		s.paymentStatus = paymentsServiceTestString(args[1])
		s.providerRequestID = paymentsServiceTestString(args[2])
		s.providerTxnID = paymentsServiceTestString(args[3])
		s.terminalTID = paymentsServiceTestString(args[4])
		s.providerStatusCode = paymentsServiceTestString(args[5])
		s.providerStatusMessage = paymentsServiceTestString(args[6])
		s.providerTxnStatus = paymentsServiceTestString(args[7])
		s.providerTxnMessage = paymentsServiceTestString(args[8])
		s.providerConfirmedAt = paymentsServiceTestOptionalTime(args[10])
		s.verifiedAt = paymentsServiceTestOptionalTime(args[11])
		return driver.RowsAffected(s.paymentUpdateRowsAffected), nil
	case strings.HasPrefix(normalized, "with payment_totals"):
		s.recomputeCount++
		if s.paymentStatus == string(enums.PaymentStatusSuccess) {
			s.billStatus = "PAID"
		}
		return driver.RowsAffected(s.recomputeRowsAffected), nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (s *paymentsServiceTestState) query(query string, args []any) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch normalized := paymentsServiceTestNormalizeQuery(query); {
	case strings.HasPrefix(normalized, "select p.status from payments p"):
		if paymentsServiceTestString(args[0]) != s.paymentID ||
			paymentsServiceTestString(args[1]) != s.billID ||
			paymentsServiceTestString(args[2]) != s.tenantID ||
			paymentsServiceTestString(args[3]) != s.storeID {
			return &paymentsServiceTestRows{columns: []string{"status"}}, nil
		}
		return &paymentsServiceTestRows{
			columns: []string{"status"},
			values:  [][]driver.Value{{s.paymentStatus}},
		}, nil
	case strings.Contains(normalized, "coalesce(p.gateway"):
		return &paymentsServiceTestRows{
			columns: []string{
				"id", "bill_id", "tenant_id", "store_id", "bill_number", "payment_mode_summary",
				"amount", "status", "gateway", "provider_request_id", "provider_txn_id", "terminal_tid",
			},
			values: [][]driver.Value{{
				s.paymentID,
				s.billID,
				s.tenantID,
				s.storeID,
				s.billNumber,
				s.paymentMode,
				s.paymentAmount,
				s.paymentStatus,
				GatewayHDFC,
				s.providerRequestID,
				s.providerTxnID,
				s.terminalTID,
			}},
		}, nil
	case strings.Contains(normalized, "from payments p"):
		return &paymentsServiceTestRows{
			columns: []string{
				"id", "bill_id", "tenant_id", "store_id", "bill_number", "payment_mode_summary",
				"amount", "status", "provider_request_id", "provider_txn_id", "terminal_tid",
				"provider_sale_requested_at",
			},
			values: [][]driver.Value{{
				s.paymentID,
				s.billID,
				s.tenantID,
				s.storeID,
				s.billNumber,
				s.paymentMode,
				s.paymentAmount,
				s.paymentStatus,
				s.providerRequestID,
				s.providerTxnID,
				s.terminalTID,
				paymentsServiceTestOptionalTimeValue(s.providerSaleRequestedAt),
			}},
		}, nil
	case strings.Contains(normalized, "from stores"):
		return &paymentsServiceTestRows{
			columns: []string{"id", "tenant_id", "hdfc_terminal_tid"},
			values:  [][]driver.Value{{s.storeID, s.tenantID, s.terminalTIDConfig}},
		}, nil
	case strings.HasPrefix(normalized, "update payments set gateway = 'hdfc', provider_request_id"):
		requestedAt := paymentsServiceTestTime(args[3])
		if s.providerRequestID == "" {
			s.providerRequestID = paymentsServiceTestString(args[1])
		}
		if s.terminalTID == "" {
			s.terminalTID = paymentsServiceTestString(args[2])
		}
		if s.providerSaleRequestedAt.IsZero() {
			s.providerSaleRequestedAt = requestedAt
		}
		return &paymentsServiceTestRows{
			columns: []string{"provider_request_id", "terminal_tid", "provider_sale_requested_at"},
			values:  [][]driver.Value{{s.providerRequestID, s.terminalTID, s.providerSaleRequestedAt}},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type paymentsServiceTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *paymentsServiceTestRows) Columns() []string {
	return r.columns
}

func (r *paymentsServiceTestRows) Close() error {
	return nil
}

func (r *paymentsServiceTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func paymentsServiceTestNormalizeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func paymentsServiceTestString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func paymentsServiceTestTime(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	case *time.Time:
		return *typed
	default:
		panic(fmt.Sprintf("unexpected time type %T", value))
	}
}

func paymentsServiceTestOptionalTime(value any) *time.Time {
	switch typed := value.(type) {
	case nil:
		return nil
	case time.Time:
		value := typed
		return &value
	case *time.Time:
		return typed
	default:
		panic(fmt.Sprintf("unexpected optional time type %T", value))
	}
}

func paymentsServiceTestOptionalTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
