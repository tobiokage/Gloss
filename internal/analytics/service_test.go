package analytics

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"gloss/internal/auth"
	platformconfig "gloss/internal/platform/config"
	platformhttp "gloss/internal/platform/http"
	"gloss/internal/shared/enums"
)

const (
	analyticsTenantA = "11111111-1111-1111-1111-111111111111"
	analyticsTenantB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	analyticsStoreA1 = "22222222-2222-2222-2222-222222222222"
	analyticsStoreA2 = "22222222-2222-2222-2222-222222222223"
	analyticsStoreB1 = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	analyticsJWTKey  = "analytics-test-secret"
)

func TestAdminAnalyticsRoutesRequireSuperAdmin(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	router := newAnalyticsTestRouter(db)

	for _, tc := range []struct {
		name   string
		path   string
		role   enums.Role
		status int
	}{
		{"super admin bills", "/admin/bills", enums.RoleSuperAdmin, http.StatusOK},
		{"store manager bills", "/admin/bills", enums.RoleStoreManager, http.StatusUnauthorized},
		{"super admin summary", "/admin/analytics/summary", enums.RoleSuperAdmin, http.StatusOK},
		{"store manager summary", "/admin/analytics/summary", enums.RoleStoreManager, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+analyticsTestToken(t, tc.role, analyticsTenantA))
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			if recorder.Code != tc.status {
				t.Fatalf("expected status %d, got %d body=%s", tc.status, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAdminBillsTenantStoreFiltersAndCancelledVisibility(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	response, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{})
	if err != nil {
		t.Fatalf("ListAdminBills returned error: %v", err)
	}
	if len(response) != 3 {
		t.Fatalf("expected 3 tenant A bills, got %d", len(response))
	}
	if response[0].BillID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3" {
		t.Fatalf("expected newest bill first, got %#v", response[0])
	}
	for _, bill := range response {
		if bill.StoreID == analyticsStoreB1 {
			t.Fatalf("tenant A response leaked tenant B bill: %#v", bill)
		}
	}
	cancelled := findAdminBill(response, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2")
	if cancelled == nil || cancelled.CancellationReason == nil || *cancelled.CancellationReason != "customer changed mind" {
		t.Fatalf("expected cancelled bill reason, got %#v", cancelled)
	}

	storeFiltered, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{
		StoreID: analyticsStoreA2,
	})
	if err != nil {
		t.Fatalf("store-filtered ListAdminBills returned error: %v", err)
	}
	if len(storeFiltered) != 1 || storeFiltered[0].StoreID != analyticsStoreA2 {
		t.Fatalf("unexpected store-filtered response: %#v", storeFiltered)
	}

	_, err = service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{
		StoreID: analyticsStoreB1,
	})
	if err == nil {
		t.Fatal("expected cross-tenant store filter to fail")
	}
}

func TestAdminBillsDateStatusSortingAndPagination(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	response, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{
		DateFrom: "2026-04-21",
		DateTo:   "2026-04-23",
		Status:   string(enums.BillStatusCancelled),
	})
	if err != nil {
		t.Fatalf("filtered ListAdminBills returned error: %v", err)
	}
	if len(response) != 1 || response[0].Status != string(enums.BillStatusCancelled) {
		t.Fatalf("unexpected date/status-filtered response: %#v", response)
	}

	paged, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{
		Limit:  "1",
		Offset: "1",
	})
	if err != nil {
		t.Fatalf("paged ListAdminBills returned error: %v", err)
	}
	if len(paged) != 1 || paged[0].BillID != "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2" {
		t.Fatalf("unexpected paged response: %#v", paged)
	}
}

func TestAdminBillsDefaultLimit(t *testing.T) {
	state := &analyticsTestState{
		stores: map[string]analyticsTestStore{
			analyticsStoreA1: {ID: analyticsStoreA1, TenantID: analyticsTenantA, Name: "Main Store"},
		},
	}
	baseTime := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	for i := 0; i < defaultAdminBillsLimit+5; i++ {
		state.bills = append(state.bills, analyticsTestBill{
			ID:                 fmt.Sprintf("aaaaaaaa-aaaa-aaaa-aaaa-%012d", i),
			TenantID:           analyticsTenantA,
			StoreID:            analyticsStoreA1,
			BillNumber:         fmt.Sprintf("A1-%06d", i),
			CreatedAt:          baseTime.Add(time.Duration(i) * time.Minute),
			Status:             string(enums.BillStatusPaid),
			TotalAmount:        1000,
			AmountPaid:         1000,
			PaymentModeSummary: "CASH",
		})
	}
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	response, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{})
	if err != nil {
		t.Fatalf("ListAdminBills returned error: %v", err)
	}
	if len(response) != defaultAdminBillsLimit {
		t.Fatalf("expected default limit %d, got %d", defaultAdminBillsLimit, len(response))
	}
}

