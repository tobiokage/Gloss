package catalogue

import (
	"strings"
	"testing"
)

func TestValidateCatalogueItemInputAllowsZeroAndRejectsExcessivePrice(t *testing.T) {
	if _, err := ValidateCatalogueItemInput("Consultation", "Hair", 0); err != nil {
		t.Fatalf("expected zero list_price to be accepted, got %v", err)
	}

	_, err := ValidateCatalogueItemInput("Premium Service", "Hair", MaxCatalogueListPricePaise+1)
	if err == nil {
		t.Fatal("expected excessive list_price to be rejected")
	}
	if !strings.Contains(err.Error(), "list_price exceeds maximum") {
		t.Fatalf("expected max price error, got %v", err)
	}
}
