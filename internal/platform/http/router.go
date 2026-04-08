package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(
	authLoginHandler nethttp.HandlerFunc,
	authMiddleware func(next nethttp.Handler) nethttp.Handler,
	superAdminOnlyMiddleware func(next nethttp.Handler) nethttp.Handler,
	storeManagerOnlyMiddleware func(next nethttp.Handler) nethttp.Handler,
	storeBootstrapHandler nethttp.HandlerFunc,
	createBillHandler nethttp.HandlerFunc,
	adminCatalogueListHandler nethttp.HandlerFunc,
	adminCatalogueCreateHandler nethttp.HandlerFunc,
	adminCatalogueUpdateHandler nethttp.HandlerFunc,
	adminCatalogueDeactivateHandler nethttp.HandlerFunc,
	adminStaffListHandler nethttp.HandlerFunc,
	adminStaffCreateHandler nethttp.HandlerFunc,
	adminStaffDeactivateHandler nethttp.HandlerFunc,
	adminStaffAssignStoreHandler nethttp.HandlerFunc,
) *chi.Mux {
	router := chi.NewRouter()

	router.Get("/health", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})

	router.Post("/auth/login", authLoginHandler)
	router.With(authMiddleware).Get("/store/bootstrap", storeBootstrapHandler)
	router.With(authMiddleware, storeManagerOnlyMiddleware).Post("/bills", createBillHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Get("/admin/catalogue", adminCatalogueListHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/catalogue", adminCatalogueCreateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Put("/admin/catalogue/{item_id}", adminCatalogueUpdateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/catalogue/{item_id}/deactivate", adminCatalogueDeactivateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Get("/admin/staff", adminStaffListHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/staff", adminStaffCreateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/staff/{id}/deactivate", adminStaffDeactivateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/staff/{id}/stores/{store_id}", adminStaffAssignStoreHandler)

	return router
}
