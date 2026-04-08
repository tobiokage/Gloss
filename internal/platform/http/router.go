package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(
	authLoginHandler nethttp.HandlerFunc,
	authMiddleware func(next nethttp.Handler) nethttp.Handler,
	superAdminOnlyMiddleware func(next nethttp.Handler) nethttp.Handler,
	storeBootstrapHandler nethttp.HandlerFunc,
	adminCatalogueListHandler nethttp.HandlerFunc,
	adminCatalogueCreateHandler nethttp.HandlerFunc,
	adminCatalogueUpdateHandler nethttp.HandlerFunc,
	adminCatalogueDeactivateHandler nethttp.HandlerFunc,
) *chi.Mux {
	router := chi.NewRouter()

	router.Get("/health", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})

	router.Post("/auth/login", authLoginHandler)
	router.With(authMiddleware).Get("/store/bootstrap", storeBootstrapHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Get("/admin/catalogue-items", adminCatalogueListHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/catalogue-items", adminCatalogueCreateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Put("/admin/catalogue-items/{item_id}", adminCatalogueUpdateHandler)
	router.With(authMiddleware, superAdminOnlyMiddleware).Post("/admin/catalogue-items/{item_id}/deactivate", adminCatalogueDeactivateHandler)

	return router
}
