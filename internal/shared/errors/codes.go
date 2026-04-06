package errors

type Code string

const (
	CodeInternalError  Code = "INTERNAL_ERROR"
	CodeInvalidConfig  Code = "INVALID_CONFIG"
	CodeInvalidRequest Code = "INVALID_REQUEST"
	CodeDBUnavailable  Code = "DB_UNAVAILABLE"
	CodeNotFound       Code = "NOT_FOUND"
)
