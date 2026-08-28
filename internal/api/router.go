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

// Deps — все зависимости, нужные роутеру, чтобы собрать хендлеры.
// Раньше эти параметры передавались по одному, но с ростом числа хендлеров
// длинный список позиционных аргументов стал бы неудобным и хрупким
// (легко перепутать порядок) — поэтому дальше растим именно эту структуру.
type Deps struct {
	Config        *config.Config
	HealthChecks  map[string]handlers.Pinger
	AvatarHandler *handlers.AvatarHandler
	WebHandler    *handlers.WebHandler
}

// NewRouter собирает chi.Router со всеми middleware и маршрутами сервиса.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", handlers.NewHealthHandler(deps.HealthChecks).ServeHTTP)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web/upload", http.StatusFound)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/avatars", deps.AvatarHandler.UploadAvatar)
		r.Get("/avatars/{avatarID}", deps.AvatarHandler.GetAvatar)
		r.Get("/avatars/{avatarID}/metadata", deps.AvatarHandler.GetMetadata)
		r.Delete("/avatars/{avatarID}", deps.AvatarHandler.DeleteAvatar)

		r.Get("/users/{userID}/avatar", deps.AvatarHandler.GetUserAvatar)
		r.Delete("/users/{userID}/avatar", deps.AvatarHandler.DeleteUserAvatar)
		r.Get("/users/{userID}/avatars", deps.AvatarHandler.ListUserAvatars)
	})

	r.Route("/web", func(r chi.Router) {
		r.Get("/upload", deps.WebHandler.UploadForm)
		r.Post("/upload", deps.WebHandler.UploadSubmit)
		r.Get("/gallery/{userID}", deps.WebHandler.GalleryPage)
		r.Handle("/static/*", http.HandlerFunc(deps.WebHandler.ServeStatic))
	})

	return r
}
