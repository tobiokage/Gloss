package http

import (
	"database/sql"
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	platformconfig "gloss/internal/platform/config"
)

type App struct {
	Config platformconfig.Config
	Logger *slog.Logger
	DB     *sql.DB
}

func NewRouter(app App) nethttp.Handler {
	router := chi.NewRouter()

	router.Get("/health", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		WriteJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})

	return router
}
