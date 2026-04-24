package billing

import (
	"strings"
	"time"

	"gloss/internal/shared/enums"
)

type AuthoritativeCatalogueLine struct {
	CatalogueItemID string
	ServiceName     string
	UnitPrice       int64
}

func BuildCalculatorInput(
	request ValidatedCreateBillRequest,
	authoritativeItems map[string]AuthoritativeCatalogueLine,
) (CalculatorInput, error) {
	if len(authoritativeItems) == 0 {
		return CalculatorInput{}, invalidRequest("authoritative catalogue data is required")
	}

	lines := make([]AuthoritativeBillLineInput, 0, len(request.Items))
	for i, item := range request.Items {
		catalogueLine, found := authoritativeItems[item.CatalogueItemID]
		if !found {
			return CalculatorInput{}, invalidRequest("authoritative catalogue item not found for items[" + itoa(i) + "]")
		}

		if strings.TrimSpace(catalogueLine.CatalogueItemID) == "" || catalogueLine.UnitPrice <= 0 {
			return CalculatorInput{}, invalidRequest("invalid authoritative catalogue line for items[" + itoa(i) + "]")
		}

		lines = append(lines, AuthoritativeBillLineInput{
			CatalogueItemID: catalogueLine.CatalogueItemID,
			ServiceName:     catalogueLine.ServiceName,
			AssignedStaffID: item.AssignedStaffID,
			UnitPrice:       catalogueLine.UnitPrice,
			Quantity:        item.Quantity,
		})
	}

	return CalculatorInput{
		Lines:          lines,
		DiscountAmount: request.DiscountAmount,
		TipAmount:      request.TipAmount,
		TipAllocations: request.TipAllocations,
		Payment:        request.Payment,
	}, nil
}

func BuildCreateBillSuccessResponse(
	store StoreSnapshot,
	bill InsertBillInput,
	items []PersistedBillItem,
	tipAllocations []InsertTipAllocationInput,
	payments []InsertPaymentInput,
) CreateBillResponse {
	graph := BillGraph{
		Store: store,
		Bill: BillRecord{
			ID:                 bill.ID,
			BillNumber:         bill.BillNumber,
			Status:             bill.Status,
			PaymentModeSummary: bill.PaymentModeSummary,
			ServiceGrossAmount: bill.ServiceGrossAmount,
			DiscountAmount:     bill.DiscountAmount,
			ServiceNetAmount:   bill.ServiceNetAmount,
			TipAmount:          bill.TipAmount,
			TaxableBaseAmount:  bill.TaxableBaseAmount,
			TaxAmount:          bill.TaxAmount,
			TotalAmount:        bill.TotalAmount,
			AmountPaid:         bill.AmountPaid,
			AmountDue:          bill.AmountDue,
			CreatedAt:          bill.CreatedAt,
			PaidAt:             bill.PaidAt,
		},
		Items:          mapPersistedBillItems(items),
		TipAllocations: mapInsertedTipAllocations(tipAllocations),
		Payments:       mapInsertedPayments(payments),
	}

	return MapBillGraphToCreateBillResponse(graph)
}

func MapBillGraphToCreateBillResponse(graph BillGraph) CreateBillResponse {
	return CreateBillResponse{
		Bill:                mapBillHeader(graph.Bill),
		Items:               mapBillItems(graph.Items),
		TipAllocations:      mapTipAllocations(graph.TipAllocations),
		Payments:            mapBillPayments(graph.Payments),
		ActiveOnlinePayment: mapActiveOnlinePayment(graph.Payments),
		Receipt:             mapReceiptPayload(graph),
	}
}

