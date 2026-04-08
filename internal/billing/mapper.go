package billing

import (
	"strings"
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
