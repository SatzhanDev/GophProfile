// Package api собирает HTTP-роутер приложения: middleware и маршруты.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/internal/handlers"
)

// NewRouter собирает chi.Router со всеми middleware и маршрутами сервиса.
// healthChecks нужен здесь только для health-чека; когда появятся хендлеры
// аватарок, сигнатура получит дополнительные зависимости (репозиторий,
// S3-клиент и т.д. — им, в отличие от health-чека, нужны не только Ping).
func NewRouter(cfg *config.Config, healthChecks map[string]handlers.Pinger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", handlers.NewHealthHandler(healthChecks).ServeHTTP)

	r.Route("/api/v1", func(r chi.Router) {
		// В следующих инкрементах здесь появятся:
		// r.Post("/avatars", ...)
		// r.Get("/avatars/{avatarID}", ...)
		// r.Get("/users/{userID}/avatar", ...)
		// r.Delete("/avatars/{avatarID}", ...)
	})

	return r
}
