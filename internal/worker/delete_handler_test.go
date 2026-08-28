package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
)

func TestDeleteHandler_Handle_DeletesAllKeys(t *testing.T) {
	storage := newFakeWorkerStorage()
	storage.objects["originals/x.jpg"] = []byte("original")
	storage.objects["thumbnails/x/100x100.jpg"] = []byte("thumb100")
	storage.objects["thumbnails/x/300x300.jpg"] = []byte("thumb300")

	h := NewDeleteHandler(storage)

	err := h.Handle(context.Background(), domain.AvatarDeleteEvent{
		AvatarID: "avatar-1",
		S3Keys: []string{
			"originals/x.jpg",
			"thumbnails/x/100x100.jpg",
			"thumbnails/x/300x300.jpg",
		},
	})
	require.NoError(t, err)
	require.Empty(t, storage.objects, "all listed objects must be deleted")
}

func TestDeleteHandler_Handle_DeletingMissingObjectIsFine(t *testing.T) {
	// Естественная идемпотентность: объекта уже нет (например, повторная
	// доставка того же события) — это не должно быть ошибкой.
	storage := newFakeWorkerStorage()
	h := NewDeleteHandler(storage)

	err := h.Handle(context.Background(), domain.AvatarDeleteEvent{
		AvatarID: "avatar-1",
		S3Keys:   []string{"originals/does-not-exist.jpg"},
	})
	require.NoError(t, err)
}

// storageWithFailingDelete позволяет проверить, что при реальной ошибке
// (не "объекта нет", а именно сбой) Handle её не проглатывает.
type storageWithFailingDelete struct {
	*fakeWorkerStorage
}

func (s storageWithFailingDelete) Delete(_ context.Context, _ string) error {
	return errors.New("s3 unavailable")
}

func TestDeleteHandler_Handle_PropagatesRealErrors(t *testing.T) {
	h := NewDeleteHandler(storageWithFailingDelete{newFakeWorkerStorage()})

	err := h.Handle(context.Background(), domain.AvatarDeleteEvent{
		AvatarID: "avatar-1",
		S3Keys:   []string{"originals/x.jpg"},
	})
	require.Error(t, err)
}