func mapBillHeader(record BillRecord) CreatedBillHeaderResponse {
	return CreatedBillHeaderResponse{
		ID:                 record.ID,
		BillNumber:         record.BillNumber,
		Status:             record.Status,
		PaymentModeSummary: record.PaymentModeSummary,
		ServiceGrossAmount: record.ServiceGrossAmount,
		DiscountAmount:     record.DiscountAmount,
		ServiceNetAmount:   record.ServiceNetAmount,
		TipAmount:          record.TipAmount,
		TaxableBaseAmount:  record.TaxableBaseAmount,
		TaxAmount:          record.TaxAmount,
		TotalAmount:        record.TotalAmount,
		AmountPaid:         record.AmountPaid,
		AmountDue:          record.AmountDue,
		CreatedAt:          record.CreatedAt.UTC(),
		PaidAt:             optionalUTCTime(record.PaidAt),
	}
}

func mapBillItems(items []BillItemRecord) []CreatedBillItemResponse {
	response := make([]CreatedBillItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, CreatedBillItemResponse{
			ID:                   item.ID,
			CatalogueItemID:      item.CatalogueItemID,
			ServiceName:          item.ServiceName,
			AssignedStaffID:      item.AssignedStaffID,
			UnitPrice:            item.UnitPrice,
			Quantity:             item.Quantity,
			LineGrossAmount:      item.LineGrossAmount,
			LineDiscountAmount:   item.LineDiscountAmount,
			LineNetAmount:        item.LineNetAmount,
			TaxableBaseAmount:    item.TaxableBaseAmount,
			TaxAmount:            item.TaxAmount,
			CommissionBaseAmount: item.CommissionBaseAmount,
			CommissionAmount:     item.CommissionAmount,
		})
	}
	return response
}

func mapTipAllocations(allocations []BillTipAllocationRecord) []CreatedBillTipAllocationResponse {
	response := make([]CreatedBillTipAllocationResponse, 0, len(allocations))
	for _, allocation := range allocations {
		response = append(response, CreatedBillTipAllocationResponse{
			ID:        allocation.ID,
			StaffID:   allocation.StaffID,
			TipAmount: allocation.TipAmount,
		})
	}
	return response
}

func mapBillPayments(payments []BillPaymentRecord) []CreatedBillPaymentResponse {
	response := make([]CreatedBillPaymentResponse, 0, len(payments))
	for _, payment := range payments {
		response = append(response, CreatedBillPaymentResponse{
			ID:            payment.ID,
			Gateway:       payment.Gateway,
			PaymentMethod: payment.PaymentMethod,
			Amount:        payment.Amount,
			Status:        payment.Status,
			CreatedAt:     payment.CreatedAt.UTC(),
			UpdatedAt:     payment.UpdatedAt.UTC(),
			VerifiedAt:    optionalUTCTime(payment.VerifiedAt),
		})
	}
	return response
}

func mapActiveOnlinePayment(payments []BillPaymentRecord) *ActiveOnlinePaymentResponse {
	for _, payment := range payments {
		if payment.PaymentMethod != string(PaymentModeOnline) {
			continue
		}
		if payment.Gateway == nil || *payment.Gateway != "HDFC" {
			continue
		}
		if payment.Status != "INITIATED" && payment.Status != "PENDING" {
			continue
		}

		providerRequestID := ""
		if payment.ProviderRequestID != nil {
			providerRequestID = *payment.ProviderRequestID
		}
		canCancelAttempt := payment.Status == string(enums.PaymentStatusPending) &&
			hasText(payment.ProviderTxnID) &&
			hasText(payment.TerminalTID)

		return &ActiveOnlinePaymentResponse{
			PaymentID:         payment.ID,
			Gateway:           "HDFC",
			Status:            payment.Status,
			TerminalFlow:      "HDFC_TERMINAL_OWNED",
			ProviderRequestID: providerRequestID,
			ProviderTxnID:     payment.ProviderTxnID,
			CanCancelAttempt:  canCancelAttempt,
		}
	}
	return nil
}

