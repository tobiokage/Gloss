package billing

import (
	"encoding/json"
	nethttp "net/http"

	"gloss/internal/auth"
	platformhttp "gloss/internal/platform/http"
	apperrors "gloss/internal/shared/errors"
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
