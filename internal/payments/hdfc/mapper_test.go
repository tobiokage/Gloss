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

func TestTxnStatusMapping(t *testing.T) {
	testCases := []struct {
		status string
		want   enums.PaymentStatus
	}{
		{status: TxnStatusInProgress, want: enums.PaymentStatusPending},
		{status: TxnStatusSuccess, want: enums.PaymentStatusSuccess},
		{status: TxnStatusFailed, want: enums.PaymentStatusFailed},
		{status: TxnStatusCanceled, want: enums.PaymentStatusCancelled},
	}

	for _, tc := range testCases {
		if got := MapTxnStatus(tc.status); got != tc.want {
			t.Fatalf("MapTxnStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
