package hdfc

import "encoding/json"

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
	StatusCode          string               `json:"statusCode"`
	StatusMessage       string               `json:"statusMessage"`
	SaleTxnID           string               `json:"saleTxnId"`
	SaleAmount          string               `json:"saleAmount"`
	SaleDateTime        string               `json:"saleDateTime"`
	TxnStatus           string               `json:"txnStatus"`
	TxnMessage          string               `json:"txnMessage"`
	BHTxnID             string               `json:"bhTxnId"`
	PaymentStatusDetail PaymentStatusDetails `json:"PaymentStatusDetails,omitempty"`
	RawPayload          []byte               `json:"-"`
}

type PaymentStatusDetails []map[string]any

func (d *PaymentStatusDetails) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*d = nil
		return nil
	}

	var details []map[string]any
	if err := json.Unmarshal(data, &details); err == nil {
		*d = details
		return nil
	}

	var detail map[string]any
	if err := json.Unmarshal(data, &detail); err != nil {
		return err
	}
	*d = []map[string]any{detail}
	return nil
}

type envelope struct {
	PayloadData string `json:"payLoadData"`
	TID         string `json:"tid"`
}
