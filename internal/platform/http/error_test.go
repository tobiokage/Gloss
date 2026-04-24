package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"testing"

	apperrors "gloss/internal/shared/errors"
)

func TestWriteErrorPreservesValidationDetails(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, apperrors.NewWithDetails(
		apperrors.CodeInvalidRequest,
		"Invalid request",
		map[string]any{"field": "quantity"},
	))

	if recorder.Code != nethttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	var body errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Error.Code != string(apperrors.CodeInvalidRequest) || body.Error.Message != "Invalid request" {
		t.Fatalf("unexpected error shape: %#v", body.Error)
	}
	if body.Error.Details["field"] != "quantity" {
		t.Fatalf("expected validation details to be preserved, got %#v", body.Error.Details)
	}
}

func TestWriteErrorSanitizesInternalDetails(t *testing.T) {
	recorder := httptest.NewRecorder()

	WriteError(recorder, apperrors.NewWithDetails(
		apperrors.CodeInternalError,
		"Failed to update payment",
		map[string]any{"reason": "pq: relation payments does not exist"},
	))

	if recorder.Code != nethttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}

	var body errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.Error.Code != string(apperrors.CodeInternalError) || body.Error.Message != "Failed to update payment" {
		t.Fatalf("unexpected error shape: %#v", body.Error)
	}
	if len(body.Error.Details) != 0 {
		t.Fatalf("expected internal details to be sanitized, got %#v", body.Error.Details)
	}
}
