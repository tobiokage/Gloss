package hdfc

import (
	"fmt"

	"gloss/internal/shared/enums"
)

const (
	TxnStatusInProgress = "InProgress"
	TxnStatusSuccess    = "Success"
	TxnStatusFailed     = "Failed"
	TxnStatusCanceled   = "Canceled"
)

func FormatRupeeAmount(amountPaise int64) string {
	rupees := amountPaise / 100
	paise := amountPaise % 100
	if paise < 0 {
		paise = -paise
	}
	return fmt.Sprintf("%d.%02d", rupees, paise)
}

func MapTxnStatus(txnStatus string) enums.PaymentStatus {
	switch txnStatus {
	case TxnStatusInProgress:
		return enums.PaymentStatusPending
	case TxnStatusSuccess:
		return enums.PaymentStatusSuccess
	case TxnStatusFailed:
		return enums.PaymentStatusFailed
	case TxnStatusCanceled:
		return enums.PaymentStatusCancelled
	default:
		return enums.PaymentStatusPending
	}
}

func BuildCreateSalePayload(req CreateSaleRequest) map[string]any {
	payload := map[string]any{
		"saleTxnId":    req.SaleTxnID,
		"saleAmount":   FormatRupeeAmount(req.AmountPaise),
		"email":        nil,
		"customerName": nil,
		"skuIds":       nil,
		"field1":       nil,
		"field2":       nil,
		"field3":       nil,
		"field4":       nil,
		"field5":       nil,
	}
	if req.Description != "" {
		payload["description"] = req.Description
	}
	return payload
}

func BuildTransactionStatusPayload(req TransactionStatusRequest) map[string]any {
	payload := map[string]any{
		"bhTxnId": req.BHTxnID,
	}
	if req.SaleTxnID != "" {
		payload["saleTxnId"] = req.SaleTxnID
	}
	return payload
}

func BuildCancelSalePayload(req CancelSaleRequest) map[string]any {
	payload := map[string]any{
		"bhTxnId": req.BHTxnID,
		"tid":     req.TID,
	}
	if req.SaleTxnID != "" {
		payload["saleTxnId"] = req.SaleTxnID
	}
	return payload
}