func TestAdminAnalyticsRejectsInvalidFilters(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	for _, tc := range []struct {
		name  string
		query AdminBillsQuery
	}{
		{"invalid store", AdminBillsQuery{StoreID: "not-a-uuid"}},
		{"invalid date", AdminBillsQuery{DateFrom: "25-04-2026"}},
		{"invalid status", AdminBillsQuery{Status: "REFUNDED"}},
		{"invalid limit", AdminBillsQuery{Limit: "0"}},
		{"invalid offset", AdminBillsQuery{Offset: "-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), tc.query)
			if err == nil {
				t.Fatal("expected invalid filter to fail")
			}
		})
	}
}

func TestAdminAnalyticsSummaryCalculatesStoredTotals(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	summary, err := service.GetAdminAnalyticsSummary(context.Background(), superAdminAuth(analyticsTenantA), AnalyticsSummaryQuery{})
	if err != nil {
		t.Fatalf("GetAdminAnalyticsSummary returned error: %v", err)
	}
	assertSummary(t, summary, AnalyticsSummaryResponse{
		TotalBills:         3,
		TotalSales:         15000,
		CancelledBillCount: 1,
		CancelledAmount:    7000,
		TotalTax:           1428,
		TotalCommission:    2400,
		TotalTip:           1000,
	})

	storeSummary, err := service.GetAdminAnalyticsSummary(context.Background(), superAdminAuth(analyticsTenantA), AnalyticsSummaryQuery{
		StoreID: analyticsStoreA1,
	})
	if err != nil {
		t.Fatalf("store summary returned error: %v", err)
	}
	assertSummary(t, storeSummary, AnalyticsSummaryResponse{
		TotalBills:         2,
		TotalSales:         10000,
		CancelledBillCount: 1,
		CancelledAmount:    7000,
		TotalTax:           476,
		TotalCommission:    900,
		TotalTip:           1000,
	})

	cancelledSummary, err := service.GetAdminAnalyticsSummary(context.Background(), superAdminAuth(analyticsTenantA), AnalyticsSummaryQuery{
		Status: string(enums.BillStatusCancelled),
	})
	if err != nil {
		t.Fatalf("cancelled summary returned error: %v", err)
	}
	assertSummary(t, cancelledSummary, AnalyticsSummaryResponse{
		TotalBills:         1,
		CancelledBillCount: 1,
		CancelledAmount:    7000,
	})
}

func TestAnalyticsQueriesDoNotMutateOrTouchPayments(t *testing.T) {
	state := newAnalyticsTestState()
	db := openAnalyticsTestDB(t, state)
	service := NewService(NewRepo(db))

	if _, err := service.ListAdminBills(context.Background(), superAdminAuth(analyticsTenantA), AdminBillsQuery{}); err != nil {
		t.Fatalf("ListAdminBills returned error: %v", err)
	}
	if _, err := service.GetAdminAnalyticsSummary(context.Background(), superAdminAuth(analyticsTenantA), AnalyticsSummaryQuery{}); err != nil {
		t.Fatalf("GetAdminAnalyticsSummary returned error: %v", err)
	}

	if state.execCalls != 0 {
		t.Fatalf("analytics must not mutate records, got %d exec calls", state.execCalls)
	}
	if state.paymentQueryCalls != 0 {
		t.Fatalf("analytics must not query payments or trigger sync paths, got %d payment queries", state.paymentQueryCalls)
	}
}

