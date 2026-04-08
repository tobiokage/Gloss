package billing

import (
	"fmt"
	"sort"
	"strings"

	"gloss/internal/shared/enums"
)

func CalculateBill(input CalculatorInput) (CalculationResult, error) {
	if err := ValidateCalculatorInput(input); err != nil {
		return CalculationResult{}, err
	}

	lineGrossAmounts := make([]int64, len(input.Lines))
	serviceGrossAmount := int64(0)
	for i, line := range input.Lines {
		gross := line.UnitPrice * line.Quantity
		lineGrossAmounts[i] = gross
		serviceGrossAmount += gross
	}

	discounts := allocateDiscountByLargestRemainder(lineGrossAmounts, input.DiscountAmount, serviceGrossAmount)
	calculatedLines := make([]CalculatedBillLine, 0, len(input.Lines))

	serviceNetAmount := int64(0)
	taxableBaseTotal := int64(0)
	taxTotal := int64(0)
	discountTotal := int64(0)

	for i, line := range input.Lines {
		lineGrossAmount := lineGrossAmounts[i]
		lineDiscountAmount := discounts[i]
		lineNetAmount := lineGrossAmount - lineDiscountAmount
		taxableBaseAmount := (lineNetAmount * taxInclusiveBaseNumerator) / taxInclusiveBaseDivisor
		taxAmount := lineNetAmount - taxableBaseAmount
		commissionAmount := (lineNetAmount * commissionPercent) / 100

		serviceNetAmount += lineNetAmount
		taxableBaseTotal += taxableBaseAmount
		taxTotal += taxAmount
		discountTotal += lineDiscountAmount

		calculatedLines = append(calculatedLines, CalculatedBillLine{
			CatalogueItemID:      line.CatalogueItemID,
			ServiceName:          line.ServiceName,
			AssignedStaffID:      line.AssignedStaffID,
			UnitPrice:            line.UnitPrice,
			Quantity:             line.Quantity,
			LineGrossAmount:      lineGrossAmount,
			LineDiscountAmount:   lineDiscountAmount,
			LineNetAmount:        lineNetAmount,
			TaxableBaseAmount:    taxableBaseAmount,
			TaxAmount:            taxAmount,
			CommissionBaseAmount: lineNetAmount,
			CommissionAmount:     commissionAmount,
		})
	}

	totalAmount := serviceNetAmount + input.TipAmount
	amountPaid, amountDue := derivePaidAndDueAmounts(totalAmount, input.Payment)
	status, err := DeriveCreateBillStatus(input.Payment.Mode)
	if err != nil {
		return CalculationResult{}, err
	}

	result := CalculationResult{
		Lines:          calculatedLines,
		TipAllocations: cloneTipAllocations(input.TipAllocations),
		Totals: CalculatedTotals{
			ServiceGrossAmount: serviceGrossAmount,
			DiscountAmount:     input.DiscountAmount,
			ServiceNetAmount:   serviceNetAmount,
			TipAmount:          input.TipAmount,
			TaxableBaseAmount:  taxableBaseTotal,
			TaxAmount:          taxTotal,
			TotalAmount:        totalAmount,
			AmountPaid:         amountPaid,
			AmountDue:          amountDue,
		},
		PaymentMode: input.Payment.Mode,
		Status:      status,
	}

	if err := validateCalculationInvariants(result, discountTotal); err != nil {
		return CalculationResult{}, err
	}

	return result, nil
}

func DeriveCreateBillStatus(mode PaymentMode) (enums.BillStatus, error) {
	switch mode {
	case PaymentModeCash:
		return enums.BillStatusPaid, nil
	case PaymentModeUPI:
		return enums.BillStatusPaymentPending, nil
	case PaymentModeSplit:
		return enums.BillStatusPartiallyPaid, nil
	default:
		return "", invalidRequest("payment.mode must be one of CASH, UPI, SPLIT")
	}
}

func FormatBillNumber(input BillNumberInput) (string, error) {
	storeCode := strings.ToUpper(strings.TrimSpace(input.StoreCode))
	if storeCode == "" {
		return "", invalidRequest("store_code is required for bill number formatting")
	}
	if input.Sequence <= 0 {
		return "", invalidRequest("bill sequence must be greater than 0")
	}
	if input.Date.IsZero() {
		return "", invalidRequest("bill date is required for bill number formatting")
	}

	return fmt.Sprintf("%s-%s-%0*d", storeCode, input.Date.Format("20060102"), billNumberSequenceWidth, input.Sequence), nil
}

