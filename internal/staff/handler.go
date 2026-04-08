package staff

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

func (h *Handler) ListStaff(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	staffList, err := h.service.ListStaff(r.Context(), authCtx.TenantID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	response := make([]StaffResponse, 0, len(staffList))
	for _, member := range staffList {
		response = append(response, mapStaffToResponse(member))
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) CreateStaff(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	var req CreateStaffRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		platformhttp.WriteError(w, apperrors.New(apperrors.CodeInvalidRequest, "Invalid request body"))
		return
	}

	member, err := h.service.CreateStaff(r.Context(), authCtx.TenantID, req)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusCreated, mapStaffToResponse(member))
}

func (h *Handler) DeactivateStaff(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	staffID := chi.URLParam(r, "id")

	member, err := h.service.DeactivateStaff(r.Context(), authCtx.TenantID, staffID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, DeactivateStaffResponse{
		ID:        member.ID,
		Active:    member.Active,
		UpdatedAt: member.UpdatedAt,
	})
}

func (h *Handler) AssignStaffToStore(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	staffID := chi.URLParam(r, "id")
	storeID := chi.URLParam(r, "store_id")

	mapping, err := h.service.AssignStaffToStore(r.Context(), authCtx.TenantID, staffID, storeID)
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, AssignStaffToStoreResponse{
		StaffID:   mapping.StaffID,
		StoreID:   mapping.StoreID,
		Active:    mapping.Active,
		UpdatedAt: mapping.UpdatedAt,
	})
}

func mapStaffToResponse(member Staff) StaffResponse {
	return StaffResponse{
		ID:        member.ID,
		Name:      member.Name,
		Active:    member.Active,
		CreatedAt: member.CreatedAt,
		UpdatedAt: member.UpdatedAt,
	}
}
