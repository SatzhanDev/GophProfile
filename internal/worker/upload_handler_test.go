package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
)

type fakeWorkerRepo struct {
	avatars map[uuid.UUID]*domain.Avatar
}

func newFakeWorkerRepo() *fakeWorkerRepo {
	return &fakeWorkerRepo{avatars: make(map[uuid.UUID]*domain.Avatar)}
}

func (r *fakeWorkerRepo) Create(_ context.Context, a *domain.Avatar) error {
	r.avatars[a.ID] = a
	return nil
}

func (r *fakeWorkerRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Avatar, error) {
	a, ok := r.avatars[id]
	if !ok {
		return nil, repository.ErrAvatarNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeWorkerRepo) ListByUserID(_ context.Context, _ string) ([]domain.Avatar, error) {
	return nil, nil
}

func (r *fakeWorkerRepo) GetLatestByUserID(_ context.Context, _ string) (*domain.Avatar, error) {
	return nil, repository.ErrAvatarNotFound
}

func (r *fakeWorkerRepo) SoftDelete(_ context.Context, _ uuid.UUID) error { return nil }

func (r *fakeWorkerRepo) UpdateProcessingStatus(_ context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ProcessingStatus = status
	return nil
}

func (r *fakeWorkerRepo) UpdateThumbnails(_ context.Context, id uuid.UUID, thumbnails map[string]string) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ThumbnailS3Keys = thumbnails
	return nil
}

type fakeWorkerStorage struct {
	objects     map[string][]byte
	downloadErr error
}

func newFakeWorkerStorage() *fakeWorkerStorage {
	return &fakeWorkerStorage{objects: make(map[string][]byte)}
}

func (s *fakeWorkerStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[key] = data
	return nil
}

func (s *fakeWorkerStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	if s.downloadErr != nil {
		return nil, s.downloadErr
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeWorkerStorage) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

// generateTestJPEGBytes рисует крошечную валидную JPEG-картинку — ровно то,
// что UploadHandler ожидает скачать из S3 и превратить в миниатюры.
func generateTestJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestUploadHandler_Handle_Success(t *testing.T) {
	repo := newFakeWorkerRepo()
	storage := newFakeWorkerStorage()
	h := NewUploadHandler(repo, storage)

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{
		ID:               id,
		UserID:           "user-1",
		S3Key:            "originals/x.jpg",
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	storage.objects["originals/x.jpg"] = generateTestJPEGBytes(t)

	err := h.Handle(context.Background(), domain.AvatarUploadEvent{
		AvatarID: id.String(),
		UserID:   "user-1",
		S3Key:    "originals/x.jpg",
	})
	require.NoError(t, err)

	got := repo.avatars[id]
	require.Equal(t, domain.ProcessingStatusCompleted, got.ProcessingStatus)
	require.Len(t, got.ThumbnailS3Keys, 2)
	require.Contains(t, got.ThumbnailS3Keys, "100x100")
	require.Contains(t, got.ThumbnailS3Keys, "300x300")

	for _, key := range got.ThumbnailS3Keys {
		require.Contains(t, storage.objects, key, "thumbnail must actually be uploaded to storage")
	}
}

func TestUploadHandler_Handle_AlreadyCompleted_IsIdempotent(t *testing.T) {
	repo := newFakeWorkerRepo()
	storage := newFakeWorkerStorage()
	h := NewUploadHandler(repo, storage)

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{
		ID:               id,
		S3Key:            "originals/x.jpg",
		ProcessingStatus: domain.ProcessingStatusCompleted, // уже обработано раньше
	}
	// В хранилище оригинала специально нет — если бы идемпотентность не
	// сработала, Handle попытался бы его скачать и упал с ошибкой.

	err := h.Handle(context.Background(), domain.AvatarUploadEvent{AvatarID: id.String()})
	require.NoError(t, err)
	require.Empty(t, storage.objects, "must not touch storage for an already-processed avatar")
}

func TestUploadHandler_Handle_AvatarDeletedMeanwhile(t *testing.T) {
	// Аватарку успели удалить, пока сообщение ждало обработки — это не
	// ошибка, обрабатывать просто нечего.
	repo := newFakeWorkerRepo()
	h := NewUploadHandler(repo, newFakeWorkerStorage())

	err := h.Handle(context.Background(), domain.AvatarUploadEvent{AvatarID: uuid.NewString()})
	require.NoError(t, err)
}

func TestUploadHandler_Handle_DownloadFailure_MarksFailed(t *testing.T) {
	repo := newFakeWorkerRepo()
	storage := newFakeWorkerStorage()
	storage.downloadErr = errors.New("s3 unavailable")
	h := NewUploadHandler(repo, storage)

	id := uuid.New()
	repo.avatars[id] = &domain.Avatar{ID: id, S3Key: "originals/x.jpg", ProcessingStatus: domain.ProcessingStatusPending}

	err := h.Handle(context.Background(), domain.AvatarUploadEvent{AvatarID: id.String()})
	require.Error(t, err)
	require.Equal(t, domain.ProcessingStatusFailed, repo.avatars[id].ProcessingStatus)
}
