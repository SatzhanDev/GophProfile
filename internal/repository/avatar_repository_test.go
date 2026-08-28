package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/domain"
)

// pgxmock подделывает пул соединений pgx целиком, не поднимая реальный
// PostgreSQL — благодаря тому, что PostgresAvatarRepository теперь зависит
// от узкого интерфейса pgxPool, а не от конкретного *pgxpool.Pool, эти
// тесты можно писать без единого реального SQL-запроса к базе. Полный,
// настоящий SQL проверяется отдельно интеграционным тестом
// (tests/integration/avatar_repository_test.go) через testcontainers-go.

func TestPostgresAvatarRepository_Create(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	avatar := &domain.Avatar{
		UserID:           "user-1",
		FileName:         "photo.jpg",
		MimeType:         "image/jpeg",
		SizeBytes:        1024,
		S3Key:            "originals/x.jpg",
		UploadStatus:     domain.UploadStatusCompleted,
		ProcessingStatus: domain.ProcessingStatusPending,
	}

	now := time.Now()
	mock.ExpectQuery("INSERT INTO avatars").
		WithArgs(pgxmock.AnyArg(), avatar.UserID, avatar.FileName, avatar.MimeType, avatar.SizeBytes, avatar.S3Key, avatar.UploadStatus, avatar.ProcessingStatus).
		WillReturnRows(pgxmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	err = repo.Create(context.Background(), avatar)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, avatar.ID, "Create must generate an id when one wasn't provided")
	require.WithinDuration(t, now, avatar.CreatedAt, 0)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresAvatarRepository_GetByID_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	id := uuid.New()
	now := time.Now()
	thumbnails := []byte(`{"100x100":"thumbnails/x/100x100.jpg"}`)

	mock.ExpectQuery("SELECT (.|\n)* FROM avatars").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "user_id", "file_name", "mime_type", "size_bytes", "s3_key", "thumbnail_s3_keys",
			"upload_status", "processing_status", "created_at", "updated_at", "deleted_at",
		}).AddRow(
			id, "user-1", "photo.jpg", "image/jpeg", int64(1024), "originals/x.jpg", thumbnails,
			domain.UploadStatusCompleted, domain.ProcessingStatusCompleted, now, now, nil,
		))

	got, err := repo.GetByID(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, "user-1", got.UserID)
	require.Equal(t, map[string]string{"100x100": "thumbnails/x/100x100.jpg"}, got.ThumbnailS3Keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresAvatarRepository_GetByID_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	id := uuid.New()
	mock.ExpectQuery("SELECT (.|\n)* FROM avatars").
		WithArgs(id).
		WillReturnError(pgx.ErrNoRows)

	_, err = repo.GetByID(context.Background(), id)
	require.ErrorIs(t, err, ErrAvatarNotFound)
}

func TestPostgresAvatarRepository_ListByUserID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	now := time.Now()
	rows := pgxmock.NewRows([]string{
		"id", "user_id", "file_name", "mime_type", "size_bytes", "s3_key", "thumbnail_s3_keys",
		"upload_status", "processing_status", "created_at", "updated_at", "deleted_at",
	}).
		AddRow(uuid.New(), "user-1", "a.jpg", "image/jpeg", int64(1), "originals/a.jpg", nil, domain.UploadStatusCompleted, domain.ProcessingStatusCompleted, now, now, nil).
		AddRow(uuid.New(), "user-1", "b.jpg", "image/jpeg", int64(1), "originals/b.jpg", nil, domain.UploadStatusCompleted, domain.ProcessingStatusPending, now, now, nil)

	mock.ExpectQuery("SELECT (.|\n)* FROM avatars").WithArgs("user-1").WillReturnRows(rows)

	avatars, err := repo.ListByUserID(context.Background(), "user-1")
	require.NoError(t, err)
	require.Len(t, avatars, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresAvatarRepository_SoftDelete_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	id := uuid.New()
	mock.ExpectExec("UPDATE avatars").
		WithArgs(id).
		WillReturnResult(pgconn.NewCommandTag("UPDATE 0"))

	err = repo.SoftDelete(context.Background(), id)
	require.ErrorIs(t, err, ErrAvatarNotFound)
}

func TestPostgresAvatarRepository_UpdateThumbnails_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := NewPostgresAvatarRepository(mock)

	id := uuid.New()
	mock.ExpectExec("UPDATE avatars").
		WithArgs(id, pgxmock.AnyArg()).
		WillReturnResult(pgconn.NewCommandTag("UPDATE 1"))

	err = repo.UpdateThumbnails(context.Background(), id, map[string]string{"100x100": "thumbnails/x/100x100.jpg"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
