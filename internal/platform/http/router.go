package http

import (
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(
	authLoginHandler nethttp.HandlerFunc,
	authMiddleware func(next nethttp.Handler) nethttp.Handler,
	storeBootstrapHandler nethttp.HandlerFunc,
) *chi.Mux {
	router := chi.NewRouter()

	router.Get("/health", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})

	router.Post("/auth/login", authLoginHandler)
	router.With(authMiddleware).Get("/store/bootstrap", storeBootstrapHandler)

	return router
}
