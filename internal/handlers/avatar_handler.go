package handlers

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/services"
	"github.com/SatzhanDev/GophProfile/pkg/placeholder"
)

// placeholderSize — размер картинки-заглушки в пикселях. Заглушка нужна,
// когда у пользователя ещё нет своей аватарки — это ключевая идея
// GophProfile: сторонняя платформа всегда получает картинку, а не 404.
const placeholderSize = 200

// AvatarHandler обрабатывает все HTTP-запросы, связанные с аватарками.
type AvatarHandler struct {
	service     *services.AvatarService
	placeholder []byte
}

// NewAvatarHandler создаёт AvatarHandler и один раз генерирует заглушку —
// не на каждый запрос, а один раз при старте приложения.
func NewAvatarHandler(service *services.AvatarService) *AvatarHandler {
	ph, err := placeholder.Generate(placeholderSize)
	if err != nil {
		// Практически недостижимо (кодирование валидного image.RGBA в PNG
		// не должно падать), но если вдруг — не роняем весь сервис из-за
		// этого, а просто останемся без заглушки и залогируем проблему.
		slog.Error("failed to generate placeholder avatar", "error", err)
	}

	return &AvatarHandler{service: service, placeholder: ph}
}

// --- DTO для ответов, повторяют формат из ТЗ ---

type uploadResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type thumbnailResponse struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

