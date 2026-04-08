package catalogue

import (
	"regexp"
	"strings"

	apperrors "gloss/internal/shared/errors"
)

var itemIDUUIDPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

type ValidatedCatalogueItemInput struct {
	Name      string
	Category  string
	ListPrice int64
}

func ValidateCatalogueItemInput(name string, category string, listPrice int64) (ValidatedCatalogueItemInput, error) {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return ValidatedCatalogueItemInput{}, apperrors.New(apperrors.CodeInvalidRequest, "name is required")
	}

	normalizedCategory := strings.TrimSpace(category)
	if normalizedCategory == "" {
		return ValidatedCatalogueItemInput{}, apperrors.New(apperrors.CodeInvalidRequest, "category is required")
	}

	if listPrice <= 0 {
		return ValidatedCatalogueItemInput{}, apperrors.New(apperrors.CodeInvalidRequest, "list_price must be greater than 0")
	}

	return ValidatedCatalogueItemInput{
		Name:      normalizedName,
		Category:  normalizedCategory,
		ListPrice: listPrice,
	}, nil
}

func ValidateItemID(itemID string) (string, error) {
	normalizedItemID := strings.TrimSpace(itemID)
	if normalizedItemID == "" {
		return "", apperrors.New(apperrors.CodeInvalidRequest, "item_id is required")
	}
	if !itemIDUUIDPattern.MatchString(normalizedItemID) {
		return "", apperrors.New(apperrors.CodeInvalidRequest, "item_id must be a valid UUID")
	}

	return normalizedItemID, nil
}
