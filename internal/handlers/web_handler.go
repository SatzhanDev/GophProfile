package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/SatzhanDev/GophProfile/internal/services"
)

// WebHandler обслуживает серверный веб-интерфейс (/web/...) — форму
// загрузки и галерею. В отличие от AvatarHandler, здесь ответы — не JSON,
// а HTML-страницы и редиректы, но бизнес-логику (AvatarService) переиспользует
// ту же самую: веб-интерфейс не более чем ещё один клиент REST API.
type WebHandler struct {
	service       *services.AvatarService
	uploadTmpl    *template.Template
	galleryTmpl   *template.Template
	staticHandler http.Handler
}

// NewWebHandler разбирает шаблоны один раз при старте (а не на каждый
// запрос — html/template.Parse не бесплатный) и готовит файловый сервер
// для статики.
func NewWebHandler(service *services.AvatarService, templatesFS, staticFS fs.FS) (*WebHandler, error) {
	uploadTmpl, err := template.ParseFS(templatesFS, "templates/upload.html")
	if err != nil {
		return nil, fmt.Errorf("parse upload template: %w", err)
	}

	galleryTmpl, err := template.ParseFS(templatesFS, "templates/gallery.html")
	if err != nil {
		return nil, fmt.Errorf("parse gallery template: %w", err)
	}

	// fs.Sub убирает префикс "static/" из путей: без этого пришлось бы
	// запрашивать /web/static/static/style.css, что не соответствовало бы
	// адресам, прописанным в самих HTML-шаблонах.
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("prepare static fs: %w", err)
	}

	return &WebHandler{
		service:       service,
		uploadTmpl:    uploadTmpl,
		galleryTmpl:   galleryTmpl,
		staticHandler: http.FileServer(http.FS(sub)),
	}, nil
}

type uploadPageData struct {
	UserID string
}

// UploadForm — GET /web/upload. user_id можно передать в query-параметре
// (так и делает ссылка "Загрузить ещё одну" из галереи), чтобы не вводить
// его заново вручную.
func (h *WebHandler) UploadForm(w http.ResponseWriter, r *http.Request) {
	data := uploadPageData{UserID: r.URL.Query().Get("user_id")}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.uploadTmpl.Execute(w, data); err != nil {
		slog.Error("failed to render upload template", "error", err)
	}
}

// UploadSubmit — POST /web/upload. Классическая отправка формы без JS:
// принимает multipart-форму с полями user_id и file напрямую (у обычной
// HTML-формы нет способа передать заголовок X-User-ID, поэтому идентификатор
// пользователя здесь — обычное поле формы, а не заголовок, как в JSON API).
// После успешной загрузки — редирект на галерею (паттерн Post-Redirect-Get,
// чтобы обновление страницы не отправляло файл повторно).
func (h *WebHandler) UploadSubmit(w http.ResponseWriter, r *http.Request) {
	userID := r.FormValue("user_id")
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}
	if !isValidUserID(userID) {
		http.Error(w, "user_id has invalid format", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, services.MaxAvatarSize+512<<10)
	if err := r.ParseMultipartForm(services.MaxAvatarSize); err != nil {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, detectedType, err := extractUploadedFile(r)
	if err != nil {
		http.Error(w, "file field is required", http.StatusBadRequest)
		return
	}
	defer func() { _ = file.Close() }()

	if _, err := h.service.Upload(r.Context(), userID, header.Filename, detectedType, header.Size, file); err != nil {
		slog.Error("web upload failed", "error", err)
		http.Error(w, "upload failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/web/gallery/"+userID, http.StatusSeeOther)
}

type galleryPageData struct {
	UserID  string
	Avatars []galleryAvatar
}

type galleryAvatar struct {
	ID               string
	FileName         string
	ProcessingStatus string
}

// GalleryPage — GET /web/gallery/{userID}.
func (h *WebHandler) GalleryPage(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	avatars, err := h.service.ListForUser(r.Context(), userID)
	if err != nil {
		slog.Error("failed to list avatars for gallery", "error", err)
		http.Error(w, "failed to load gallery", http.StatusInternalServerError)
		return
	}

	data := galleryPageData{UserID: userID, Avatars: make([]galleryAvatar, 0, len(avatars))}
	for _, a := range avatars {
		data.Avatars = append(data.Avatars, galleryAvatar{
			ID:               a.ID.String(),
			FileName:         a.FileName,
			ProcessingStatus: string(a.ProcessingStatus),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.galleryTmpl.Execute(w, data); err != nil {
		slog.Error("failed to render gallery template", "error", err)
	}
}

// ServeStatic отдаёт CSS/JS из web/static по адресам /web/static/*.
func (h *WebHandler) ServeStatic(w http.ResponseWriter, r *http.Request) {
	http.StripPrefix("/web/static/", h.staticHandler).ServeHTTP(w, r)
}
