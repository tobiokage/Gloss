package auth

import (
	"encoding/json"
	nethttp "net/http"

	platformhttp "gloss/internal/platform/http"
	apperrors "gloss/internal/shared/errors"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Login(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req LoginRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	result, err := h.service.Login(r.Context(), req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, LoginResponse{
		Token: result.Token,
	})
}
