package payments

import "time"

const (
	GatewayHDFC             = "HDFC"
	providerRequestIDPrefix = "SALE"
)

type StoreTerminalConfig struct {
	StoreID         string
	TenantID        string
	HDFCTerminalTID string
}

type PaymentForSale struct {
	ID                      string
	BillID                  string
	TenantID                string
	StoreID                 string
	BillNumber              string
	PaymentMode             string
	Amount                  int64
	Status                  string
	ProviderRequestID       string
	ProviderTxnID           string
	TerminalTID             string
	ProviderSaleRequestedAt *time.Time
}

type SaleRequestClaim struct {
	ProviderRequestID string
	TerminalTID       string
	RequestedAt       time.Time
}

type SaleUpdateInput struct {
	PaymentID             string
	BillID                string
	Status                string
	ProviderRequestID     string
	ProviderTxnID         string
	TerminalTID           string
	ProviderStatusCode    string
	ProviderStatusMessage string
	ProviderTxnStatus     string
	ProviderTxnMessage    string
	ResponsePayload       []byte
	ConfirmedAt           *time.Time
	VerifiedAt            *time.Time
	UpdatedAt             time.Time
}
