package money

// Paise is the canonical unit for all money values in the system.
type Paise int64

func IsNonNegative(value Paise) bool {
	return value >= 0
}