func newAnalyticsTestRouter(db *sql.DB) http.Handler {
	cfg := platformconfig.Config{
		Auth: platformconfig.AuthConfig{JWTSecret: analyticsJWTKey, JWTTTL: time.Hour},
	}
	authMiddleware := auth.Middleware(cfg)
	superAdminOnlyMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCtx, err := auth.AuthContextFromContext(r.Context())
			if err != nil {
				platformhttp.WriteError(w, err)
				return
			}
			if err := auth.RequireRole(authCtx, enums.RoleSuperAdmin); err != nil {
				platformhttp.WriteError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	storeManagerOnlyMiddleware := func(next http.Handler) http.Handler { return next }
	handler := NewHandler(NewService(NewRepo(db)))
	noop := func(w http.ResponseWriter, _ *http.Request) {
		platformhttp.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}

	return platformhttp.NewRouter(
		noop,
		authMiddleware,
		superAdminOnlyMiddleware,
		storeManagerOnlyMiddleware,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		noop,
		handler.ListAdminBills,
		handler.GetAdminAnalyticsSummary,
	)
}

func analyticsTestToken(t *testing.T, role enums.Role, tenantID string) string {
	t.Helper()
	claims := auth.Claims{
		UserID:   "33333333-3333-3333-3333-333333333333",
		TenantID: tenantID,
		Role:     string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	if role == enums.RoleStoreManager {
		claims.StoreID = analyticsStoreA1
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(analyticsJWTKey))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return token
}

func superAdminAuth(tenantID string) auth.AuthContext {
	return auth.AuthContext{
		UserID:   "33333333-3333-3333-3333-333333333334",
		TenantID: tenantID,
		Role:     string(enums.RoleSuperAdmin),
	}
}

func findAdminBill(bills []AdminBillResponse, billID string) *AdminBillResponse {
	for i := range bills {
		if bills[i].BillID == billID {
			return &bills[i]
		}
	}
	return nil
}

func assertSummary(t *testing.T, got AnalyticsSummaryResponse, want AnalyticsSummaryResponse) {
	t.Helper()
	if got != want {
		encodedGot, _ := json.Marshal(got)
		encodedWant, _ := json.Marshal(want)
		t.Fatalf("unexpected summary\n got: %s\nwant: %s", encodedGot, encodedWant)
	}
}

type analyticsTestState struct {
	stores            map[string]analyticsTestStore
	bills             []analyticsTestBill
	commissions       map[string]int64
	execCalls         int
	paymentQueryCalls int
}

type analyticsTestStore struct {
	ID       string
	TenantID string
	Name     string
}

type analyticsTestBill struct {
	ID                 string
	TenantID           string
	StoreID            string
	BillNumber         string
	CreatedAt          time.Time
	Status             string
	TotalAmount        int64
	AmountPaid         int64
	AmountDue          int64
	PaymentModeSummary string
	TaxAmount          int64
	TipAmount          int64
	CancellationReason *string
}

func newAnalyticsTestState() *analyticsTestState {
	reason := "customer changed mind"
	return &analyticsTestState{
		stores: map[string]analyticsTestStore{
			analyticsStoreA1: {ID: analyticsStoreA1, TenantID: analyticsTenantA, Name: "Main Store"},
			analyticsStoreA2: {ID: analyticsStoreA2, TenantID: analyticsTenantA, Name: "Mall Store"},
			analyticsStoreB1: {ID: analyticsStoreB1, TenantID: analyticsTenantB, Name: "Other Tenant Store"},
		},
		bills: []analyticsTestBill{
			{
				ID:                 "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1",
				TenantID:           analyticsTenantA,
				StoreID:            analyticsStoreA1,
				BillNumber:         "A1-001",
				CreatedAt:          time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
				Status:             string(enums.BillStatusPaid),
				TotalAmount:        10000,
				AmountPaid:         10000,
				PaymentModeSummary: "CASH",
				TaxAmount:          476,
				TipAmount:          1000,
			},
			{
				ID:                 "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2",
				TenantID:           analyticsTenantA,
				StoreID:            analyticsStoreA1,
				BillNumber:         "A1-002",
				CreatedAt:          time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
				Status:             string(enums.BillStatusCancelled),
				TotalAmount:        7000,
				PaymentModeSummary: "ONLINE",
				TaxAmount:          333,
				TipAmount:          500,
				CancellationReason: &reason,
			},
			{
				ID:                 "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3",
				TenantID:           analyticsTenantA,
				StoreID:            analyticsStoreA2,
				BillNumber:         "A2-001",
				CreatedAt:          time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC),
				Status:             string(enums.BillStatusPartiallyPaid),
				TotalAmount:        20000,
				AmountPaid:         5000,
				AmountDue:          15000,
				PaymentModeSummary: "SPLIT",
				TaxAmount:          952,
			},
			{
				ID:                 "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1",
				TenantID:           analyticsTenantB,
				StoreID:            analyticsStoreB1,
				BillNumber:         "B1-001",
				CreatedAt:          time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC),
				Status:             string(enums.BillStatusPaid),
				TotalAmount:        30000,
				AmountPaid:         30000,
				PaymentModeSummary: "CASH",
				TaxAmount:          1428,
			},
		},
		commissions: map[string]int64{
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1": 900,
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2": 600,
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3": 1500,
			"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1": 2000,
		},
	}
}

