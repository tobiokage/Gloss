package hdfc

type CreateSaleRequest struct {
	TID         string
	SaleTxnID   string
	AmountPaise int64
	Description string
}

type TransactionStatusRequest struct {
	TID       string
	BHTxnID   string
	SaleTxnID string
}

type CancelSaleRequest struct {
	TID       string
	BHTxnID   string
	SaleTxnID string
}

type TransactionResponse struct {
	StatusCode          string         `json:"statusCode"`
	StatusMessage       string         `json:"statusMessage"`
	SaleTxnID           string         `json:"saleTxnId"`
	SaleAmount          string         `json:"saleAmount"`
	SaleDateTime        string         `json:"saleDateTime"`
	TxnStatus           string         `json:"txnStatus"`
	TxnMessage          string         `json:"txnMessage"`
	BHTxnID             string         `json:"bhTxnId"`
	PaymentStatusDetail map[string]any `json:"PaymentStatusDetails,omitempty"`
	RawPayload          []byte         `json:"-"`
}

type envelope struct {
	PayloadData string `json:"payLoadData"`
	TID         string `json:"tid"`
}
