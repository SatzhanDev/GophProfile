// Package services содержит бизнес-логику приложения — то, что не является
// ни HTTP (handlers), ни доступом к данным (repository), а связывает их
// воедино по правилам, которые диктует продукт (лимиты, проверки прав и т.д.).
package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"

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

// EventPublisher — то, что нужно AvatarService от брокера сообщений:
// опубликовать событие с заданным routing key. Реальная реализация —
// *broker.Publisher (RabbitMQ), но сервис снова работает с интерфейсом.
type EventPublisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
}

// AvatarService реализует бизнес-правила поверх репозитория, хранилища
// и брокера сообщений.
type AvatarService struct {
	repo      repository.AvatarRepository
	storage   Storage
	publisher EventPublisher
}

// NewAvatarService создаёт AvatarService.
func NewAvatarService(repo repository.AvatarRepository, storage Storage, publisher EventPublisher) *AvatarService {
	return &AvatarService{repo: repo, storage: storage, publisher: publisher}
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

	// Файл и метаданные уже надёжно сохранены — это то, что действительно
	// важно для пользователя. Поэтому если публикация события не удалась
	// (RabbitMQ временно недоступен и т.п.), мы не проваливаем весь запрос
	// с ошибкой 500 — просто логируем: аватарка загружена, а генерация
	// миниатюр по ней отложится (в текущей версии — до следующей загрузки
	// или ручного вмешательства; полноценный outbox/ретрай — уже за рамками MVP).
	event := domain.AvatarUploadEvent{
		AvatarID: avatar.ID.String(),
		UserID:   avatar.UserID,
		S3Key:    avatar.S3Key,
	}
	if err := s.publisher.Publish(ctx, domain.RoutingKeyAvatarUploaded, event); err != nil {
		slog.Error("failed to publish avatar upload event", "avatar_id", avatar.ID, "error", err)
	}

	return avatar, nil
}

// thumbnailContentType — воркер (см. pkg/thumbnail) всегда кодирует
// миниатюры в JPEG независимо от формата оригинала, поэтому и Content-Type
// для них фиксированный, а не MimeType исходного файла.
const thumbnailContentType = "image/jpeg"

// GetFile отдаёт метаданные, поток байт и Content-Type нужного варианта
// аватарки. size == "" или "original" — оригинал (Content-Type — исходный
// avatar.MimeType). Любое другое значение ищется среди уже сгенерированных
// миниатюр (avatar.ThumbnailS3Keys, всегда JPEG); если воркер её ещё не
// сделал — вернётся ErrThumbnailNotAvailable.
func (s *AvatarService) GetFile(ctx context.Context, id uuid.UUID, size string) (*domain.Avatar, io.ReadCloser, string, error) {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}

	key := avatar.S3Key
	contentType := avatar.MimeType
	if size != "" && size != "original" {
		thumbKey, ok := avatar.ThumbnailS3Keys[size]
		if !ok {
			return nil, nil, "", ErrThumbnailNotAvailable
		}
		key = thumbKey
		contentType = thumbnailContentType
	}

	body, err := s.storage.Download(ctx, key)
	if err != nil {
		return nil, nil, "", fmt.Errorf("download object: %w", err)
	}

	return avatar, body, contentType, nil
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
func (s *AvatarService) Delete(ctx context.Context, id uuid.UUID, requesterUserID string) error {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if avatar.UserID != requesterUserID {
		return ErrForbidden
	}

	return s.softDeleteAndPublish(ctx, avatar)
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

	return s.softDeleteAndPublish(ctx, avatar)
}

// softDeleteAndPublish — общая часть Delete и DeleteLatestForUser: помечает
// запись удалённой в БД и публикует событие для воркера, чтобы тот в фоне
// стёр из S3 сам файл и все его миниатюры (по ТЗ — асинхронное удаление).
//
// Мягкое удаление в БД — это источник истины о том, видна ли аватарка
// пользователям; оно уже произошло к моменту публикации события, поэтому,
// как и при загрузке, ошибку публикации не считаем фатальной для запроса.
func (s *AvatarService) softDeleteAndPublish(ctx context.Context, avatar *domain.Avatar) error {
	if err := s.repo.SoftDelete(ctx, avatar.ID); err != nil {
		return err
	}

	keys := make([]string, 0, 1+len(avatar.ThumbnailS3Keys))
	keys = append(keys, avatar.S3Key)
	for _, thumbKey := range avatar.ThumbnailS3Keys {
		keys = append(keys, thumbKey)
	}

	event := domain.AvatarDeleteEvent{AvatarID: avatar.ID.String(), S3Keys: keys}
	if err := s.publisher.Publish(ctx, domain.RoutingKeyAvatarDeleted, event); err != nil {
		slog.Error("failed to publish avatar delete event", "avatar_id", avatar.ID, "error", err)
	}

	return nil
}
