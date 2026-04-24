package billing

import (
	"regexp"
	"strconv"
	"strings"

	apperrors "gloss/internal/shared/errors"
)

var createBillUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

type ValidatedCreateBillRequest struct {
	Items          []ValidatedCreateBillItem
	DiscountAmount int64
	TipAmount      int64
	TipAllocations []TipAllocation
	Payment        PaymentInput
}

type ValidatedCreateBillItem struct {
	CatalogueItemID string
	Quantity        int64
	AssignedStaffID string
}

func ValidateCreateBillRequest(req CreateBillRequest) (ValidatedCreateBillRequest, error) {
	if len(req.Items) == 0 {
		return ValidatedCreateBillRequest{}, invalidRequest("items are required")
	}

	validatedItems := make([]ValidatedCreateBillItem, 0, len(req.Items))
	for i, item := range req.Items {
		catalogueItemID, err := validateCreateBillUUID("items["+itoa(i)+"].catalogue_item_id", item.CatalogueItemID)
		if err != nil {
			return ValidatedCreateBillRequest{}, err
		}
		if item.Quantity <= 0 {
			return ValidatedCreateBillRequest{}, invalidRequest("items[" + itoa(i) + "].quantity must be greater than 0")
		}
		if item.Quantity > MaxBillItemQuantity {
			return ValidatedCreateBillRequest{}, invalidRequest("items[" + itoa(i) + "].quantity exceeds maximum allowed quantity")
		}
		assignedStaffID, err := validateCreateBillUUID("items["+itoa(i)+"].assigned_staff_id", item.AssignedStaffID)
		if err != nil {
			return ValidatedCreateBillRequest{}, err
		}

		validatedItems = append(validatedItems, ValidatedCreateBillItem{
			CatalogueItemID: catalogueItemID,
			Quantity:        item.Quantity,
			AssignedStaffID: assignedStaffID,
		})
	}

	discountAmount := int64(0)
	if req.DiscountAmount != nil {
		discountAmount = *req.DiscountAmount
	}
	if discountAmount < 0 {
		return ValidatedCreateBillRequest{}, invalidRequest("discount_amount cannot be negative")
	}

	tipAmount := int64(0)
	if req.TipAmount != nil {
		tipAmount = *req.TipAmount
	}
	if tipAmount < 0 {
		return ValidatedCreateBillRequest{}, invalidRequest("tip_amount cannot be negative")
	}
	if tipAmount > MaxBillTipAmountPaise {
		return ValidatedCreateBillRequest{}, invalidRequest("tip_amount exceeds maximum allowed amount")
	}

	payment, err := validateAndNormalizePayment(req.Payment)
	if err != nil {
		return ValidatedCreateBillRequest{}, err
	}

	tipAllocations, err := validateAndNormalizeTipAllocations(req.TipAllocations, tipAmount)
	if err != nil {
		return ValidatedCreateBillRequest{}, err
	}

	if payment.Mode == PaymentModeSplit && payment.CashAmount <= 0 {
		return ValidatedCreateBillRequest{}, invalidRequest("payment.cash_amount must be greater than 0 for SPLIT")
	}
	if payment.Mode != PaymentModeSplit && payment.CashAmount != 0 {
		return ValidatedCreateBillRequest{}, invalidRequest("payment.cash_amount is only allowed for SPLIT mode")
	}

	return ValidatedCreateBillRequest{
		Items:          validatedItems,
		DiscountAmount: discountAmount,
		TipAmount:      tipAmount,
		TipAllocations: tipAllocations,
		Payment:        payment,
	}, nil
}

func ValidateCalculatorInput(input CalculatorInput) error {
	if len(input.Lines) == 0 {
		return invalidRequest("at least one authoritative line is required")
	}

	serviceGrossAmount := int64(0)
	for i, line := range input.Lines {
		if strings.TrimSpace(line.CatalogueItemID) == "" {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].catalogue_item_id is required")
		}
		if strings.TrimSpace(line.AssignedStaffID) == "" {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].assigned_staff_id is required")
		}
		if line.UnitPrice < 0 {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].unit_price cannot be negative")
		}
		if line.UnitPrice > MaxBillLineUnitPricePaise {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].unit_price exceeds maximum allowed amount")
		}
		if line.Quantity <= 0 {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].quantity must be greater than 0")
		}
		if line.Quantity > MaxBillItemQuantity {
			return invalidRequest("authoritative_lines[" + itoa(i) + "].quantity exceeds maximum allowed quantity")
		}
		lineGrossAmount, err := checkedMul(line.UnitPrice, line.Quantity, "line gross amount")
		if err != nil {
			return err
		}
		serviceGrossAmount, err = checkedAdd(serviceGrossAmount, lineGrossAmount, "service gross amount")
		if err != nil {
			return err
		}
		if serviceGrossAmount > MaxBillTotalAmountPaise {
			return invalidRequest("service subtotal exceeds maximum bill amount")
		}
	}

	if input.DiscountAmount < 0 {
		return invalidRequest("discount_amount cannot be negative")
	}
	if input.TipAmount < 0 {
		return invalidRequest("tip_amount cannot be negative")
	}
	if input.TipAmount > MaxBillTipAmountPaise {
		return invalidRequest("tip_amount exceeds maximum allowed amount")
	}

	maxDiscount, err := maxDiscountAmount(serviceGrossAmount)
	if err != nil {
		return err
	}
	if input.DiscountAmount > maxDiscount {
		return invalidRequest("discount_amount exceeds 30% cap of service subtotal")
	}

	if err := validateTipAllocations(input.TipAllocations, input.TipAmount); err != nil {
		return err
	}

	serviceNetAmount := serviceGrossAmount - input.DiscountAmount
	totalAmount, err := checkedAdd(serviceNetAmount, input.TipAmount, "total amount")
	if err != nil {
		return err
	}
	if totalAmount <= 0 {
		return invalidRequest("total_amount must be greater than 0")
	}
	if totalAmount > MaxBillTotalAmountPaise {
		return invalidRequest("total_amount exceeds maximum bill amount")
	}

	if _, err := validatePaymentMode(input.Payment.Mode); err != nil {
		return err
	}
	switch input.Payment.Mode {
	case PaymentModeSplit:
		if input.Payment.CashAmount <= 0 {
			return invalidRequest("payment.cash_amount must be greater than 0 for SPLIT")
		}
		if input.Payment.CashAmount >= totalAmount {
			return invalidRequest("payment.cash_amount must be less than total_amount for SPLIT")
		}
	default:
		if input.Payment.CashAmount != 0 {
			return invalidRequest("payment.cash_amount is only allowed for SPLIT mode")
		}
	}

	return nil
}

