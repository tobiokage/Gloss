package billing

import (
	"time"

	"gloss/internal/shared/enums"
)

type PaymentMode string

const (
	PaymentModeCash  PaymentMode = "CASH"
	PaymentModeUPI   PaymentMode = "UPI"
	PaymentModeSplit PaymentMode = "SPLIT"
)

const (
	maxDiscountPercent        int64 = 30
	commissionPercent         int64 = 10
	taxInclusiveBaseNumerator int64 = 100
	taxInclusiveBaseDivisor   int64 = 105
	billNumberSequenceWidth         = 6
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