func hasText(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func mapReceiptPayload(graph BillGraph) ReceiptPayloadResponse {
	return ReceiptPayloadResponse{
		Store: ReceiptStoreResponse{
			ID:       graph.Store.ID,
			Name:     graph.Store.Name,
			Code:     graph.Store.Code,
			Location: graph.Store.Location,
		},
		Bill: ReceiptBillResponse{
			ID:                 graph.Bill.ID,
			BillNumber:         graph.Bill.BillNumber,
			Status:             graph.Bill.Status,
			PaymentModeSummary: graph.Bill.PaymentModeSummary,
			CreatedAt:          graph.Bill.CreatedAt.UTC(),
			PaidAt:             optionalUTCTime(graph.Bill.PaidAt),
		},
		Items:    mapReceiptItems(graph.Items),
		Payments: mapReceiptPayments(graph.Payments),
		Totals: ReceiptTotalsResponse{
			ServiceGrossAmount: graph.Bill.ServiceGrossAmount,
			DiscountAmount:     graph.Bill.DiscountAmount,
			ServiceNetAmount:   graph.Bill.ServiceNetAmount,
			TipAmount:          graph.Bill.TipAmount,
			TaxableBaseAmount:  graph.Bill.TaxableBaseAmount,
			TaxAmount:          graph.Bill.TaxAmount,
			TotalAmount:        graph.Bill.TotalAmount,
			AmountPaid:         graph.Bill.AmountPaid,
			AmountDue:          graph.Bill.AmountDue,
		},
	}
}

func mapReceiptItems(items []BillItemRecord) []ReceiptItemResponse {
	response := make([]ReceiptItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, ReceiptItemResponse{
			ServiceName:     item.ServiceName,
			Quantity:        item.Quantity,
			UnitPrice:       item.UnitPrice,
			LineGrossAmount: item.LineGrossAmount,
			LineNetAmount:   item.LineNetAmount,
		})
	}
	return response
}

func mapReceiptPayments(payments []BillPaymentRecord) []ReceiptPaymentResponse {
	response := make([]ReceiptPaymentResponse, 0, len(payments))
	for _, payment := range payments {
		response = append(response, ReceiptPaymentResponse{
			PaymentMethod: payment.PaymentMethod,
			Amount:        payment.Amount,
			Status:        payment.Status,
		})
	}
	return response
}

func mapPersistedBillItems(items []PersistedBillItem) []BillItemRecord {
	response := make([]BillItemRecord, 0, len(items))
	for _, item := range items {
		response = append(response, BillItemRecord{
			ID:                   item.ID,
			CatalogueItemID:      item.CatalogueItemID,
			ServiceName:          item.ServiceName,
			AssignedStaffID:      item.AssignedStaffID,
			UnitPrice:            item.UnitPrice,
			Quantity:             item.Quantity,
			LineGrossAmount:      item.LineGrossAmount,
			LineDiscountAmount:   item.LineDiscountAmount,
			LineNetAmount:        item.LineNetAmount,
			TaxableBaseAmount:    item.TaxableBaseAmount,
			TaxAmount:            item.TaxAmount,
			CommissionBaseAmount: item.CommissionBaseAmount,
			CommissionAmount:     item.CommissionAmount,
		})
	}
	return response
}

func mapInsertedTipAllocations(allocations []InsertTipAllocationInput) []BillTipAllocationRecord {
	response := make([]BillTipAllocationRecord, 0, len(allocations))
	for _, allocation := range allocations {
		response = append(response, BillTipAllocationRecord{
			ID:        allocation.ID,
			StaffID:   allocation.StaffID,
			TipAmount: allocation.TipAmount,
		})
	}
	return response
}

func mapInsertedPayments(payments []InsertPaymentInput) []BillPaymentRecord {
	response := make([]BillPaymentRecord, 0, len(payments))
	for _, payment := range payments {
		response = append(response, BillPaymentRecord{
			ID:            payment.ID,
			Gateway:       payment.Gateway,
			PaymentMethod: payment.PaymentMethod,
			Amount:        payment.Amount,
			Status:        payment.Status,
			CreatedAt:     payment.CreatedAt,
			UpdatedAt:     payment.UpdatedAt,
			VerifiedAt:    payment.VerifiedAt,
		})
	}
	return response
}

func optionalUTCTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utcValue := value.UTC()
	return &utcValue
}
