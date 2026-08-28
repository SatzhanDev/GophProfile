package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
)

// --- фейки зависимостей ---
//
// AvatarService работает с тремя маленькими интерфейсами (repository.AvatarRepository,
// Storage, EventPublisher) — именно поэтому его можно протестировать полностью
// в памяти, без реальных PostgreSQL/S3/RabbitMQ.

type fakeRepo struct {
	avatars map[uuid.UUID]*domain.Avatar

	createErr error
	getErr    error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{avatars: make(map[uuid.UUID]*domain.Avatar)}
}

func (r *fakeRepo) Create(_ context.Context, a *domain.Avatar) error {
	if r.createErr != nil {
		return r.createErr
	}
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	cp := *a
	r.avatars[a.ID] = &cp
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Avatar, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	a, ok := r.avatars[id]
	if !ok || a.DeletedAt != nil {
		return nil, repository.ErrAvatarNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeRepo) ListByUserID(_ context.Context, userID string) ([]domain.Avatar, error) {
	var result []domain.Avatar
	for _, a := range r.avatars {
		if a.UserID == userID && a.DeletedAt == nil {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (r *fakeRepo) GetLatestByUserID(_ context.Context, userID string) (*domain.Avatar, error) {
	var latest *domain.Avatar
	for _, a := range r.avatars {
		if a.UserID != userID || a.DeletedAt != nil {
			continue
		}
		if latest == nil || a.CreatedAt.After(latest.CreatedAt) {
			latest = a
		}
	}
	if latest == nil {
		return nil, repository.ErrAvatarNotFound
	}
	cp := *latest
	return &cp, nil
}

func (r *fakeRepo) SoftDelete(_ context.Context, id uuid.UUID) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	now := time.Now()
	a.DeletedAt = &now
	return nil
}

func (r *fakeRepo) UpdateProcessingStatus(_ context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ProcessingStatus = status
	return nil
}

func (r *fakeRepo) UpdateThumbnails(_ context.Context, id uuid.UUID, thumbnails map[string]string) error {
	a, ok := r.avatars[id]
	if !ok {
		return repository.ErrAvatarNotFound
	}
	a.ThumbnailS3Keys = thumbnails
	return nil
}

type fakeStorage struct {
	objects   map[string][]byte
	uploadErr error
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{objects: make(map[string][]byte)}
}

func (s *fakeStorage) Upload(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	if s.uploadErr != nil {
		return s.uploadErr
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.objects[key] = data
	return nil
}

func (s *fakeStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

type publishedEvent struct {
	routingKey string
	payload    any
}

type fakePublisher struct {
	events     []publishedEvent
	publishErr error
}

func (p *fakePublisher) Publish(_ context.Context, routingKey string, payload any) error {
	p.events = append(p.events, publishedEvent{routingKey: routingKey, payload: payload})
	return p.publishErr
}

// --- тесты ---

func TestAvatarService_Upload_Success(t *testing.T) {
	repo := newFakeRepo()
	storage := newFakeStorage()
	publisher := &fakePublisher{}
	svc := NewAvatarService(repo, storage, publisher)

	content := []byte("fake jpeg bytes")
	avatar, err := svc.Upload(context.Background(), "user-1", "photo.jpg", "image/jpeg", int64(len(content)), bytes.NewReader(content))
	require.NoError(t, err)

	require.Equal(t, "user-1", avatar.UserID)
	require.Equal(t, domain.UploadStatusCompleted, avatar.UploadStatus)
	require.Equal(t, domain.ProcessingStatusPending, avatar.ProcessingStatus)

	require.Equal(t, content, storage.objects[avatar.S3Key])
	require.Contains(t, repo.avatars, avatar.ID)

	require.Len(t, publisher.events, 1)
	require.Equal(t, domain.RoutingKeyAvatarUploaded, publisher.events[0].routingKey)
	event, ok := publisher.events[0].payload.(domain.AvatarUploadEvent)
	require.True(t, ok)
	require.Equal(t, avatar.ID.String(), event.AvatarID)
	require.Equal(t, avatar.S3Key, event.S3Key)
}

func TestAvatarService_Upload_FileTooLarge(t *testing.T) {
	repo := newFakeRepo()
	storage := newFakeStorage()
	publisher := &fakePublisher{}
	svc := NewAvatarService(repo, storage, publisher)

	_, err := svc.Upload(context.Background(), "user-1", "photo.jpg", "image/jpeg", MaxAvatarSize+1, bytes.NewReader(nil))
	require.ErrorIs(t, err, ErrFileTooLarge)

	require.Empty(t, storage.objects, "storage must not be touched when validation fails")
	require.Empty(t, repo.avatars, "repository must not be touched when validation fails")
}

func TestAvatarService_Upload_UnsupportedType(t *testing.T) {
	svc := NewAvatarService(newFakeRepo(), newFakeStorage(), &fakePublisher{})

	_, err := svc.Upload(context.Background(), "user-1", "file.txt", "text/plain", 10, bytes.NewReader([]byte("hello")))
	require.ErrorIs(t, err, ErrUnsupportedType)
}

func TestAvatarService_Upload_StorageFailure(t *testing.T) {
	repo := newFakeRepo()
	storage := newFakeStorage()
	storage.uploadErr = errors.New("s3 is down")
	svc := NewAvatarService(repo, storage, &fakePublisher{})

	_, err := svc.Upload(context.Background(), "user-1", "photo.jpg", "image/jpeg", 5, bytes.NewReader([]byte("hello")))
	require.Error(t, err)
	require.Empty(t, repo.avatars, "metadata must not be saved if the upload to S3 failed")
}

func TestAvatarService_GetFile(t *testing.T) {
	repo := newFakeRepo()
	storage := newFakeStorage()
	svc := NewAvatarService(repo, storage, &fakePublisher{})

	avatarID := uuid.New()
	repo.avatars[avatarID] = &domain.Avatar{
		ID:              avatarID,
		UserID:          "user-1",
		MimeType:        "image/png",
		S3Key:           "originals/x.png",
		ThumbnailS3Keys: map[string]string{"100x100": "thumbnails/x/100x100.jpg"},
	}
	storage.objects["originals/x.png"] = []byte("original bytes")
	storage.objects["thumbnails/x/100x100.jpg"] = []byte("thumbnail bytes")

	t.Run("original", func(t *testing.T) {
		avatar, body, contentType, err := svc.GetFile(context.Background(), avatarID, "")
		require.NoError(t, err)
		defer func() { _ = body.Close() }()

		require.Equal(t, "image/png", contentType, "original keeps its own MIME type")
		data, _ := io.ReadAll(body)
		require.Equal(t, "original bytes", string(data))
		require.Equal(t, avatarID, avatar.ID)
	})

	t.Run("thumbnail", func(t *testing.T) {
		_, body, contentType, err := svc.GetFile(context.Background(), avatarID, "100x100")
		require.NoError(t, err)
		defer func() { _ = body.Close() }()

		require.Equal(t, "image/jpeg", contentType, "thumbnails are always JPEG regardless of original format")
		data, _ := io.ReadAll(body)
		require.Equal(t, "thumbnail bytes", string(data))
	})

	t.Run("missing thumbnail size", func(t *testing.T) {
		_, _, _, err := svc.GetFile(context.Background(), avatarID, "999x999")
		require.ErrorIs(t, err, ErrThumbnailNotAvailable)
	})

	t.Run("unknown avatar", func(t *testing.T) {
		_, _, _, err := svc.GetFile(context.Background(), uuid.New(), "")
		require.ErrorIs(t, err, repository.ErrAvatarNotFound)
	})
}

func TestAvatarService_Delete(t *testing.T) {
	t.Run("forbidden for non-owner", func(t *testing.T) {
		repo := newFakeRepo()
		publisher := &fakePublisher{}
		svc := NewAvatarService(repo, newFakeStorage(), publisher)

		id := uuid.New()
		repo.avatars[id] = &domain.Avatar{ID: id, UserID: "owner", S3Key: "originals/x.jpg"}

		err := svc.Delete(context.Background(), id, "someone-else")
		require.ErrorIs(t, err, ErrForbidden)
		require.Empty(t, publisher.events, "must not publish anything when the delete was rejected")
	})

	t.Run("not found", func(t *testing.T) {
		svc := NewAvatarService(newFakeRepo(), newFakeStorage(), &fakePublisher{})

		err := svc.Delete(context.Background(), uuid.New(), "user-1")
		require.ErrorIs(t, err, repository.ErrAvatarNotFound)
	})

	t.Run("success publishes all s3 keys", func(t *testing.T) {
		repo := newFakeRepo()
		publisher := &fakePublisher{}
		svc := NewAvatarService(repo, newFakeStorage(), publisher)

		id := uuid.New()
		repo.avatars[id] = &domain.Avatar{
			ID:              id,
			UserID:          "owner",
			S3Key:           "originals/x.jpg",
			ThumbnailS3Keys: map[string]string{"100x100": "thumbnails/x/100x100.jpg", "300x300": "thumbnails/x/300x300.jpg"},
		}

		err := svc.Delete(context.Background(), id, "owner")
		require.NoError(t, err)

		require.NotNil(t, repo.avatars[id].DeletedAt, "soft delete must set deleted_at")

		require.Len(t, publisher.events, 1)
		require.Equal(t, domain.RoutingKeyAvatarDeleted, publisher.events[0].routingKey)
		event, ok := publisher.events[0].payload.(domain.AvatarDeleteEvent)
		require.True(t, ok)
		require.ElementsMatch(t, []string{
			"originals/x.jpg",
			"thumbnails/x/100x100.jpg",
			"thumbnails/x/300x300.jpg",
		}, event.S3Keys)
	})
}

func TestAvatarService_GetLatestForUser_PicksMostRecent(t *testing.T) {
	repo := newFakeRepo()
	storage := newFakeStorage()
	svc := NewAvatarService(repo, storage, &fakePublisher{})

	older := uuid.New()
	newer := uuid.New()
	now := time.Now()
	repo.avatars[older] = &domain.Avatar{ID: older, UserID: "user-1", S3Key: "originals/older.jpg", CreatedAt: now}
	repo.avatars[newer] = &domain.Avatar{ID: newer, UserID: "user-1", S3Key: "originals/newer.jpg", CreatedAt: now.Add(time.Hour)}
	storage.objects["originals/newer.jpg"] = []byte("newer")

	avatar, body, err := svc.GetLatestForUser(context.Background(), "user-1")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	require.Equal(t, newer, avatar.ID)
}
