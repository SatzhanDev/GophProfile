package worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/SatzhanDev/GophProfile/internal/domain"
)

// DeleteHandler обрабатывает событие avatar.deleted: стирает из S3 все
// перечисленные в событии объекты (оригинал и все успевшие сгенерироваться
// миниатюры).
type DeleteHandler struct {
	storage Storage
}

// NewDeleteHandler создаёт DeleteHandler.
func NewDeleteHandler(storage Storage) *DeleteHandler {
	return &DeleteHandler{storage: storage}
}

// Handle — точка входа, вызывается RunLoop на каждое событие из очереди
// avatars.cleanup.
//
// Отдельной проверки на "уже удалено" здесь не нужно — в отличие от
// генерации миниатюр, удаление объекта в S3 естественно идемпотентно:
// удалить уже удалённый (или никогда не существовавший) объект — не
// ошибка ни для MinIO, ни для настоящего AWS S3 (см. pkg/s3.Client.Delete).
func (h *DeleteHandler) Handle(ctx context.Context, event domain.AvatarDeleteEvent) error {
	for _, key := range event.S3Keys {
		if err := h.storage.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete object %q: %w", key, err)
		}
	}

	slog.Info("avatar files deleted", "avatar_id", event.AvatarID, "objects", len(event.S3Keys))
	return nil
}
