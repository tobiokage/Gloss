package enums

type BillStatus string

const (
	BillStatusPaid           BillStatus = "PAID"
	BillStatusPaymentPending BillStatus = "PAYMENT_PENDING"
	BillStatusPaymentFailed  BillStatus = "PAYMENT_FAILED"
	BillStatusPartiallyPaid  BillStatus = "PARTIALLY_PAID"
	BillStatusCancelled      BillStatus = "CANCELLED"
)
