package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/services"
	"github.com/SatzhanDev/GophProfile/web"
)

// newTestWebHandler собирает WebHandler на тех же фейках, что и
// AvatarHandler в avatar_handler_test.go, но с настоящими embed-шаблонами
// из пакета web — так тест заодно проверяет, что html/template.ParseFS
// реально находит и разбирает upload.html/gallery.html без ошибок.
func newTestWebHandler(t *testing.T) (*WebHandler, *fakeAvatarRepo) {
	t.Helper()

	repo := newFakeAvatarRepo()
	storage := newFakeAvatarStorage()
	svc := services.NewAvatarService(repo, storage, fakeAvatarPublisher{})

	h, err := NewWebHandler(svc, web.TemplatesFS, web.StaticFS)
	require.NoError(t, err)

	return h, repo
}

func TestWebHandler_UploadForm_PrefillsUserID(t *testing.T) {
	h, _ := newTestWebHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/web/upload?user_id=test-user", nil)
	rec := httptest.NewRecorder()

	h.UploadForm(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `value="test-user"`)
}

func TestWebHandler_GalleryPage_ListsAvatars(t *testing.T) {
	h, repo := newTestWebHandler(t)

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{
		ID:               id,
		UserID:           "test-user",
		FileName:         "photo.jpg",
		ProcessingStatus: domain.ProcessingStatusCompleted,
	}

	r := chi.NewRouter()
	r.Get("/web/gallery/{userID}", h.GalleryPage)

	req := httptest.NewRequest(http.MethodGet, "/web/gallery/test-user", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "photo.jpg")
	require.Contains(t, body, id.String())
}

func TestWebHandler_GalleryPage_EmptyState(t *testing.T) {
	h, _ := newTestWebHandler(t)

	r := chi.NewRouter()
	r.Get("/web/gallery/{userID}", h.GalleryPage)

	req := httptest.NewRequest(http.MethodGet, "/web/gallery/nobody", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "пока нет аватарок")
}

// newWebUploadRequest собирает multipart-тело классической формы
// /web/upload — в отличие от JSON API, user_id здесь обычное поле формы,
// а не заголовок.
func newWebUploadRequest(t *testing.T, userID, fileName string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if userID != "" {
		require.NoError(t, w.WriteField("user_id", userID))
	}
	if fileName != "" {
		part, err := w.CreateFormFile("file", fileName)
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/web/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestWebHandler_UploadSubmit_MissingUserID(t *testing.T) {
	h, _ := newTestWebHandler(t)

	req := newWebUploadRequest(t, "", "photo.jpg", generateTestJPEG(t))
	rec := httptest.NewRecorder()

	h.UploadSubmit(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWebHandler_UploadSubmit_Success_RedirectsToGallery(t *testing.T) {
	h, repo := newTestWebHandler(t)

	req := newWebUploadRequest(t, "test-user", "photo.jpg", generateTestJPEG(t))
	rec := httptest.NewRecorder()

	h.UploadSubmit(rec, req)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	require.Equal(t, "/web/gallery/test-user", rec.Header().Get("Location"))
	require.Len(t, repo.avatars, 1, "upload must have actually created an avatar")
}
