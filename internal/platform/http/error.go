package http

import (
	stderrors "errors"
	nethttp "net/http"

	apperrors "gloss/internal/shared/errors"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func WriteError(w nethttp.ResponseWriter, err error) {
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		appErr = apperrors.New(apperrors.CodeInternalError, "Internal server error")
	}

	details := appErr.Details
	if details == nil {
		details = map[string]any{}
	}

	WriteJSON(w, statusFromCode(appErr.Code), errorEnvelope{
		Error: errorBody{
			Code:    string(appErr.Code),
			Message: appErr.Message,
			Details: details,
		},
	})
}

func statusFromCode(code apperrors.Code) int {
	switch code {
	case apperrors.CodeInvalidConfig:
		return nethttp.StatusInternalServerError
	case apperrors.CodeInvalidRequest:
		return nethttp.StatusBadRequest
	case apperrors.CodeDBUnavailable:
		return nethttp.StatusServiceUnavailable
	case apperrors.CodeNotFound:
		return nethttp.StatusNotFound
	case apperrors.CodeUnauthorized:
		return nethttp.StatusUnauthorized
	default:
		return nethttp.StatusInternalServerError
	}
}