func maxDiscountAmount(serviceGrossAmount int64) int64 {
	return (serviceGrossAmount * maxDiscountPercent) / 100
}

func allocateDiscountByLargestRemainder(lineGrossAmounts []int64, discountAmount int64, serviceGrossAmount int64) []int64 {
	allocations := make([]int64, len(lineGrossAmounts))
	if discountAmount == 0 || len(lineGrossAmounts) == 0 || serviceGrossAmount == 0 {
		return allocations
	}

	type remainderRow struct {
		index     int
		remainder int64
	}

	remainders := make([]remainderRow, 0, len(lineGrossAmounts))
	allocated := int64(0)

	for i, lineGrossAmount := range lineGrossAmounts {
		numerator := discountAmount * lineGrossAmount
		baseAllocation := numerator / serviceGrossAmount
		remainder := numerator % serviceGrossAmount

		allocations[i] = baseAllocation
		allocated += baseAllocation
		remainders = append(remainders, remainderRow{
			index:     i,
			remainder: remainder,
		})
	}

	remaining := discountAmount - allocated
	if remaining <= 0 {
		return allocations
	}

	sort.SliceStable(remainders, func(i int, j int) bool {
		return remainders[i].remainder > remainders[j].remainder
	})

	for i := int64(0); i < remaining; i++ {
		target := remainders[i].index
		allocations[target]++
	}

	return allocations
}

func derivePaidAndDueAmounts(totalAmount int64, payment PaymentInput) (int64, int64) {
	switch payment.Mode {
	case PaymentModeCash:
		return totalAmount, 0
	case PaymentModeUPI:
		return 0, totalAmount
	case PaymentModeSplit:
		return payment.CashAmount, totalAmount - payment.CashAmount
	default:
		return 0, totalAmount
	}
}

func cloneTipAllocations(allocations []TipAllocation) []TipAllocation {
	cloned := make([]TipAllocation, len(allocations))
	copy(cloned, allocations)
	return cloned
}

func validateCalculationInvariants(result CalculationResult, lineDiscountTotal int64) error {
	if lineDiscountTotal != result.Totals.DiscountAmount {
		return invalidRequest("invariant failed: sum of line discounts must equal bill discount")
	}

	lineNetTotal := int64(0)
	taxablePlusTaxTotal := int64(0)
	for _, line := range result.Lines {
		lineNetTotal += line.LineNetAmount
		taxablePlusTaxTotal += line.TaxableBaseAmount + line.TaxAmount
	}

	if lineNetTotal != result.Totals.ServiceNetAmount {
		return invalidRequest("invariant failed: sum of line net amounts must equal service net total")
	}
	if taxablePlusTaxTotal != result.Totals.ServiceNetAmount {
		return invalidRequest("invariant failed: taxable bases plus tax amounts must equal service net total")
	}

	tipTotal := int64(0)
	for _, allocation := range result.TipAllocations {
		tipTotal += allocation.TipAmount
	}
	if tipTotal != result.Totals.TipAmount {
		return invalidRequest("invariant failed: sum of tip allocations must equal tip amount")
	}

	if result.Totals.AmountPaid+result.Totals.AmountDue != result.Totals.TotalAmount {
		return invalidRequest("invariant failed: amount paid plus amount due must equal total amount")
	}

	switch result.Status {
	case enums.BillStatusPaid:
		if result.Totals.AmountDue != 0 {
			return invalidRequest("invariant failed: PAID requires amount_due = 0")
		}
	case enums.BillStatusPaymentPending:
		if result.Totals.AmountPaid != 0 {
			return invalidRequest("invariant failed: PAYMENT_PENDING requires amount_paid = 0")
		}
	case enums.BillStatusPartiallyPaid:
		if result.Totals.AmountPaid <= 0 || result.Totals.AmountDue <= 0 {
			return invalidRequest("invariant failed: PARTIALLY_PAID requires amount_paid > 0 and amount_due > 0")
		}
	}

	return nil
}
