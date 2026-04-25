package analytics

import (
	nethttp "net/http"

	"gloss/internal/auth"
	platformhttp "gloss/internal/platform/http"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListAdminBills(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	queryValues := r.URL.Query()
	response, err := h.service.ListAdminBills(r.Context(), authCtx, AdminBillsQuery{
		StoreID:  queryValues.Get("store_id"),
		DateFrom: queryValues.Get("date_from"),
		DateTo:   queryValues.Get("date_to"),
		Status:   queryValues.Get("status"),
		Limit:    queryValues.Get("limit"),
		Offset:   queryValues.Get("offset"),
	})
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) GetAdminAnalyticsSummary(w nethttp.ResponseWriter, r *nethttp.Request) {
	authCtx, err := auth.AuthContextFromContext(r.Context())
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	queryValues := r.URL.Query()
	response, err := h.service.GetAdminAnalyticsSummary(r.Context(), authCtx, AnalyticsSummaryQuery{
		StoreID:  queryValues.Get("store_id"),
		DateFrom: queryValues.Get("date_from"),
		DateTo:   queryValues.Get("date_to"),
		Status:   queryValues.Get("status"),
	})
	if err != nil {
		platformhttp.WriteError(w, err)
		return
	}

	platformhttp.WriteJSON(w, nethttp.StatusOK, response)
}
