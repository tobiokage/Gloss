package bootstrap

import (
	nethttp "net/http"

	"gloss/internal/auth"
	platformhttp "gloss/internal/platform/http"
	"gloss/internal/shared/enums"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetStoreBootstrap(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	if err := auth.RequireRole(authCtx, enums.RoleStoreManager); err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	response, err := h.service.GetStoreBootstrap(r.Context(), authCtx.TenantID, authCtx.StoreID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}
