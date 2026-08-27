package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/pkg/thumbnail"
)

// thumbnailSizes — размеры миниатюр из ТЗ (100x100 и 300x300).
var thumbnailSizes = []int{100, 300}

// Storage — то же самое, что services.Storage: положить/скачать байты по
// ключу в S3. Объявлен здесь заново, а не импортирован из internal/services,
// потому что internal/worker не должен зависеть от internal/services —
// это два независимых потребителя одного и того же *s3.Client.
type Storage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// UploadHandler обрабатывает событие avatar.uploaded: генерирует миниатюры
// и обновляет статус обработки в БД.
type UploadHandler struct {
	repo    repository.AvatarRepository
	storage Storage
}

// NewUploadHandler создаёт UploadHandler.
func NewUploadHandler(repo repository.AvatarRepository, storage Storage) *UploadHandler {
	return &UploadHandler{repo: repo, storage: storage}
}

// Handle — точка входа, вызывается RunLoop на каждое событие из очереди
// avatars.thumbnails.
func (h *UploadHandler) Handle(ctx context.Context, event domain.AvatarUploadEvent) error {
	avatarID, err := uuid.Parse(event.AvatarID)
	if err != nil {
		return fmt.Errorf("parse avatar id %q: %w", event.AvatarID, err)
	}

	avatar, err := h.repo.GetByID(ctx, avatarID)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			// Аватарку успели удалить, пока сообщение ждало обработки —
			// это нормальный сценарий гонки, а не ошибка: обрабатывать нечего.
			slog.Info("avatar no longer exists, skipping", "avatar_id", event.AvatarID)
			return nil
		}
		return fmt.Errorf("get avatar: %w", err)
	}

	// Идемпотентность (п. 2.2 ТЗ): если аватарка уже обработана — например,
	// это повторная доставка одного и того же сообщения от RabbitMQ —
	// не переделываем работу заново.
	if avatar.ProcessingStatus == domain.ProcessingStatusCompleted {
		slog.Info("avatar already processed, skipping", "avatar_id", event.AvatarID)
		return nil
	}

	if err := h.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingStatusProcessing); err != nil {
		return fmt.Errorf("mark processing: %w", err)
	}

	thumbKeys, err := h.generateThumbnails(ctx, avatar)
	if err != nil {
		if markErr := h.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingStatusFailed); markErr != nil {
			slog.Error("failed to mark avatar as failed", "avatar_id", event.AvatarID, "error", markErr)
		}
		return fmt.Errorf("generate thumbnails: %w", err)
	}

	if err := h.repo.UpdateThumbnails(ctx, avatarID, thumbKeys); err != nil {
		return fmt.Errorf("save thumbnail keys: %w", err)
	}

	if err := h.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingStatusCompleted); err != nil {
		return fmt.Errorf("mark completed: %w", err)
	}

	slog.Info("avatar processed", "avatar_id", event.AvatarID, "thumbnails", len(thumbKeys))
	return nil
}

// generateThumbnails скачивает оригинал один раз и на его основе делает
// все размеры миниатюр из thumbnailSizes.
func (h *UploadHandler) generateThumbnails(ctx context.Context, avatar *domain.Avatar) (map[string]string, error) {
	original, err := h.storage.Download(ctx, avatar.S3Key)
	if err != nil {
		return nil, fmt.Errorf("download original: %w", err)
	}
	defer original.Close()

	// Читаем оригинал в память целиком: он не больше 10 МБ (лимит из ТЗ),
	// а нам нужно декодировать его повторно для каждого размера миниатюры —
	// удобнее иметь байты под рукой, чем перекачивать файл из S3 дважды.
	data, err := io.ReadAll(original)
	if err != nil {
		return nil, fmt.Errorf("read original: %w", err)
	}

	keys := make(map[string]string, len(thumbnailSizes))
	for _, size := range thumbnailSizes {
		thumbData, err := thumbnail.Generate(bytes.NewReader(data), size)
		if err != nil {
			return nil, fmt.Errorf("generate %dx%d thumbnail: %w", size, size, err)
		}

		sizeLabel := fmt.Sprintf("%dx%d", size, size)
		key := fmt.Sprintf("thumbnails/%s/%s.jpg", avatar.ID, sizeLabel)

		if err := h.storage.Upload(ctx, key, bytes.NewReader(thumbData), int64(len(thumbData)), "image/jpeg"); err != nil {
			return nil, fmt.Errorf("upload %s thumbnail: %w", sizeLabel, err)
		}

		keys[sizeLabel] = key
	}

	return keys, nil
}