var (
	analyticsTestDriverOnce    sync.Once
	analyticsTestDriverCounter uint64
	analyticsTestDriverStates  sync.Map
)

func openAnalyticsTestDB(t *testing.T, state *analyticsTestState) *sql.DB {
	t.Helper()
	analyticsTestDriverOnce.Do(func() {
		sql.Register("analytics_test_driver", analyticsTestDriver{})
	})

	dsn := fmt.Sprintf("analytics-test-%d", atomic.AddUint64(&analyticsTestDriverCounter, 1))
	analyticsTestDriverStates.Store(dsn, state)
	db, err := sql.Open("analytics_test_driver", dsn)
	if err != nil {
		t.Fatalf("failed to open analytics test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
		analyticsTestDriverStates.Delete(dsn)
	})
	return db
}

type analyticsTestDriver struct{}

func (analyticsTestDriver) Open(name string) (driver.Conn, error) {
	stateValue, ok := analyticsTestDriverStates.Load(name)
	if !ok {
		return nil, fmt.Errorf("unknown analytics test state: %s", name)
	}
	return &analyticsTestConn{state: stateValue.(*analyticsTestState)}, nil
}

type analyticsTestConn struct {
	state *analyticsTestState
}

func (c *analyticsTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare not supported")
}

func (c *analyticsTestConn) Close() error {
	return nil
}

func (c *analyticsTestConn) Begin() (driver.Tx, error) {
	return analyticsTestTx{}, nil
}

func (c *analyticsTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.execCalls++
	return driver.RowsAffected(0), nil
}

func (c *analyticsTestConn) QueryContext(_ context.Context, query string, values []driver.NamedValue) (driver.Rows, error) {
	args := analyticsTestNamedValues(values)
	normalized := analyticsNormalizeQuery(query)
	if strings.Contains(normalized, "from payments") {
		c.state.paymentQueryCalls++
		return nil, fmt.Errorf("analytics must not query payments")
	}

	switch {
	case strings.HasPrefix(normalized, "select 1 from stores"):
		return c.queryStoreExists(args), nil
	case strings.HasPrefix(normalized, "select b.id::text"):
		return c.queryAdminBills(normalized, args), nil
	case strings.HasPrefix(normalized, "with filtered_bills"):
		return c.querySummary(normalized, args), nil
	default:
		return nil, fmt.Errorf("unexpected analytics query: %s", query)
	}
}

type analyticsTestTx struct{}

func (analyticsTestTx) Commit() error {
	return nil
}

func (analyticsTestTx) Rollback() error {
	return nil
}

func (c *analyticsTestConn) queryStoreExists(args []any) driver.Rows {
	storeID := analyticsTestString(args[0])
	tenantID := analyticsTestString(args[1])
	store, exists := c.state.stores[storeID]
	if !exists || store.TenantID != tenantID {
		return &analyticsTestRows{columns: []string{"marker"}}
	}
	return &analyticsTestRows{columns: []string{"marker"}, values: [][]driver.Value{{1}}}
}

