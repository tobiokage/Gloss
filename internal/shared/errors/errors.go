package errors

type AppError struct {
	Code    Code           `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return e.Message
}

func New(code Code, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Details: map[string]any{},
	}
}

func NewWithDetails(code Code, message string, details map[string]any) *AppError {
	if details == nil {
		details = map[string]any{}
	}

	return &AppError{
		Code:    code,
		Message: message,
		Details: details,
	}
}