type metadataResponse struct {
	ID         string              `json:"id"`
	UserID     string              `json:"user_id"`
	FileName   string              `json:"file_name"`
	MimeType   string              `json:"mime_type"`
	Size       int64               `json:"size"`
	Thumbnails []thumbnailResponse `json:"thumbnails"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
}

// toMetadataResponse превращает доменную модель в DTO для ответа.
//
// Поле "dimensions" (ширина/высота оригинала) из примера в ТЗ здесь
// намеренно отсутствует: у нас в БД нет для этого столбцов, а определять
// размеры на лету при каждом запросе — лишняя работа. Это естественно
// добавить в инкременте с воркером: он и так декодирует картинку, чтобы
// сделать миниатюры, — тогда же можно один раз посчитать и сохранить размеры.
func toMetadataResponse(a *domain.Avatar) metadataResponse {
	sizes := make([]string, 0, len(a.ThumbnailS3Keys))
	for size := range a.ThumbnailS3Keys {
		sizes = append(sizes, size)
	}
	sort.Strings(sizes) // map не гарантирует порядок — сортируем для стабильного JSON

	thumbnails := make([]thumbnailResponse, 0, len(sizes))
	for _, size := range sizes {
		thumbnails = append(thumbnails, thumbnailResponse{
			Size: size,
			URL:  fmt.Sprintf("/api/v1/avatars/%s?size=%s", a.ID, size),
		})
	}

	return metadataResponse{
		ID:         a.ID.String(),
		UserID:     a.UserID,
		FileName:   a.FileName,
		MimeType:   a.MimeType,
		Size:       a.SizeBytes,
		Thumbnails: thumbnails,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

// UploadAvatar — POST /api/v1/avatars.
func (h *AvatarHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	// Жёстко обрезаем тело запроса чуть выше лимита файла (запас — под
	// служебные байты multipart-разметки: границы между частями и т.п.).
	// Если клиент пришлёт больше — ParseMultipartForm ниже вернёт ошибку,
	// не дав серверу вычитать и забуферизовать весь избыточный поток.
	r.Body = http.MaxBytesReader(w, r.Body, services.MaxAvatarSize+512<<10)

	if err := r.ParseMultipartForm(services.MaxAvatarSize); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error":    "File too large",
			"max_size": services.MaxAvatarSize,
		})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	// Определяем реальный тип файла по первым байтам содержимого (magic
	// bytes), а не по Content-Type, который прислал клиент, — этому
	// заголовку доверять нельзя, его можно подделать никак не трогая сам файл.
	sniff := make([]byte, 512)
	n, err := file.Read(sniff)
	if err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	detectedType := http.DetectContentType(sniff[:n])

	// multipart.File — это ещё и io.Seeker, поэтому просто перематываем
	// файл в начало вместо того, чтобы городить составной reader из уже
	// прочитанных байт и остатка потока.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	avatar, err := h.service.Upload(r.Context(), userID, header.Filename, detectedType, header.Size, file)
	switch {
	case errors.Is(err, services.ErrUnsupportedType):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "Invalid file format",
			"details": "Supported formats: jpeg, png, webp",
		})
		return
	case errors.Is(err, services.ErrFileTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error":    "File too large",
			"max_size": services.MaxAvatarSize,
		})
		return
	case err != nil:
		slog.Error("failed to upload avatar", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to upload avatar")
		return
	}

	writeJSON(w, http.StatusCreated, uploadResponse{
		ID:        avatar.ID.String(),
		UserID:    avatar.UserID,
		URL:       fmt.Sprintf("/api/v1/avatars/%s", avatar.ID),
		Status:    string(avatar.ProcessingStatus),
		CreatedAt: avatar.CreatedAt,
	})
}

// GetAvatar — GET /api/v1/avatars/{avatarID}. Поддерживает ?size=100x100
// (и т.п.) для отдачи готовой миниатюры вместо оригинала; ?format= из ТЗ
// (конвертация на лету в другой формат) не реализован — это отдельная
// недешёвая функциональность за рамками MVP.
func (h *AvatarHandler) GetAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatarID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}

	size := r.URL.Query().Get("size")

	avatar, body, err := h.service.GetFile(r.Context(), id, size)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrAvatarNotFound):
			writeError(w, http.StatusNotFound, "Avatar not found")
		case errors.Is(err, services.ErrThumbnailNotAvailable):
			writeError(w, http.StatusNotFound, "Requested size is not available yet")
		default:
			slog.Error("failed to fetch avatar", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to fetch avatar")
		}
		return
	}
	defer body.Close()

	writeImage(w, avatar, body)
}

// GetUserAvatar — GET /api/v1/users/{userID}/avatar. Это тот самый эндпоинт
// из идеи продукта: если у пользователя есть аватарка — отдаём её,
// если нет (ErrAvatarNotFound) — отдаём заглушку, а не 404.
func (h *AvatarHandler) GetUserAvatar(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	avatar, body, err := h.service.GetLatestForUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			h.writePlaceholder(w)
			return
		}
		slog.Error("failed to fetch user avatar", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch avatar")
		return
	}
	defer body.Close()

	writeImage(w, avatar, body)
}

func (h *AvatarHandler) writePlaceholder(w http.ResponseWriter) {
	if h.placeholder == nil {
		writeError(w, http.StatusInternalServerError, "placeholder unavailable")
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.placeholder)
}

// writeImage — общая часть отдачи бинарных данных картинки для
// GetAvatar и GetUserAvatar: одинаковые заголовки, одинаковое копирование.
func writeImage(w http.ResponseWriter, avatar *domain.Avatar, body io.Reader) {
	w.Header().Set("Content-Type", avatar.MimeType)
	w.Header().Set("Cache-Control", "max-age=86400")
	w.Header().Set("ETag", fmt.Sprintf(`"%s-%d"`, avatar.ID, avatar.UpdatedAt.Unix()))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// GetMetadata — GET /api/v1/avatars/{avatarID}/metadata.
func (h *AvatarHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatarID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}

	avatar, err := h.service.GetMetadata(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			writeError(w, http.StatusNotFound, "Avatar not found")
			return
		}
		slog.Error("failed to fetch avatar metadata", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to fetch avatar metadata")
		return
	}

	writeJSON(w, http.StatusOK, toMetadataResponse(avatar))
}

// ListUserAvatars — GET /api/v1/users/{userID}/avatars.
func (h *AvatarHandler) ListUserAvatars(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	avatars, err := h.service.ListForUser(r.Context(), userID)
	if err != nil {
		slog.Error("failed to list avatars", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list avatars")
		return
	}

	responses := make([]metadataResponse, 0, len(avatars))
	for i := range avatars {
		responses = append(responses, toMetadataResponse(&avatars[i]))
	}

	writeJSON(w, http.StatusOK, responses)
}

// DeleteAvatar — DELETE /api/v1/avatars/{avatarID}.
func (h *AvatarHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatarID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	err = h.service.Delete(r.Context(), id, userID)
	if !handleDeleteError(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteUserAvatar — DELETE /api/v1/users/{userID}/avatar.
func (h *AvatarHandler) DeleteUserAvatar(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")

	requesterID := r.Header.Get("X-User-ID")
	if requesterID == "" {
		writeError(w, http.StatusBadRequest, "X-User-ID header is required")
		return
	}

	err := h.service.DeleteLatestForUser(r.Context(), userID, requesterID)
	if !handleDeleteError(w, err) {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteError разбирает ошибку, общую для DeleteAvatar и
// DeleteUserAvatar, и сама пишет ответ клиенту в случае ошибки.
// Возвращает true, если ошибки не было и можно продолжать (писать 204).
func handleDeleteError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, repository.ErrAvatarNotFound):
		writeError(w, http.StatusNotFound, "Avatar not found")
	case errors.Is(err, services.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":   "Forbidden",
			"details": "You can only delete your own avatars",
		})
	default:
		slog.Error("failed to delete avatar", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete avatar")
	}
	return false
}
