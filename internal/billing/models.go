package billing

import (
	"time"

	"gloss/internal/shared/enums"
)

type PaymentMode string

const (
	PaymentModeCash   PaymentMode = "CASH"
	PaymentModeOnline PaymentMode = "ONLINE"
	PaymentModeSplit  PaymentMode = "SPLIT"
)

const (
	maxDiscountPercent        int64 = 30
	commissionPercent         int64 = 10
	taxInclusiveBaseNumerator int64 = 100
	taxInclusiveBaseDivisor   int64 = 105
	billNumberSequenceWidth         = 6
)

const (
	MaxBillLineUnitPricePaise int64 = 100_000_000
	MaxBillItemQuantity       int64 = 1_000
	MaxBillTipAmountPaise     int64 = 10_000_000
	MaxBillTotalAmountPaise   int64 = 100_000_000
)

// AuthoritativeBillLineInput must be built from backend-verified catalogue data.
type AuthoritativeBillLineInput struct {
	CatalogueItemID string
	ServiceName     string
	AssignedStaffID string
	UnitPrice       int64
	Quantity        int64
}

type TipAllocation struct {
	StaffID   string
	TipAmount int64
}

type PaymentInput struct {
	Mode       PaymentMode
	CashAmount int64
}

type CalculatorInput struct {
	Lines          []AuthoritativeBillLineInput
	DiscountAmount int64
	TipAmount      int64
	TipAllocations []TipAllocation
	Payment        PaymentInput
}

type CalculatedBillLine struct {
	CatalogueItemID      string
	ServiceName          string
	AssignedStaffID      string
	UnitPrice            int64
	Quantity             int64
	LineGrossAmount      int64
	LineDiscountAmount   int64
	LineNetAmount        int64
	TaxableBaseAmount    int64
	TaxAmount            int64
	CommissionBaseAmount int64
	CommissionAmount     int64
}

type CalculatedTotals struct {
	ServiceGrossAmount int64
	DiscountAmount     int64
	ServiceNetAmount   int64
	TipAmount          int64
	TaxableBaseAmount  int64
	TaxAmount          int64
	TotalAmount        int64
	AmountPaid         int64
	AmountDue          int64
}

type CalculationResult struct {
	Lines          []CalculatedBillLine
	TipAllocations []TipAllocation
	Totals         CalculatedTotals
	PaymentMode    PaymentMode
	Status         enums.BillStatus
}

type BillNumberInput struct {
	StoreCode string
	Date      time.Time
	Sequence  int64
}

type StoreSnapshot struct {
	ID       string
	TenantID string
	Name     string
	Code     string
	Location string
}

type AuthoritativeStaffMember struct {
	ID string
}

type BillRecord struct {
	ID                 string
	BillNumber         string
	Status             string
	PaymentModeSummary string
	ServiceGrossAmount int64
	DiscountAmount     int64
	ServiceNetAmount   int64
	TipAmount          int64
	TaxableBaseAmount  int64
	TaxAmount          int64
	TotalAmount        int64
	AmountPaid         int64
	AmountDue          int64
	CreatedAt          time.Time
	PaidAt             *time.Time
}

type BillItemRecord struct {
	ID                   string
	CatalogueItemID      string
	ServiceName          string
	AssignedStaffID      string
	UnitPrice            int64
	Quantity             int64
	LineGrossAmount      int64
	LineDiscountAmount   int64
	LineNetAmount        int64
	TaxableBaseAmount    int64
	TaxAmount            int64
	CommissionBaseAmount int64
	CommissionAmount     int64
}

type BillTipAllocationRecord struct {
	ID        string
	StaffID   string
	TipAmount int64
}

type BillPaymentRecord struct {
	ID                string
	Gateway           *string
	PaymentMethod     string
	Amount            int64
	Status            string
	ProviderRequestID *string
	ProviderTxnID     *string
	TerminalTID       *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	VerifiedAt        *time.Time
}

type BillGraph struct {
	Store          StoreSnapshot
	Bill           BillRecord
	Items          []BillItemRecord
	TipAllocations []BillTipAllocationRecord
	Payments       []BillPaymentRecord
}

type CreateBillInput struct {
	ClientBillRef  string
	IdempotencyKey string
	RequestHash    string
	UserID         string
	TenantID       string
	StoreID        string
	Request        ValidatedCreateBillRequest
}

type PersistedBillItem struct {
	ID                   string
	CatalogueItemID      string
	AssignedStaffID      string
	ServiceName          string
	UnitPrice            int64
	Quantity             int64
	LineGrossAmount      int64
	LineDiscountAmount   int64
	LineNetAmount        int64
	TaxableBaseAmount    int64
	TaxAmount            int64
	CommissionBaseAmount int64
	CommissionAmount     int64
}

type InsertBillInput struct {
	ID                 string
	TenantID           string
	StoreID            string
	BillNumber         string
	Status             string
	ServiceGrossAmount int64
	DiscountAmount     int64
	ServiceNetAmount   int64
	TipAmount          int64
	TaxableBaseAmount  int64
	TaxAmount          int64
	TotalAmount        int64
	AmountPaid         int64
	AmountDue          int64
	PaymentModeSummary string
	CreatedByUserID    string
	CreatedAt          time.Time
	PaidAt             *time.Time
}

type InsertTipAllocationInput struct {
	ID        string
	BillID    string
	StaffID   string
	TipAmount int64
	CreatedAt time.Time
}

type InsertCommissionLedgerInput struct {
	ID                   string
	BillID               string
	BillItemID           string
	StaffID              string
	BaseAmount           int64
	CommissionPercentBPS int
	CommissionAmount     int64
	CreatedAt            time.Time
}

type InsertPaymentInput struct {
	ID            string
	BillID        string
	Gateway       *string
	PaymentMethod string
	Amount        int64
	Status        string
	VerifiedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
