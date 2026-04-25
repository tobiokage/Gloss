package analytics

import "time"

type AdminBillsQuery struct {
	StoreID  string
	DateFrom string
	DateTo   string
	Status   string
	Limit    string
	Offset   string
}

type AdminBillResponse struct {
	BillID             string    `json:"bill_id"`
	BillNumber         string    `json:"bill_number"`
	CreatedAt          time.Time `json:"created_at"`
	StoreID            string    `json:"store_id"`
	StoreName          string    `json:"store_name"`
	Status             string    `json:"status"`
	TotalAmount        int64     `json:"total_amount"`
	AmountPaid         int64     `json:"amount_paid"`
	AmountDue          int64     `json:"amount_due"`
	PaymentModeSummary string    `json:"payment_mode_summary"`
	CancellationReason *string   `json:"cancellation_reason,omitempty"`
}

type AnalyticsSummaryQuery struct {
	StoreID  string
	DateFrom string
	DateTo   string
	Status   string
}

type AnalyticsSummaryResponse struct {
	TotalBills         int64 `json:"total_bills"`
	TotalSales         int64 `json:"total_sales"`
	CancelledBillCount int64 `json:"cancelled_bill_count"`
	CancelledAmount    int64 `json:"cancelled_amount"`
	TotalTax           int64 `json:"total_tax"`
	TotalCommission    int64 `json:"total_commission"`
	TotalTip           int64 `json:"total_tip"`
}

type adminBillFilters struct {
	StoreID  string
	DateFrom *time.Time
	DateTo   *time.Time
	Status   string
	Limit    int
	Offset   int
}

type analyticsSummaryFilters struct {
	StoreID  string
	DateFrom *time.Time
	DateTo   *time.Time
	Status   string
}

type AdminBillRow struct {
	BillID             string
	BillNumber         string
	CreatedAt          time.Time
	StoreID            string
	StoreName          string
	Status             string
	TotalAmount        int64
	AmountPaid         int64
	AmountDue          int64
	PaymentModeSummary string
	CancellationReason *string
}

type AnalyticsSummary struct {
	TotalBills         int64
	TotalSales         int64
	CancelledBillCount int64
	CancelledAmount    int64
	TotalTax           int64
	TotalCommission    int64
	TotalTip           int64
}
