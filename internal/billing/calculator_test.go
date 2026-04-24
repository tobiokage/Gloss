package billing

import (
	"math"
	"strings"
	"testing"

	"gloss/internal/shared/enums"
)

func TestCalculateBillCashOnlineAndSplit(t *testing.T) {
	tests := []struct {
		name       string
		payment    PaymentInput
		wantStatus enums.BillStatus
		wantPaid   int64
		wantDue    int64
	}{
		{
			name:       "cash",
			payment:    PaymentInput{Mode: PaymentModeCash},
			wantStatus: enums.BillStatusPaid,
			wantPaid:   20_000,
			wantDue:    0,
		},
		{
			name:       "online",
			payment:    PaymentInput{Mode: PaymentModeOnline},
			wantStatus: enums.BillStatusPaymentPending,
			wantPaid:   0,
			wantDue:    20_000,
		},
		{
			name:       "split",
			payment:    PaymentInput{Mode: PaymentModeSplit, CashAmount: 7_500},
			wantStatus: enums.BillStatusPartiallyPaid,
			wantPaid:   7_500,
			wantDue:    12_500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalculateBill(CalculatorInput{
				Lines: []AuthoritativeBillLineInput{
					calculatorTestLine("11111111-1111-4111-8111-111111111111", 10_000, 2),
				},
				Payment: tt.payment,
			})
			if err != nil {
				t.Fatalf("CalculateBill returned error: %v", err)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("expected status %q, got %q", tt.wantStatus, result.Status)
			}
			if result.Totals.AmountPaid != tt.wantPaid || result.Totals.AmountDue != tt.wantDue {
				t.Fatalf("unexpected totals: paid=%d due=%d", result.Totals.AmountPaid, result.Totals.AmountDue)
			}
		})
	}
}

func TestCalculateBillInvariants(t *testing.T) {
	result, err := CalculateBill(CalculatorInput{
		Lines: []AuthoritativeBillLineInput{
			calculatorTestLine("11111111-1111-4111-8111-111111111111", 10_501, 1),
			calculatorTestLine("22222222-2222-4222-8222-222222222222", 20_099, 1),
		},
		DiscountAmount: 3_001,
		TipAmount:      500,
		TipAllocations: []TipAllocation{
			{StaffID: "33333333-3333-4333-8333-333333333333", TipAmount: 300},
			{StaffID: "44444444-4444-4444-8444-444444444444", TipAmount: 200},
		},
		Payment: PaymentInput{Mode: PaymentModeCash},
	})
	if err != nil {
		t.Fatalf("CalculateBill returned error: %v", err)
	}

	var lineDiscounts, lineNet, taxablePlusTax, tips, commissionTotal int64
	for _, line := range result.Lines {
		lineDiscounts += line.LineDiscountAmount
		lineNet += line.LineNetAmount
		taxablePlusTax += line.TaxableBaseAmount + line.TaxAmount
		commissionTotal += line.CommissionAmount
		if line.CommissionAmount != line.LineNetAmount*commissionPercent/100 {
			t.Fatalf("commission mismatch for line %#v", line)
		}
	}
	for _, allocation := range result.TipAllocations {
		tips += allocation.TipAmount
	}

	if lineDiscounts != result.Totals.DiscountAmount {
		t.Fatalf("discount invariant failed: lines=%d bill=%d", lineDiscounts, result.Totals.DiscountAmount)
	}
	if lineNet != result.Totals.ServiceNetAmount {
		t.Fatalf("net invariant failed: lines=%d bill=%d", lineNet, result.Totals.ServiceNetAmount)
	}
	if taxablePlusTax != result.Totals.ServiceNetAmount {
		t.Fatalf("tax invariant failed: tax+base=%d net=%d", taxablePlusTax, result.Totals.ServiceNetAmount)
	}
	if tips != result.Totals.TipAmount {
		t.Fatalf("tip invariant failed: tips=%d total=%d", tips, result.Totals.TipAmount)
	}
	if commissionTotal == 0 {
		t.Fatal("expected commission to be calculated")
	}
}

func TestCalculateBillRejectsOverflowAndUnsafeBounds(t *testing.T) {
	tests := []struct {
		name  string
		input CalculatorInput
		want  string
	}{
		{
			name: "unit price quantity overflow",
			input: CalculatorInput{
				Lines: []AuthoritativeBillLineInput{
					calculatorTestLine("11111111-1111-4111-8111-111111111111", math.MaxInt64, 2),
				},
				Payment: PaymentInput{Mode: PaymentModeCash},
			},
			want: "unit_price exceeds maximum",
		},
		{
			name: "excessive quantity",
			input: CalculatorInput{
				Lines: []AuthoritativeBillLineInput{
					calculatorTestLine("11111111-1111-4111-8111-111111111111", 100, MaxBillItemQuantity+1),
				},
				Payment: PaymentInput{Mode: PaymentModeCash},
			},
			want: "quantity exceeds maximum",
		},
		{
			name: "excessive tip",
			input: CalculatorInput{
				Lines: []AuthoritativeBillLineInput{
					calculatorTestLine("11111111-1111-4111-8111-111111111111", 10_000, 1),
				},
				TipAmount: MaxBillTipAmountPaise + 1,
				Payment:   PaymentInput{Mode: PaymentModeCash},
			},
			want: "tip_amount exceeds maximum",
		},
		{
			name: "excessive discount",
			input: CalculatorInput{
				Lines: []AuthoritativeBillLineInput{
					calculatorTestLine("11111111-1111-4111-8111-111111111111", 10_000, 1),
				},
				DiscountAmount: 3_001,
				Payment:        PaymentInput{Mode: PaymentModeCash},
			},
			want: "discount_amount exceeds 30% cap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateBill(tt.input)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func calculatorTestLine(id string, price int64, quantity int64) AuthoritativeBillLineInput {
	return AuthoritativeBillLineInput{
		CatalogueItemID: id,
		ServiceName:     "Service",
		AssignedStaffID: "55555555-5555-4555-8555-555555555555",
		UnitPrice:       price,
		Quantity:        quantity,
	}
}
