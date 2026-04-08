package billing

type CreateBillRequest struct {
	Items          []CreateBillItemRequest      `json:"items"`
	DiscountAmount *int64                       `json:"discount_amount,omitempty"`
	TipAmount      *int64                       `json:"tip_amount,omitempty"`
	TipAllocations []CreateBillTipAllocationDTO `json:"tip_allocations,omitempty"`
	Payment        CreateBillPaymentRequest     `json:"payment"`
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

type BillCalculationResponse struct {
	Status         string                         `json:"status"`
	PaymentMode    string                         `json:"payment_mode"`
	Items          []BillCalculationItemResponse  `json:"items"`
	TipAllocations []BillCalculationTipAllocation `json:"tip_allocations"`
	Totals         BillCalculationTotalsResponse  `json:"totals"`
	Payment        BillCalculationPaymentResponse `json:"payment"`
}

type BillCalculationItemResponse struct {
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

type BillCalculationTipAllocation struct {
	StaffID   string `json:"staff_id"`
	TipAmount int64  `json:"tip_amount"`
}

type BillCalculationTotalsResponse struct {
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

type BillCalculationPaymentResponse struct {
	CashAmount int64 `json:"cash_amount"`
	UPIAmount  int64 `json:"upi_amount"`
}
