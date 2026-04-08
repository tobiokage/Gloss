package billing

import "time"

type CreateBillRequest struct {
	ClientBillRef  string                       `json:"client_bill_ref"`
	Items          []CreateBillItemRequest      `json:"items"`
	DiscountAmount *int64                       `json:"discount_amount,omitempty"`
	TipAmount      *int64                       `json:"tip_amount,omitempty"`
	TipAllocations []CreateBillTipAllocationDTO `json:"tip_allocations,omitempty"`
	Payment        CreateBillPaymentRequest     `json:"payment"`
	IdempotencyKey string                       `json:"idempotency_key"`
}

type CreateBillItemRequest struct {
	CatalogueItemID string `json:"catalogue_item_id"`
	Quantity        int64  `json:"quantity"`
	AssignedStaffID string `json:"assigned_staff_id"`
}

type CreateBillTipAllocationDTO struct {
	StaffID   string `json:"staff_id"`
	TipAmount int64  `json:"tip_amount"`
}

type CreateBillPaymentRequest struct {
	Mode       string `json:"mode"`
	CashAmount *int64 `json:"cash_amount,omitempty"`
}

type CreateBillResponse struct {
	Bill           CreatedBillHeaderResponse          `json:"bill"`
	Items          []CreatedBillItemResponse          `json:"items"`
	TipAllocations []CreatedBillTipAllocationResponse `json:"tip_allocations"`
	Payments       []CreatedBillPaymentResponse       `json:"payments"`
	Receipt        ReceiptPayloadResponse             `json:"receipt"`
}

type CreatedBillHeaderResponse struct {
	ID                 string     `json:"id"`
	BillNumber         string     `json:"bill_number"`
	Status             string     `json:"status"`
	PaymentModeSummary string     `json:"payment_mode_summary"`
	ServiceGrossAmount int64      `json:"service_gross_amount"`
	DiscountAmount     int64      `json:"discount_amount"`
	ServiceNetAmount   int64      `json:"service_net_amount"`
	TipAmount          int64      `json:"tip_amount"`
	TaxableBaseAmount  int64      `json:"taxable_base_amount"`
	TaxAmount          int64      `json:"tax_amount"`
	TotalAmount        int64      `json:"total_amount"`
	AmountPaid         int64      `json:"amount_paid"`
	AmountDue          int64      `json:"amount_due"`
	CreatedAt          time.Time  `json:"created_at"`
	PaidAt             *time.Time `json:"paid_at,omitempty"`
}

type CreatedBillItemResponse struct {
	ID                   string `json:"id"`
	CatalogueItemID      string `json:"catalogue_item_id"`
	ServiceName          string `json:"service_name"`
	AssignedStaffID      string `json:"assigned_staff_id"`
	UnitPrice            int64  `json:"unit_price"`
	Quantity             int64  `json:"quantity"`
	LineGrossAmount      int64  `json:"line_gross_amount"`
	LineDiscountAmount   int64  `json:"line_discount_amount"`
	LineNetAmount        int64  `json:"line_net_amount"`
	TaxableBaseAmount    int64  `json:"taxable_base_amount"`
	TaxAmount            int64  `json:"tax_amount"`
	CommissionBaseAmount int64  `json:"commission_base_amount"`
	CommissionAmount     int64  `json:"commission_amount"`
}

type CreatedBillTipAllocationResponse struct {
	ID        string `json:"id"`
	StaffID   string `json:"staff_id"`
	TipAmount int64  `json:"tip_amount"`
}

type CreatedBillPaymentResponse struct {
	ID            string     `json:"id"`
	PaymentMethod string     `json:"payment_method"`
	Amount        int64      `json:"amount"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
}

type ReceiptPayloadResponse struct {
	Store    ReceiptStoreResponse     `json:"store"`
	Bill     ReceiptBillResponse      `json:"bill"`
	Items    []ReceiptItemResponse    `json:"items"`
	Payments []ReceiptPaymentResponse `json:"payments"`
	Totals   ReceiptTotalsResponse    `json:"totals"`
}

type ReceiptStoreResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	Location string `json:"location"`
}

type ReceiptBillResponse struct {
	ID                 string     `json:"id"`
	BillNumber         string     `json:"bill_number"`
	Status             string     `json:"status"`
	PaymentModeSummary string     `json:"payment_mode_summary"`
	CreatedAt          time.Time  `json:"created_at"`
	PaidAt             *time.Time `json:"paid_at,omitempty"`
}

type ReceiptItemResponse struct {
	ServiceName     string `json:"service_name"`
	Quantity        int64  `json:"quantity"`
	UnitPrice       int64  `json:"unit_price"`
	LineGrossAmount int64  `json:"line_gross_amount"`
	LineNetAmount   int64  `json:"line_net_amount"`
}

type ReceiptPaymentResponse struct {
	PaymentMethod string `json:"payment_method"`
	Amount        int64  `json:"amount"`
	Status        string `json:"status"`
}

type ReceiptTotalsResponse struct {
	ServiceGrossAmount int64 `json:"service_gross_amount"`
	DiscountAmount     int64 `json:"discount_amount"`
	ServiceNetAmount   int64 `json:"service_net_amount"`
	TipAmount          int64 `json:"tip_amount"`
	TaxableBaseAmount  int64 `json:"taxable_base_amount"`
	TaxAmount          int64 `json:"tax_amount"`
	TotalAmount        int64 `json:"total_amount"`
	AmountPaid         int64 `json:"amount_paid"`
	AmountDue          int64 `json:"amount_due"`
}
