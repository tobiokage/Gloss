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

	"gloss/internal/payments/hdfc"
	"gloss/internal/shared/enums"
)

func TestSyncPendingBillPaymentStatusOnlineSuccessSettlesBill(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{
		state: state,
		response: hdfc.TransactionResponse{
			StatusCode:    "00",
			StatusMessage: "Success",
			SaleTxnID:     state.providerRequestID,
			TxnStatus:     hdfc.TxnStatusSuccess,
			TxnMessage:    "Approved",
			BHTxnID:       state.providerTxnID,
			PaymentStatusDetail: hdfc.PaymentStatusDetails{{
				"paymentMode": "CardPayment",
			}},
			RawPayload: []byte(`{"txnStatus":"Success","PaymentStatusDetails":[{"paymentMode":"CardPayment"}]}`),
		},
	}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if client.statusCallCount != 1 {
		t.Fatalf("expected one HDFC status call, got %d", client.statusCallCount)
	}
	if state.statusCalledAfterBegin {
		t.Fatal("HDFC status call happened after transaction begin")
	}
	if state.paymentStatus != string(enums.PaymentStatusSuccess) {
		t.Fatalf("expected payment SUCCESS, got %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusPaid) {
		t.Fatalf("expected bill PAID, got %q", state.billStatus)
	}
	if state.amountPaid != state.totalAmount || state.amountDue != 0 {
		t.Fatalf("unexpected bill totals: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
	if state.actualCompletionMode != "CardPayment" {
		t.Fatalf("expected actual completion mode, got %q", state.actualCompletionMode)
	}
	if audit.calls != 1 || audit.lastAction != "PAYMENT_SUCCESS" {
		t.Fatalf("expected PAYMENT_SUCCESS audit, calls=%d action=%q", audit.calls, audit.lastAction)
	}
}

func TestSyncPendingBillPaymentStatusSplitSuccessSettlesWhenCovered(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.paymentMode = "SPLIT"
	state.billStatus = string(enums.BillStatusPartiallyPaid)
	state.successfulCashAmount = 5_000
	state.paymentAmount = 5_500
	state.totalAmount = 10_500
	state.amountPaid = 5_000
	state.amountDue = 5_500
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusSuccess) {
		t.Fatalf("expected payment SUCCESS, got %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusPaid) {
		t.Fatalf("expected split bill PAID, got %q", state.billStatus)
	}
	if state.amountPaid != state.totalAmount || state.amountDue != 0 {
		t.Fatalf("unexpected split totals: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
}

func TestSyncPendingBillPaymentStatusSplitFailureStaysPartiallyPaid(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.paymentMode = "SPLIT"
	state.billStatus = string(enums.BillStatusPartiallyPaid)
	state.successfulCashAmount = 5_000
	state.paymentAmount = 5_500
	state.totalAmount = 10_500
	state.amountPaid = 5_000
	state.amountDue = 5_500
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusFailed)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusFailed) {
		t.Fatalf("expected payment FAILED, got %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusPartiallyPaid) {
		t.Fatalf("split failure must keep bill PARTIALLY_PAID, got %q", state.billStatus)
	}
	if state.amountPaid != 5_000 || state.amountDue != 5_500 {
		t.Fatalf("unexpected split failure totals: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
}

func TestSyncPendingBillPaymentStatusOnlineFailureSetsPaymentFailed(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusFailed)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusFailed) {
		t.Fatalf("expected payment FAILED, got %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusPaymentFailed) {
		t.Fatalf("expected bill PAYMENT_FAILED, got %q", state.billStatus)
	}
	if state.amountPaid != 0 || state.amountDue != state.totalAmount {
		t.Fatalf("unexpected online failure totals: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
}

func TestSyncPendingBillPaymentStatusCancellationSetsPaymentFailedForOnlineOnly(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusCanceled)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusCancelled) {
		t.Fatalf("expected payment CANCELLED, got %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusPaymentFailed) {
		t.Fatalf("expected bill PAYMENT_FAILED after online-only cancellation, got %q", state.billStatus)
	}
}

func TestSyncPendingBillPaymentStatusPendingRemainsPendingWithoutAudit(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusInProgress)}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("expected payment PENDING, got %q", state.paymentStatus)
	}
	if state.recomputeCount != 0 {
		t.Fatalf("pending status must not recompute bill, got %d recomputes", state.recomputeCount)
	}
	if audit.calls != 0 {
		t.Fatalf("pending status must not write transition audit, got %d calls", audit.calls)
	}
}

func TestSyncPendingBillPaymentStatusRepeatedSuccessIsIdempotent(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for i := 0; i < 2; i++ {
		if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
			t.Fatalf("SyncPendingBillPaymentStatus attempt %d returned error: %v", i+1, err)
		}
	}

	if client.statusCallCount != 1 {
		t.Fatalf("repeated success must not call HDFC again, got %d calls", client.statusCallCount)
	}
	if state.statusUpdateCount != 1 || state.recomputeCount != 1 {
		t.Fatalf("expected one update/recompute, got updates=%d recomputes=%d", state.statusUpdateCount, state.recomputeCount)
	}
	if audit.calls != 1 {
		t.Fatalf("expected one audit transition, got %d", audit.calls)
	}
	if state.amountPaid != state.totalAmount || state.amountDue != 0 {
		t.Fatalf("success was double-counted or not settled: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
}

func TestSyncPendingBillPaymentStatusDelayedFailureAfterSuccessDoesNotDowngrade(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.findStatusOverride = string(enums.PaymentStatusPending)
	state.paymentStatus = string(enums.PaymentStatusSuccess)
	state.billStatus = string(enums.BillStatusPaid)
	state.amountPaid = state.totalAmount
	state.amountDue = 0
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusFailed)}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusSuccess) || state.billStatus != string(enums.BillStatusPaid) {
		t.Fatalf("delayed failure downgraded state: payment=%q bill=%q", state.paymentStatus, state.billStatus)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 || audit.calls != 0 {
		t.Fatalf("terminal success must not mutate/audit, updates=%d recomputes=%d audits=%d", state.statusUpdateCount, state.recomputeCount, audit.calls)
	}
}

func TestSyncPendingBillPaymentStatusTerminalFailureIsNotMutatedByDelayedSuccess(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.findStatusOverride = string(enums.PaymentStatusPending)
	state.paymentStatus = string(enums.PaymentStatusFailed)
	state.billStatus = string(enums.BillStatusPaymentFailed)
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if state.paymentStatus != string(enums.PaymentStatusFailed) || state.billStatus != string(enums.BillStatusPaymentFailed) {
		t.Fatalf("terminal failure mutated incorrectly: payment=%q bill=%q", state.paymentStatus, state.billStatus)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 {
		t.Fatalf("terminal failure must not update, updates=%d recomputes=%d", state.statusUpdateCount, state.recomputeCount)
	}
}

func TestSyncPendingBillPaymentStatusCancelledBillIsNotResurrected(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.billStatus = string(enums.BillStatusCancelled)
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if client.statusCallCount != 0 {
		t.Fatalf("cancelled bill must not call HDFC, got %d calls", client.statusCallCount)
	}
	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("cancelled bill pre-call guard mutated payment to %q", state.paymentStatus)
	}
	if state.billStatus != string(enums.BillStatusCancelled) {
		t.Fatalf("cancelled bill was resurrected to %q", state.billStatus)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 || audit.calls != 0 {
		t.Fatalf("cancelled bill must not update/recompute/audit, updates=%d recomputes=%d audits=%d", state.statusUpdateCount, state.recomputeCount, audit.calls)
	}
}

func TestSyncPendingBillPaymentStatusLockedCancelledBillDoesNotMutate(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	db := openPaymentStatusSyncTestDB(t, state)
	audit := &paymentStatusSyncAuditRecorder{}
	client := &paymentStatusSyncHDFCClient{
		state:                  state,
		response:               paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess),
		cancelBillBeforeReturn: true,
	}
	service := NewService(NewRepo(db), client, audit, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
		t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
	}

	if client.statusCallCount != 1 {
		t.Fatalf("expected one HDFC status call before race guard, got %d", client.statusCallCount)
	}
	if state.paymentStatus != string(enums.PaymentStatusPending) {
		t.Fatalf("locked cancelled bill mutated payment to %q", state.paymentStatus)
	}
	if state.amountPaid != 0 || state.amountDue != state.totalAmount {
		t.Fatalf("locked cancelled bill recomputed totals: paid=%d due=%d", state.amountPaid, state.amountDue)
	}
	if state.billStatus != string(enums.BillStatusCancelled) {
		t.Fatalf("locked cancelled bill changed to %q", state.billStatus)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 || audit.calls != 0 {
		t.Fatalf("locked cancelled bill must not update/recompute/audit, updates=%d recomputes=%d audits=%d", state.statusUpdateCount, state.recomputeCount, audit.calls)
	}
}

func TestSyncPendingBillPaymentStatusMissingProviderTxnIDReturnsError(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.providerTxnID = ""
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID)
	if err == nil || !strings.Contains(err.Error(), "provider transaction id") {
		t.Fatalf("expected missing provider transaction id error, got %v", err)
	}
	if client.statusCallCount != 0 {
		t.Fatalf("missing provider txn id must not call HDFC, got %d calls", client.statusCallCount)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 {
		t.Fatalf("missing provider txn id must not mutate, updates=%d recomputes=%d", state.statusUpdateCount, state.recomputeCount)
	}
}

func TestSyncPendingBillPaymentStatusMissingTerminalTIDReturnsError(t *testing.T) {
	state := newPaymentStatusSyncTestState()
	state.terminalTID = ""
	db := openPaymentStatusSyncTestDB(t, state)
	client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
	service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID)
	if err == nil || !strings.Contains(err.Error(), "terminal id") {
		t.Fatalf("expected missing terminal id error, got %v", err)
	}
	if client.statusCallCount != 0 {
		t.Fatalf("missing terminal id must not call HDFC, got %d calls", client.statusCallCount)
	}
	if state.statusUpdateCount != 0 || state.recomputeCount != 0 {
		t.Fatalf("missing terminal id must not mutate, updates=%d recomputes=%d", state.statusUpdateCount, state.recomputeCount)
	}
}

func TestSyncPendingBillPaymentStatusNoPendingPaymentDoesNotCallHDFC(t *testing.T) {
	testCases := []struct {
		name          string
		paymentStatus string
		gateway       string
	}{
		{name: "non-pending", paymentStatus: string(enums.PaymentStatusSuccess), gateway: GatewayHDFC},
		{name: "non-HDFC", paymentStatus: string(enums.PaymentStatusPending), gateway: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state := newPaymentStatusSyncTestState()
			state.paymentStatus = tc.paymentStatus
			state.gateway = tc.gateway
			db := openPaymentStatusSyncTestDB(t, state)
			client := &paymentStatusSyncHDFCClient{state: state, response: paymentStatusSyncResponse(state, hdfc.TxnStatusSuccess)}
			service := NewService(NewRepo(db), client, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

			if err := service.SyncPendingBillPaymentStatus(context.Background(), state.tenantID, state.storeID, state.billID, state.userID); err != nil {
				t.Fatalf("SyncPendingBillPaymentStatus returned error: %v", err)
			}
			if client.statusCallCount != 0 {
				t.Fatalf("expected no HDFC status call, got %d", client.statusCallCount)
			}
			if state.beginCount != 0 {
				t.Fatalf("expected no settlement transaction, got %d begins", state.beginCount)
			}
		})
	}
}

type paymentStatusSyncHDFCClient struct {
	state                  *paymentStatusSyncTestState
	response               hdfc.TransactionResponse
	err                    error
	statusCallCount        int
	cancelBillBeforeReturn bool
}

func (c *paymentStatusSyncHDFCClient) CreateSale(context.Context, hdfc.CreateSaleRequest) (hdfc.TransactionResponse, error) {
	return hdfc.TransactionResponse{}, errors.New("CreateSale should not be called by status sync")
}

func (c *paymentStatusSyncHDFCClient) GetTransactionStatus(_ context.Context, req hdfc.TransactionStatusRequest) (hdfc.TransactionResponse, error) {
	c.statusCallCount++
	if c.state != nil {
		c.state.mu.Lock()
		c.state.statusCalledAfterBegin = c.state.beginCount > 0
		c.state.lastStatusRequest = req
		if c.cancelBillBeforeReturn {
			c.state.billStatus = string(enums.BillStatusCancelled)
		}
		c.state.mu.Unlock()
	}
	return c.response, c.err
}

type paymentStatusSyncAuditRecorder struct {
	calls      int
	lastAction string
}

func (r *paymentStatusSyncAuditRecorder) RecordPaymentEvent(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
	action string,
	_ map[string]any,
) error {
	r.calls++
	r.lastAction = action
	return nil
}

type paymentStatusSyncTestState struct {
	mu                     sync.Mutex
	tenantID               string
	storeID                string
	billID                 string
	paymentID              string
	userID                 string
	billNumber             string
	paymentMode            string
	billStatus             string
	gateway                string
	totalAmount            int64
	amountPaid             int64
	amountDue              int64
	successfulCashAmount   int64
	paymentAmount          int64
	paymentStatus          string
	findStatusOverride     string
	providerRequestID      string
	providerTxnID          string
	terminalTID            string
	actualCompletionMode   string
	statusUpdateCount      int
	recomputeCount         int
	beginCount             int
	statusCalledAfterBegin bool
	lastStatusRequest      hdfc.TransactionStatusRequest
}

func newPaymentStatusSyncTestState() *paymentStatusSyncTestState {
	return &paymentStatusSyncTestState{
		tenantID:          "tenant-1",
		storeID:           "store-1",
		billID:            "bill-1",
		paymentID:         "payment-1",
		userID:            "user-1",
		billNumber:        "MSS-000001",
		paymentMode:       "ONLINE",
		billStatus:        string(enums.BillStatusPaymentPending),
		gateway:           GatewayHDFC,
		totalAmount:       10_500,
		amountPaid:        0,
		amountDue:         10_500,
		paymentAmount:     10_500,
		paymentStatus:     string(enums.PaymentStatusPending),
		providerRequestID: "SALE-1",
		providerTxnID:     "BH-1",
		terminalTID:       "63000019",
	}
}

func paymentStatusSyncResponse(state *paymentStatusSyncTestState, txnStatus string) hdfc.TransactionResponse {
	return hdfc.TransactionResponse{
		StatusCode:    "00",
		StatusMessage: "OK",
		SaleTxnID:     state.providerRequestID,
		TxnStatus:     txnStatus,
		TxnMessage:    txnStatus,
		BHTxnID:       state.providerTxnID,
		RawPayload:    []byte(fmt.Sprintf(`{"txnStatus":%q}`, txnStatus)),
	}
}

var (
	paymentStatusSyncDriverOnce    sync.Once
	paymentStatusSyncDriverCounter uint64
	paymentStatusSyncDriverStates  sync.Map
)

func openPaymentStatusSyncTestDB(t *testing.T, state *paymentStatusSyncTestState) *sql.DB {
	t.Helper()

	paymentStatusSyncDriverOnce.Do(func() {
		sql.Register("payment_status_sync_test_driver", paymentStatusSyncDriver{})
	})

	dsn := fmt.Sprintf("payment-status-sync-test-%d", atomic.AddUint64(&paymentStatusSyncDriverCounter, 1))
	paymentStatusSyncDriverStates.Store(dsn, state)

	db, err := sql.Open("payment_status_sync_test_driver", dsn)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	db.SetMaxOpenConns(1)

	t.Cleanup(func() {
		_ = db.Close()
		paymentStatusSyncDriverStates.Delete(dsn)
	})

	return db
}

type paymentStatusSyncDriver struct{}

func (paymentStatusSyncDriver) Open(name string) (driver.Conn, error) {
	stateValue, ok := paymentStatusSyncDriverStates.Load(name)
	if !ok {
		return nil, fmt.Errorf("unknown payment status sync test state: %s", name)
	}
	return &paymentStatusSyncConn{state: stateValue.(*paymentStatusSyncTestState)}, nil
}

type paymentStatusSyncConn struct {
	state *paymentStatusSyncTestState
}

func (c *paymentStatusSyncConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *paymentStatusSyncConn) Close() error {
	return nil
}

func (c *paymentStatusSyncConn) Begin() (driver.Tx, error) {
	c.state.mu.Lock()
	c.state.beginCount++
	c.state.mu.Unlock()
	return paymentStatusSyncTx{}, nil
}

func (c *paymentStatusSyncConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

func (c *paymentStatusSyncConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.state.exec(query, paymentStatusSyncNamedValues(args))
}

func (c *paymentStatusSyncConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.state.query(query, paymentStatusSyncNamedValues(args))
}

type paymentStatusSyncTx struct{}

func (paymentStatusSyncTx) Commit() error {
	return nil
}

func (paymentStatusSyncTx) Rollback() error {
	return nil
}

func (s *paymentStatusSyncTestState) query(query string, args []any) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := paymentStatusSyncNormalizeQuery(query)
	switch {
	case strings.HasPrefix(normalized, "select p.id::text"):
		return s.queryPendingPayment(args)
	case strings.HasPrefix(normalized, "select p.status, b.status"):
		if paymentStatusSyncString(args[0]) != s.paymentID ||
			paymentStatusSyncString(args[1]) != s.billID ||
			paymentStatusSyncString(args[2]) != s.tenantID ||
			paymentStatusSyncString(args[3]) != s.storeID {
			return &paymentStatusSyncRows{columns: []string{"status", "status"}}, nil
		}
		return &paymentStatusSyncRows{
			columns: []string{"payment_status", "bill_status"},
			values:  [][]driver.Value{{s.paymentStatus, s.billStatus}},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func (s *paymentStatusSyncTestState) queryPendingPayment(args []any) (driver.Rows, error) {
	if paymentStatusSyncString(args[0]) != s.billID ||
		paymentStatusSyncString(args[1]) != s.tenantID ||
		paymentStatusSyncString(args[2]) != s.storeID {
		return paymentStatusSyncPendingRows(nil), nil
	}
	findStatus := s.paymentStatus
	if s.findStatusOverride != "" {
		findStatus = s.findStatusOverride
	}
	if s.gateway != GatewayHDFC ||
		s.billStatus == string(enums.BillStatusCancelled) ||
		(findStatus != string(enums.PaymentStatusInitiated) && findStatus != string(enums.PaymentStatusPending)) {
		return paymentStatusSyncPendingRows(nil), nil
	}
	return paymentStatusSyncPendingRows([][]driver.Value{{
		s.paymentID,
		s.billID,
		s.tenantID,
		s.storeID,
		s.billNumber,
		s.paymentMode,
		s.paymentAmount,
		findStatus,
		s.gateway,
		s.providerRequestID,
		s.providerTxnID,
		s.terminalTID,
	}}), nil
}

func paymentStatusSyncPendingRows(values [][]driver.Value) *paymentStatusSyncRows {
	return &paymentStatusSyncRows{
		columns: []string{
			"id", "bill_id", "tenant_id", "store_id", "bill_number", "payment_mode_summary",
			"amount", "status", "gateway", "provider_request_id", "provider_txn_id", "terminal_tid",
		},
		values: values,
	}
}

func (s *paymentStatusSyncTestState) exec(query string, args []any) (driver.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := paymentStatusSyncNormalizeQuery(query)
	switch {
	case strings.HasPrefix(normalized, "update payments set status ="):
		if paymentStatusSyncString(args[0]) != s.paymentID ||
			paymentStatusSyncString(args[15]) != s.billID ||
			paymentStatusSyncString(args[16]) != s.tenantID ||
			paymentStatusSyncString(args[17]) != s.storeID {
			return driver.RowsAffected(0), nil
		}
		s.paymentStatus = paymentStatusSyncString(args[1])
		s.providerRequestID = paymentStatusSyncString(args[2])
		s.providerTxnID = paymentStatusSyncString(args[3])
		s.terminalTID = paymentStatusSyncString(args[4])
		s.actualCompletionMode = paymentStatusSyncString(args[9])
		s.statusUpdateCount++
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(normalized, "with payment_totals"):
		s.recomputeCount++
		successfulAmount := s.successfulCashAmount
		if s.paymentStatus == string(enums.PaymentStatusSuccess) {
			successfulAmount += s.paymentAmount
		}
		if successfulAmount > s.totalAmount {
			successfulAmount = s.totalAmount
		}
		s.amountPaid = successfulAmount
		s.amountDue = s.totalAmount - successfulAmount
		if s.amountDue < 0 {
			s.amountDue = 0
		}
		if s.billStatus != string(enums.BillStatusCancelled) {
			switch {
			case s.amountPaid >= s.totalAmount:
				s.billStatus = string(enums.BillStatusPaid)
			case s.paymentMode == "ONLINE" && !paymentStatusSyncIsUnresolved(s.paymentStatus):
				s.billStatus = string(enums.BillStatusPaymentFailed)
			case s.paymentMode == "ONLINE":
				s.billStatus = string(enums.BillStatusPaymentPending)
			case s.paymentMode == "SPLIT":
				s.billStatus = string(enums.BillStatusPartiallyPaid)
			}
		}
		return driver.RowsAffected(1), nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

type paymentStatusSyncRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *paymentStatusSyncRows) Columns() []string {
	return r.columns
}

func (r *paymentStatusSyncRows) Close() error {
	return nil
}

func (r *paymentStatusSyncRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func paymentStatusSyncNamedValues(values []driver.NamedValue) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value.Value)
	}
	return args
}

func paymentStatusSyncNormalizeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func paymentStatusSyncString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func paymentStatusSyncIsUnresolved(status string) bool {
	return status == string(enums.PaymentStatusInitiated) || status == string(enums.PaymentStatusPending)
}
