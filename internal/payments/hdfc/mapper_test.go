package hdfc

import (
	"encoding/json"
	"strings"
	"testing"

	"gloss/internal/shared/enums"
)

func TestFormatRupeeAmount(t *testing.T) {
	testCases := []struct {
		paise int64
		want  string
	}{
		{paise: 1, want: "0.01"},
		{paise: 100, want: "1.00"},
		{paise: 10550, want: "105.50"},
	}

	for _, tc := range testCases {
		if got := FormatRupeeAmount(tc.paise); got != tc.want {
			t.Fatalf("FormatRupeeAmount(%d) = %q, want %q", tc.paise, got, tc.want)
		}
	}
}

func TestCreateSalePayloadOmitsMobileNo(t *testing.T) {
	payload := BuildCreateSalePayload(CreateSaleRequest{
		TID:         "63000019",
		SaleTxnID:   "SALE123",
		AmountPaise: 10550,
		Description: "STORE-1",
	})

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}
	if strings.Contains(string(encoded), "mobileNo") {
		t.Fatalf("mobileNo must be omitted from HDFC sale payload: %s", string(encoded))
	}
	if payload["saleAmount"] != "105.50" {
		t.Fatalf("expected saleAmount 105.50, got %#v", payload["saleAmount"])
	}
}

func TestTransactionStatusPayloadUsesDocumentedIdentifiers(t *testing.T) {
	payload := BuildTransactionStatusPayload(TransactionStatusRequest{
		TID:       "63000019",
		BHTxnID:   "BH123",
		SaleTxnID: "SALE123",
	})

	if payload["bhTxnId"] != "BH123" {
		t.Fatalf("expected bhTxnId in status payload, got %#v", payload["bhTxnId"])
	}
	if payload["saleTxnId"] != "SALE123" {
		t.Fatalf("expected saleTxnId in status payload, got %#v", payload["saleTxnId"])
	}
	if _, exists := payload["tid"]; exists {
		t.Fatalf("tid belongs in encrypted envelope, not status inner payload: %#v", payload)
	}
}

func TestTxnStatusMapping(t *testing.T) {
	testCases := []struct {
		status string
		want   enums.PaymentStatus
	}{
		{status: TxnStatusInProgress, want: enums.PaymentStatusPending},
		{status: txnStatusInProgressCompact, want: enums.PaymentStatusPending},
		{status: TxnStatusSuccess, want: enums.PaymentStatusSuccess},
		{status: TxnStatusFailed, want: enums.PaymentStatusFailed},
		{status: TxnStatusCanceled, want: enums.PaymentStatusCancelled},
		{status: "Unknown", want: enums.PaymentStatusPending},
		{status: " " + TxnStatusSuccess + " ", want: enums.PaymentStatusSuccess},
	}

	for _, tc := range testCases {
		if got := MapTxnStatus(tc.status); got != tc.want {
			t.Fatalf("MapTxnStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestTransactionStatusResponseDocumentedPaymentStatusDetailsArray(t *testing.T) {
	const payload = `{
		"statusCode":"100",
		"statusMessage":"Success",
		"saleTxnId":"638594898314708295",
		"saleAmount":"10.00",
		"bhTxnId":"6A7E55D2D3DB46198BC2CA1C013C186C",
		"saleDateTime":"2024-Aug-17 11:10:40",
		"txnStatus":"Success",
		"txnMessage":"Transaction Success/Completed",
		"PaymentStatusDetails":[{"paymentMode":"Card Payment","txnTID":"63000019"}]
	}`

	var response TransactionResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		t.Fatalf("failed to decode documented status response: %v", err)
	}
	if got := ActualCompletionMode(response); got != "Card Payment" {
		t.Fatalf("ActualCompletionMode() = %q, want Card Payment", got)
	}
	if got := MapTxnStatus(response.TxnStatus); got != enums.PaymentStatusSuccess {
		t.Fatalf("MapTxnStatus(%q) = %q, want SUCCESS", response.TxnStatus, got)
	}
}
