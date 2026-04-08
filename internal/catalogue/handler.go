package catalogue

import (
	"encoding/json"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

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

func (h *Handler) ListCatalogueItems(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	items, err := h.service.ListCatalogueItems(r.Context(), authCtx.TenantID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	response := make([]CatalogueItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, mapCatalogueItemToResponse(item))
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) CreateCatalogueItem(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	var req UpsertCatalogueItemRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	item, err := h.service.CreateCatalogueItem(r.Context(), authCtx.TenantID, req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusCreated, mapCatalogueItemToResponse(item))
}

func (h *Handler) UpdateCatalogueItem(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	itemID := chi.URLParam(r, "item_id")

	var req UpsertCatalogueItemRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	item, err := h.service.UpdateCatalogueItem(r.Context(), authCtx.TenantID, itemID, req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, mapCatalogueItemToResponse(item))
}

func (h *Handler) DeactivateCatalogueItem(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	itemID := chi.URLParam(r, "item_id")

	item, err := h.service.DeactivateCatalogueItem(r.Context(), authCtx.TenantID, itemID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, DeactivateCatalogueItemResponse{
		ID:        item.ID,
		Active:    item.Active,
		UpdatedAt: item.UpdatedAt,
	})
}

func mapCatalogueItemToResponse(item CatalogueItem) CatalogueItemResponse {
	return CatalogueItemResponse{
		ID:        item.ID,
		Name:      item.Name,
		Category:  item.Category,
		ListPrice: item.ListPrice,
		Active:    item.Active,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}
