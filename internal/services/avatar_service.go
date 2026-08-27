// Package services содержит бизнес-логику приложения — то, что не является
// ни HTTP (handlers), ни доступом к данным (repository), а связывает их
// воедино по правилам, которые диктует продукт (лимиты, проверки прав и т.д.).
package services

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
)

// MaxAvatarSize — лимит на размер загружаемого файла, 10 МБ по ТЗ.
const MaxAvatarSize = 10 << 20

// allowedMimeTypes сопоставляет MIME-тип, который мы поддерживаем,
// расширению файла для ключа в S3. Список закрытый и намеренно короткий —
// ровно то, что просит ТЗ (JPEG, PNG, WebP).
var allowedMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// Storage — то, что нужно AvatarService от файлового хранилища: положить,
// скачать и удалить байты по ключу. Реальная реализация — *s3.Client,
// но сервис работает с интерфейсом, поэтому в тестах можно подставить фейк.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// AvatarService реализует бизнес-правила поверх репозитория и хранилища.
type AvatarService struct {
	repo    repository.AvatarRepository
	storage Storage
}

// NewAvatarService создаёт AvatarService.
func NewAvatarService(repo repository.AvatarRepository, storage Storage) *AvatarService {
	return &AvatarService{repo: repo, storage: storage}
}

// Upload проверяет лимиты, сохраняет оригинал в S3 и создаёт метаданные в БД.
//
// detectedMimeType должен быть определён по содержимому файла (magic bytes),
// а не взят из заголовка Content-Type от клиента — это ответственность
// вызывающего кода (см. handlers.AvatarHandler.UploadAvatar). Проверка
// размера здесь — это уже вторая линия защиты: основную отсечку делает
// http.MaxBytesReader ещё до захода в этот метод.
func (s *AvatarService) Upload(ctx context.Context, userID, fileName, detectedMimeType string, size int64, reader io.Reader) (*domain.Avatar, error) {
	if size > MaxAvatarSize {
		return nil, ErrFileTooLarge
	}

	ext, ok := allowedMimeTypes[detectedMimeType]
	if !ok {
		return nil, ErrUnsupportedType
	}

	avatar := &domain.Avatar{
		ID:               uuid.New(),
		UserID:           userID,
		FileName:         fileName,
		MimeType:         detectedMimeType,
		SizeBytes:        size,
		UploadStatus:     domain.UploadStatusUploading,
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	avatar.S3Key = fmt.Sprintf("originals/%s%s", avatar.ID, ext)

	if err := s.storage.Upload(ctx, avatar.S3Key, reader, size, detectedMimeType); err != nil {
		return nil, fmt.Errorf("upload original to s3: %w", err)
	}
	avatar.UploadStatus = domain.UploadStatusCompleted

	if err := s.repo.Create(ctx, avatar); err != nil {
		return nil, fmt.Errorf("save avatar metadata: %w", err)
	}

	return avatar, nil
}

// GetFile отдаёт метаданные и поток байт нужного варианта аватарки.
// size == "" или "original" — оригинал. Любое другое значение ищется
// среди уже сгенерированных миниатюр (avatar.ThumbnailS3Keys); пока воркер
// (появится в инкременте 6) их не сгенерировал — вернётся ErrThumbnailNotAvailable.
func (s *AvatarService) GetFile(ctx context.Context, id uuid.UUID, size string) (*domain.Avatar, io.ReadCloser, error) {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	key := avatar.S3Key
	if size != "" && size != "original" {
		thumbKey, ok := avatar.ThumbnailS3Keys[size]
		if !ok {
			return nil, nil, ErrThumbnailNotAvailable
		}
		key = thumbKey
	}

	body, err := s.storage.Download(ctx, key)
	if err != nil {
		return nil, nil, fmt.Errorf("download object: %w", err)
	}

	return avatar, body, nil
}

// GetLatestForUser нужен эндпоинту GET /users/{user_id}/avatar — самой
// свежей аватарке пользователя. Размер здесь не параметризуем: ТЗ не
// требует query-параметров для этого маршрута, только для /avatars/{id}.
func (s *AvatarService) GetLatestForUser(ctx context.Context, userID string) (*domain.Avatar, io.ReadCloser, error) {
	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	body, err := s.storage.Download(ctx, avatar.S3Key)
	if err != nil {
		return nil, nil, fmt.Errorf("download object: %w", err)
	}

	return avatar, body, nil
}

// GetMetadata отдаёт только метаданные, без содержимого файла.
func (s *AvatarService) GetMetadata(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	return s.repo.GetByID(ctx, id)
}

// ListForUser — все активные аватарки пользователя.
func (s *AvatarService) ListForUser(ctx context.Context, userID string) ([]domain.Avatar, error) {
	return s.repo.ListByUserID(ctx, userID)
}

// Delete мягко удаляет аватарку, только если запрашивает её владелец.
// Сам файл в S3 пока не трогаем — по ТЗ его удаление асинхронное, через
// брокер и воркера, это появится в инкременте 5-6.
func (s *AvatarService) Delete(ctx context.Context, id uuid.UUID, requesterUserID string) error {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if avatar.UserID != requesterUserID {
		return ErrForbidden
	}

	return s.repo.SoftDelete(ctx, id)
}

// DeleteLatestForUser — то же самое, что Delete, но для маршрута
// DELETE /users/{user_id}/avatar: удаляет самую свежую аватарку пользователя.
func (s *AvatarService) DeleteLatestForUser(ctx context.Context, userID, requesterUserID string) error {
	if userID != requesterUserID {
		return ErrForbidden
	}

	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return err
	}

	return s.repo.SoftDelete(ctx, avatar.ID)
}