func validateAndNormalizePayment(payment CreateBillPaymentRequest) (PaymentInput, error) {
	mode, err := validatePaymentMode(PaymentMode(strings.ToUpper(strings.TrimSpace(payment.Mode))))
	if err != nil {
		return PaymentInput{}, err
	}

	cashAmount := int64(0)
	if payment.CashAmount != nil {
		cashAmount = *payment.CashAmount
	}

	if cashAmount < 0 {
		return PaymentInput{}, invalidRequest("payment.cash_amount cannot be negative")
	}

	return PaymentInput{
		Mode:       mode,
		CashAmount: cashAmount,
	}, nil
}

func validatePaymentMode(mode PaymentMode) (PaymentMode, error) {
	switch mode {
	case PaymentModeCash, PaymentModeOnline, PaymentModeSplit:
		return mode, nil
	default:
		return "", invalidRequest("payment.mode must be one of CASH, ONLINE, SPLIT")
	}
}

func validateAndNormalizeTipAllocations(raw []CreateBillTipAllocationDTO, tipAmount int64) ([]TipAllocation, error) {
	normalized := make([]TipAllocation, 0, len(raw))

	for _, allocation := range raw {
		normalized = append(normalized, TipAllocation{
			StaffID:   strings.TrimSpace(allocation.StaffID),
			TipAmount: allocation.TipAmount,
		})
	}

	if err := validateTipAllocations(normalized, tipAmount); err != nil {
		return nil, err
	}

	return normalized, nil
}

func invalidRequest(message string) error {
	return apperrors.New(apperrors.CodeInvalidRequest, message)
}

func itoa(value int) string {
	return strconv.Itoa(value)
}

func validateTipAllocations(allocations []TipAllocation, tipAmount int64) error {
	if tipAmount == 0 {
		if len(allocations) > 0 {
			return invalidRequest("tip_allocations must be empty when tip_amount is 0")
		}
		return nil
	}

	allocationTotal := int64(0)
	seenStaffIDs := make(map[string]struct{}, len(allocations))
	for i, allocation := range allocations {
		normalizedStaffID, err := validateCreateBillUUID("tip_allocations["+itoa(i)+"].staff_id", allocation.StaffID)
		if err != nil {
			return err
		}
		allocation.StaffID = normalizedStaffID
		allocations[i] = allocation
		if _, exists := seenStaffIDs[allocation.StaffID]; exists {
			return invalidRequest("tip_allocations[" + itoa(i) + "].staff_id must be unique")
		}
		seenStaffIDs[allocation.StaffID] = struct{}{}
		if allocation.TipAmount < 0 {
			return invalidRequest("tip_allocations[" + itoa(i) + "].tip_amount cannot be negative")
		}
		if allocation.TipAmount > MaxBillTipAmountPaise {
			return invalidRequest("tip_allocations[" + itoa(i) + "].tip_amount exceeds maximum allowed amount")
		}
		var addErr error
		allocationTotal, addErr = checkedAdd(allocationTotal, allocation.TipAmount, "tip allocation amount")
		if addErr != nil {
			return addErr
		}
	}

	if allocationTotal != tipAmount {
		return invalidRequest("tip_allocations sum must match tip_amount")
	}
	return nil
}

func validateCreateBillUUID(fieldName string, rawValue string) (string, error) {
	normalizedValue := strings.TrimSpace(rawValue)
	if normalizedValue == "" {
		return "", invalidRequest(fieldName + " is required")
	}
	if !createBillUUIDPattern.MatchString(normalizedValue) {
		return "", invalidRequest(fieldName + " must be a valid UUID")
	}

	return normalizedValue, nil
}
