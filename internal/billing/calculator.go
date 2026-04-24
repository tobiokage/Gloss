package billing

import (
	"fmt"
	"math"
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
		gross, err := checkedMul(line.UnitPrice, line.Quantity, "line gross amount")
		if err != nil {
			return CalculationResult{}, err
		}
		lineGrossAmounts[i] = gross
		serviceGrossAmount, err = checkedAdd(serviceGrossAmount, gross, "service gross amount")
		if err != nil {
			return CalculationResult{}, err
		}
	}

	discounts, err := allocateDiscountByLargestRemainder(lineGrossAmounts, input.DiscountAmount, serviceGrossAmount)
	if err != nil {
		return CalculationResult{}, err
	}
	calculatedLines := make([]CalculatedBillLine, 0, len(input.Lines))

	serviceNetAmount := int64(0)
	taxableBaseTotal := int64(0)
	taxTotal := int64(0)
	discountTotal := int64(0)

	for i, line := range input.Lines {
		lineGrossAmount := lineGrossAmounts[i]
		lineDiscountAmount := discounts[i]
		lineNetAmount := lineGrossAmount - lineDiscountAmount
		taxableBaseAmount, err := checkedPercentFloor(lineNetAmount, taxInclusiveBaseNumerator, taxInclusiveBaseDivisor, "taxable base amount")
		if err != nil {
			return CalculationResult{}, err
		}
		taxAmount := lineNetAmount - taxableBaseAmount
		commissionAmount, err := checkedPercentFloor(lineNetAmount, commissionPercent, 100, "commission amount")
		if err != nil {
			return CalculationResult{}, err
		}

		serviceNetAmount, err = checkedAdd(serviceNetAmount, lineNetAmount, "service net amount")
		if err != nil {
			return CalculationResult{}, err
		}
		taxableBaseTotal, err = checkedAdd(taxableBaseTotal, taxableBaseAmount, "taxable base amount")
		if err != nil {
			return CalculationResult{}, err
		}
		taxTotal, err = checkedAdd(taxTotal, taxAmount, "tax amount")
		if err != nil {
			return CalculationResult{}, err
		}
		discountTotal, err = checkedAdd(discountTotal, lineDiscountAmount, "discount amount")
		if err != nil {
			return CalculationResult{}, err
		}

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

	totalAmount, err := checkedAdd(serviceNetAmount, input.TipAmount, "total amount")
	if err != nil {
		return CalculationResult{}, err
	}
	amountPaid, amountDue, err := derivePaidAndDueAmounts(totalAmount, input.Payment)
	if err != nil {
		return CalculationResult{}, err
	}
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
	case PaymentModeOnline:
		return enums.BillStatusPaymentPending, nil
	case PaymentModeSplit:
		return enums.BillStatusPartiallyPaid, nil
	default:
		return "", invalidRequest("payment.mode must be one of CASH, ONLINE, SPLIT")
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

func maxDiscountAmount(serviceGrossAmount int64) (int64, error) {
	return checkedPercentFloor(serviceGrossAmount, maxDiscountPercent, 100, "max discount amount")
}

func allocateDiscountByLargestRemainder(lineGrossAmounts []int64, discountAmount int64, serviceGrossAmount int64) ([]int64, error) {
	allocations := make([]int64, len(lineGrossAmounts))
	if discountAmount == 0 || len(lineGrossAmounts) == 0 || serviceGrossAmount == 0 {
		return allocations, nil
	}

	type remainderRow struct {
		index     int
		remainder int64
	}

	remainders := make([]remainderRow, 0, len(lineGrossAmounts))
	allocated := int64(0)

	for i, lineGrossAmount := range lineGrossAmounts {
		numerator, err := checkedMul(discountAmount, lineGrossAmount, "discount allocation")
		if err != nil {
			return nil, err
		}
		baseAllocation := numerator / serviceGrossAmount
		remainder := numerator % serviceGrossAmount

		allocations[i] = baseAllocation
		allocated, err = checkedAdd(allocated, baseAllocation, "discount allocation")
		if err != nil {
			return nil, err
		}
		remainders = append(remainders, remainderRow{
			index:     i,
			remainder: remainder,
		})
	}

	remaining := discountAmount - allocated
	if remaining <= 0 {
		return allocations, nil
	}

	sort.SliceStable(remainders, func(i int, j int) bool {
		return remainders[i].remainder > remainders[j].remainder
	})

	for i := int64(0); i < remaining; i++ {
		target := remainders[i].index
		allocations[target]++
	}

	return allocations, nil
}

func derivePaidAndDueAmounts(totalAmount int64, payment PaymentInput) (int64, int64, error) {
	switch payment.Mode {
	case PaymentModeCash:
		return totalAmount, 0, nil
	case PaymentModeOnline:
		return 0, totalAmount, nil
	case PaymentModeSplit:
		if payment.CashAmount > totalAmount {
			return 0, 0, invalidRequest("payment.cash_amount must be less than total_amount for SPLIT")
		}
		return payment.CashAmount, totalAmount - payment.CashAmount, nil
	default:
		return 0, totalAmount, nil
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
		var err error
		lineNetTotal, err = checkedAdd(lineNetTotal, line.LineNetAmount, "line net amount")
		if err != nil {
			return err
		}
		lineTaxTotal, err := checkedAdd(line.TaxableBaseAmount, line.TaxAmount, "line tax total")
		if err != nil {
			return err
		}
		taxablePlusTaxTotal, err = checkedAdd(taxablePlusTaxTotal, lineTaxTotal, "tax total")
		if err != nil {
			return err
		}
	}

	if lineNetTotal != result.Totals.ServiceNetAmount {
		return invalidRequest("invariant failed: sum of line net amounts must equal service net total")
	}
	if taxablePlusTaxTotal != result.Totals.ServiceNetAmount {
		return invalidRequest("invariant failed: taxable bases plus tax amounts must equal service net total")
	}

	tipTotal := int64(0)
	for _, allocation := range result.TipAllocations {
		var err error
		tipTotal, err = checkedAdd(tipTotal, allocation.TipAmount, "tip amount")
		if err != nil {
			return err
		}
	}
	if tipTotal != result.Totals.TipAmount {
		return invalidRequest("invariant failed: sum of tip allocations must equal tip amount")
	}

	paidPlusDue, err := checkedAdd(result.Totals.AmountPaid, result.Totals.AmountDue, "payment amount")
	if err != nil {
		return err
	}
	if paidPlusDue != result.Totals.TotalAmount {
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

func checkedAdd(a int64, b int64, fieldName string) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, invalidRequest(fieldName + " exceeds supported range")
	}
	return a + b, nil
}

func checkedMul(a int64, b int64, fieldName string) (int64, error) {
	if a < 0 || b < 0 {
		return 0, invalidRequest(fieldName + " cannot be negative")
	}
	if a != 0 && b > math.MaxInt64/a {
		return 0, invalidRequest(fieldName + " exceeds supported range")
	}
	return a * b, nil
}

func checkedPercentFloor(amount int64, numerator int64, divisor int64, fieldName string) (int64, error) {
	if divisor <= 0 {
		return 0, invalidRequest(fieldName + " has invalid divisor")
	}
	product, err := checkedMul(amount, numerator, fieldName)
	if err != nil {
		return 0, err
	}
	return product / divisor, nil
}
