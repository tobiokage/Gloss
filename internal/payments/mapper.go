package payments

import (
	"strings"

	"gloss/internal/payments/hdfc"
	"gloss/internal/shared/enums"
)

func mapHDFCSaleInitiationStatus(response hdfc.TransactionResponse) (string, bool) {
	switch strings.TrimSpace(response.TxnStatus) {
	case hdfc.TxnStatusFailed:
		return string(enums.PaymentStatusFailed), true
	case hdfc.TxnStatusCanceled:
		return string(enums.PaymentStatusCancelled), true
	default:
		return string(enums.PaymentStatusPending), false
	}
}
