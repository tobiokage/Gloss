package billing

import (
	"testing"
	"time"

	"gloss/internal/shared/enums"
)

func TestActiveOnlinePaymentCannotCancelWithoutProviderTxnID(t *testing.T) {
	now := time.Now().UTC()
	gateway := "HDFC"
	providerRequestID := "SALE123"
	terminalTID := "63000019"

	response := MapBillGraphToCreateBillResponse(BillGraph{
		Bill: BillRecord{ID: "bill-1", CreatedAt: now},
		Payments: []BillPaymentRecord{{
			ID:                "payment-1",
			Gateway:           &gateway,
			PaymentMethod:     string(PaymentModeOnline),
			Status:            string(enums.PaymentStatusPending),
			ProviderRequestID: &providerRequestID,
			TerminalTID:       &terminalTID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}},
	})

	if response.ActiveOnlinePayment == nil {
		t.Fatal("expected active online payment")
	}
	if response.ActiveOnlinePayment.CanCancelAttempt {
		t.Fatal("expected can_cancel_attempt=false without provider_txn_id")
	}
}

func TestActiveOnlinePaymentCanCancelOnlyWithProviderTxnIDAndTerminalTID(t *testing.T) {
	now := time.Now().UTC()
	gateway := "HDFC"
	providerRequestID := "SALE123"
	providerTxnID := "BH123"
	terminalTID := "63000019"

	response := MapBillGraphToCreateBillResponse(BillGraph{
		Bill: BillRecord{ID: "bill-1", CreatedAt: now},
		Payments: []BillPaymentRecord{{
			ID:                "payment-1",
			Gateway:           &gateway,
			PaymentMethod:     string(PaymentModeOnline),
			Status:            string(enums.PaymentStatusPending),
			ProviderRequestID: &providerRequestID,
			ProviderTxnID:     &providerTxnID,
			TerminalTID:       &terminalTID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}},
	})

	if response.ActiveOnlinePayment == nil {
		t.Fatal("expected active online payment")
	}
	if !response.ActiveOnlinePayment.CanCancelAttempt {
		t.Fatal("expected can_cancel_attempt=true for pending HDFC payment with provider_txn_id and terminal_tid")
	}

	initiatedResponse := MapBillGraphToCreateBillResponse(BillGraph{
		Bill: BillRecord{ID: "bill-1", CreatedAt: now},
		Payments: []BillPaymentRecord{{
			ID:                "payment-1",
			Gateway:           &gateway,
			PaymentMethod:     string(PaymentModeOnline),
			Status:            string(enums.PaymentStatusInitiated),
			ProviderRequestID: &providerRequestID,
			ProviderTxnID:     &providerTxnID,
			TerminalTID:       &terminalTID,
			CreatedAt:         now,
			UpdatedAt:         now,
		}},
	})

	if initiatedResponse.ActiveOnlinePayment == nil {
		t.Fatal("expected active online payment")
	}
	if initiatedResponse.ActiveOnlinePayment.CanCancelAttempt {
		t.Fatal("expected can_cancel_attempt=false before payment is provider-cancellable")
	}
}
