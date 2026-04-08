package billing

import (
	"strings"
	"testing"
)

func TestValidateCreateBillRequestRejectsMalformedUUIDs(t *testing.T) {
	testCases := []struct {
		name          string
		mutate        func(*CreateBillRequest)
		expectedField string
	}{
		{
			name: "catalogue item id",
			mutate: func(req *CreateBillRequest) {
				req.Items[0].CatalogueItemID = "not-a-uuid"
			},
			expectedField: "items[0].catalogue_item_id",
		},
		{
			name: "assigned staff id",
			mutate: func(req *CreateBillRequest) {
				req.Items[0].AssignedStaffID = "not-a-uuid"
			},
			expectedField: "items[0].assigned_staff_id",
		},
		{
			name: "tip allocation staff id",
			mutate: func(req *CreateBillRequest) {
				req.TipAmount = int64PtrForTest(100)
				req.TipAllocations = []CreateBillTipAllocationDTO{
					{StaffID: "not-a-uuid", TipAmount: 100},
				}
			},
			expectedField: "tip_allocations[0].staff_id",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := validCreateBillRequestForValidation()
			tc.mutate(&req)

			_, err := ValidateCreateBillRequest(req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.expectedField) || !strings.Contains(err.Error(), "valid UUID") {
				t.Fatalf("expected UUID validation error for %s, got %v", tc.expectedField, err)
			}
		})
	}
}

func validCreateBillRequestForValidation() CreateBillRequest {
	return CreateBillRequest{
		ClientBillRef: "tablet-1",
		Items: []CreateBillItemRequest{
			{
				CatalogueItemID: "11111111-1111-1111-1111-111111111111",
				Quantity:        1,
				AssignedStaffID: "22222222-2222-2222-2222-222222222222",
			},
		},
		Payment: CreateBillPaymentRequest{
			Mode: string(PaymentModeCash),
		},
		IdempotencyKey: "idem-validation",
	}
}

func int64PtrForTest(value int64) *int64 {
	return &value
}
