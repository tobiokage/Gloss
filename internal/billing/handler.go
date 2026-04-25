package billing

import (
	"encoding/json"
	nethttp "net/http"

	"gloss/internal/auth"
	platformhttp "gloss/internal/platform/http"
	apperrors "gloss/internal/shared/errors"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateBill(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	var req CreateBillRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	response, err := h.service.CreateBill(r.Context(), authCtx, req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusCreated, response)
}

func (h *Handler) GetBill(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	response, err := h.service.GetBill(r.Context(), authCtx, chi.URLParam(r, "bill_id"))
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) CancelBill(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	var req CancelBillRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	response, err := h.service.CancelBill(r.Context(), authCtx, chi.URLParam(r, "bill_id"), req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) RetryOnlinePayment(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	var req RetryOnlinePaymentRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	response, err := h.service.RetryOnlinePayment(r.Context(), authCtx, chi.URLParam(r, "bill_id"), req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) CancelPaymentAttempt(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	response, err := h.service.CancelPaymentAttempt(
		r.Context(),
		authCtx,
		chi.URLParam(r, "bill_id"),
		chi.URLParam(r, "payment_id"),
	)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}
