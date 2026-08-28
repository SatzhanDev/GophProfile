package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/services"
)

// Фейки те же по духу, что и в internal/services/avatar_service_test.go —
// файлы _test.go не экспортируются между пакетами, поэтому набор из
// нескольких строк проще продублировать, чем городить общий тестовый пакет
// ради трёх маленьких структур.

type fakeAvatarRepo struct {
	avatars map[uuid.UUID]*domain.Avatar
}

func newFakeAvatarRepo() *fakeAvatarRepo {
	return &fakeAvatarRepo{avatars: make(map[uuid.UUID]*domain.Avatar)}
}

func (r *fakeAvatarRepo) Create(_ context.Context, a *domain.Avatar) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	cp := *a
	r.avatars[a.ID] = &cp
	return nil
}

func (r *fakeAvatarRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Avatar, error) {
	a, ok := r.avatars[id]
	if !ok || a.DeletedAt != nil {
		return nil, repository.ErrAvatarNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeAvatarRepo) ListByUserID(_ context.Context, userID string) ([]domain.Avatar, error) {
	var result []domain.Avatar
	for _, a := range r.avatars {
		if a.UserID == userID && a.DeletedAt == nil {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (r *fakeAvatarRepo) GetLatestByUserID(_ context.Context, userID string) (*domain.Avatar, error) {
	var latest *domain.Avatar
	for _, a := range r.avatars {
		if a.UserID == userID && a.DeletedAt == nil {
			latest = a
		}
	}
	if latest == nil {
		return nil, repository.ErrAvatarNotFound
	}
	cp := *latest
	return &cp, nil
}

func (r *fakeAvatarRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	deletedAt := time.Now()
	a.DeletedAt = &deletedAt
	return nil
}

func (r *fakeAvatarRepo) UpdateProcessingStatus(_ context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ProcessingStatus = status
	return nil
}

func (r *fakeAvatarRepo) UpdateThumbnails(_ context.Context, id uuid.UUID, thumbnails map[string]string) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ThumbnailS3Keys = thumbnails
	return nil
}

type fakeAvatarStorage struct {
	objects map[string][]byte
}

func newFakeAvatarStorage() *fakeAvatarStorage {
	return &fakeAvatarStorage{objects: make(map[string][]byte)}
}

func (s *fakeAvatarStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[key] = data
	return nil
}

func (s *fakeAvatarStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, repository.ErrAvatarNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeAvatarStorage) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

type fakeAvatarPublisher struct{}

func (fakeAvatarPublisher) Publish(_ context.Context, _ string, _ any) error { return nil }

// newTestAvatarHandler собирает полностью рабочий AvatarHandler на фейковой
// инфраструктуре — так тесты хендлера проверяют настоящую бизнес-логику
// (AvatarService без изменений), а не только маршрутизацию HTTP.
func newTestAvatarHandler() (*AvatarHandler, *fakeAvatarRepo, *fakeAvatarStorage) {
	repo := newFakeAvatarRepo()
	storage := newFakeAvatarStorage()
	svc := services.NewAvatarService(repo, storage, fakeAvatarPublisher{})
	return NewAvatarHandler(svc), repo, storage
}

func generateTestJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

// newMultipartUploadRequest собирает multipart/form-data запрос так же,
// как это делает браузер при отправке формы или curl -F.
func newMultipartUploadRequest(t *testing.T, url, userID, fileName string, content []byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if fileName != "" {
		part, err := w.CreateFormFile("file", fileName)
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	return req
}

func TestAvatarHandler_UploadAvatar_MissingUserID(t *testing.T) {
	h, _, _ := newTestAvatarHandler()

	req := newMultipartUploadRequest(t, "/api/v1/avatars", "", "photo.jpg", generateTestJPEG(t))
	rec := httptest.NewRecorder()

	h.UploadAvatar(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAvatarHandler_UploadAvatar_MissingFile(t *testing.T) {
	h, _, _ := newTestAvatarHandler()

	req := newMultipartUploadRequest(t, "/api/v1/avatars", "user-1", "", nil)
	rec := httptest.NewRecorder()

	h.UploadAvatar(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAvatarHandler_UploadAvatar_Success(t *testing.T) {
	h, repo, storage := newTestAvatarHandler()

	req := newMultipartUploadRequest(t, "/api/v1/avatars", "user-1", "photo.jpg", generateTestJPEG(t))
	rec := httptest.NewRecorder()

	h.UploadAvatar(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp uploadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "user-1", resp.UserID)
	require.Equal(t, "pending", resp.Status)

	require.Contains(t, repo.avatars, uuid.MustParse(resp.ID))
	require.NotEmpty(t, storage.objects)
}

func TestAvatarHandler_GetAvatar_NotFound(t *testing.T) {
	h, _, _ := newTestAvatarHandler()

	r := chi.NewRouter()
	r.Get("/api/v1/avatars/{avatarID}", h.GetAvatar)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAvatarHandler_GetUserAvatar_ReturnsPlaceholderWhenMissing(t *testing.T) {
	h, _, _ := newTestAvatarHandler()

	r := chi.NewRouter()
	r.Get("/api/v1/users/{userID}/avatar", h.GetUserAvatar)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/nobody/avatar", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.NotEmpty(t, rec.Body.Bytes())
}

func TestAvatarHandler_DeleteAvatar_Forbidden(t *testing.T) {
	h, repo, _ := newTestAvatarHandler()

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{ID: id, UserID: "owner", S3Key: "originals/x.jpg"}

	r := chi.NewRouter()
	r.Delete("/api/v1/avatars/{avatarID}", h.DeleteAvatar)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+id.String(), nil)
	req.Header.Set("X-User-ID", "someone-else")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAvatarHandler_DeleteAvatar_Success(t *testing.T) {
	h, repo, _ := newTestAvatarHandler()

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{ID: id, UserID: "owner", S3Key: "originals/x.jpg"}

	r := chi.NewRouter()
	r.Delete("/api/v1/avatars/{avatarID}", h.DeleteAvatar)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+id.String(), nil)
	req.Header.Set("X-User-ID", "owner")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.NotNil(t, repo.avatars[id].DeletedAt)
}
