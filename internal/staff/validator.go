package staff

import (
	"regexp"
	"strings"

	apperrors "gloss/internal/shared/errors"
)

var uuidPattern = regexp.MustCompile(
	`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
)

func ValidateStaffName(name string) (string, error) {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return "", apperrors.New(apperrors.CodeInvalidRequest, "name is required")
	}

	return normalizedName, nil
}

func ValidateStaffID(staffID string) (string, error) {
	return validateUUIDValue("id", staffID)
}

func ValidateStoreID(storeID string) (string, error) {
	return validateUUIDValue("store_id", storeID)
}

func validateUUIDValue(fieldName string, rawValue string) (string, error) {
	normalizedValue := strings.TrimSpace(rawValue)
	if normalizedValue == "" {
		return "", apperrors.New(apperrors.CodeInvalidRequest, fieldName+" is required")
	}
	if !uuidPattern.MatchString(normalizedValue) {
		return "", apperrors.New(apperrors.CodeInvalidRequest, fieldName+" must be a valid UUID")
	}

	return normalizedValue, nil
}