func (c *analyticsTestConn) queryAdminBills(query string, args []any) driver.Rows {
	filterArgs := args[:len(args)-2]
	limit := int(analyticsTestInt64(args[len(args)-2]))
	offset := int(analyticsTestInt64(args[len(args)-1]))
	bills := c.filteredBills(query, filterArgs)
	sort.Slice(bills, func(i, j int) bool {
		if bills[i].CreatedAt.Equal(bills[j].CreatedAt) {
			return bills[i].ID > bills[j].ID
		}
		return bills[i].CreatedAt.After(bills[j].CreatedAt)
	})
	if offset > len(bills) {
		bills = nil
	} else {
		bills = bills[offset:]
	}
	if limit < len(bills) {
		bills = bills[:limit]
	}

	rows := make([][]driver.Value, 0, len(bills))
	for _, bill := range bills {
		store := c.state.stores[bill.StoreID]
		var reason any
		if bill.CancellationReason != nil {
			reason = *bill.CancellationReason
		}
		rows = append(rows, []driver.Value{
			bill.ID,
			bill.BillNumber,
			bill.CreatedAt,
			store.ID,
			store.Name,
			bill.Status,
			bill.TotalAmount,
			bill.AmountPaid,
			bill.AmountDue,
			bill.PaymentModeSummary,
			reason,
		})
	}
	return &analyticsTestRows{
		columns: []string{
			"bill_id", "bill_number", "created_at", "store_id", "store_name", "status",
			"total_amount", "amount_paid", "amount_due", "payment_mode_summary", "cancellation_reason",
		},
		values: rows,
	}
}

func (c *analyticsTestConn) querySummary(query string, args []any) driver.Rows {
	bills := c.filteredBills(query, args)
	var summary AnalyticsSummary
	for _, bill := range bills {
		summary.TotalBills++
		if bill.Status == string(enums.BillStatusCancelled) {
			summary.CancelledBillCount++
			summary.CancelledAmount += bill.TotalAmount
			continue
		}
		summary.TotalSales += bill.AmountPaid
		summary.TotalTax += bill.TaxAmount
		summary.TotalTip += bill.TipAmount
		summary.TotalCommission += c.state.commissions[bill.ID]
	}

	return &analyticsTestRows{
		columns: []string{
			"total_bills", "total_sales", "cancelled_bill_count", "cancelled_amount",
			"total_tax", "total_commission", "total_tip",
		},
		values: [][]driver.Value{{
			summary.TotalBills,
			summary.TotalSales,
			summary.CancelledBillCount,
			summary.CancelledAmount,
			summary.TotalTax,
			summary.TotalCommission,
			summary.TotalTip,
		}},
	}
}

func (c *analyticsTestConn) filteredBills(query string, args []any) []analyticsTestBill {
	tenantID := analyticsTestString(args[0])
	argIndex := 1
	var storeID string
	if strings.Contains(query, "b.store_id =") {
		storeID = analyticsTestString(args[argIndex])
		argIndex++
	}
	var dateFrom *time.Time
	if strings.Contains(query, "b.created_at >=") {
		value := analyticsTestTime(args[argIndex])
		dateFrom = &value
		argIndex++
	}
	var dateTo *time.Time
	if strings.Contains(query, "b.created_at <") {
		value := analyticsTestTime(args[argIndex])
		dateTo = &value
		argIndex++
	}
	var status string
	if strings.Contains(query, "b.status =") {
		status = analyticsTestString(args[argIndex])
	}

	filtered := make([]analyticsTestBill, 0)
	for _, bill := range c.state.bills {
		if bill.TenantID != tenantID {
			continue
		}
		if storeID != "" && bill.StoreID != storeID {
			continue
		}
		if dateFrom != nil && bill.CreatedAt.Before(*dateFrom) {
			continue
		}
		if dateTo != nil && !bill.CreatedAt.Before(*dateTo) {
			continue
		}
		if status != "" && bill.Status != status {
			continue
		}
		filtered = append(filtered, bill)
	}
	return filtered
}

type analyticsTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *analyticsTestRows) Columns() []string {
	return r.columns
}

func (r *analyticsTestRows) Close() error {
	return nil
}

func (r *analyticsTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func analyticsTestNamedValues(values []driver.NamedValue) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value.Value)
	}
	return args
}

func analyticsNormalizeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func analyticsTestString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func analyticsTestInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	default:
		panic(fmt.Sprintf("unexpected integer type %T", value))
	}
}

func analyticsTestTime(value any) time.Time {
	switch typed := value.(type) {
	case time.Time:
		return typed
	default:
		panic(fmt.Sprintf("unexpected time type %T", value))
	}
}
