// Package repository отвечает за доступ к данным. Выше (в services/handlers)
// код работает с интерфейсом AvatarRepository и не знает, что за ним
// стоит именно PostgreSQL — это можно будет подменить, например, в тестах.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SatzhanDev/GophProfile/internal/domain"
)

// AvatarRepository описывает операции над таблицей avatars, которые
// нужны остальному приложению. Конкретная реализация — PostgresAvatarRepository.
type AvatarRepository interface {
	// Create сохраняет новую аватарку. Если a.ID пустой (uuid.Nil),
	// репозиторий сгенерирует его сам и запишет обратно в a.ID.
	Create(ctx context.Context, a *domain.Avatar) error

	// GetByID возвращает аватарку по id. Если её нет или она удалена —
	// ErrAvatarNotFound.
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)

	// ListByUserID возвращает все не удалённые аватарки пользователя,
	// от новых к старым.
	ListByUserID(ctx context.Context, userID string) ([]domain.Avatar, error)

	// GetLatestByUserID возвращает самую свежую аватарку пользователя —
	// то, что нужно отдавать по GET /users/{user_id}/avatar.
	GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error)

	// SoftDelete проставляет deleted_at, саму запись из БД не удаляет.
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// UpdateProcessingStatus меняет статус фоновой обработки —
	// вызывается воркером после генерации миниатюр.
	UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status domain.ProcessingStatus) error

	// UpdateThumbnails записывает ключи сгенерированных миниатюр в S3.
	UpdateThumbnails(ctx context.Context, id uuid.UUID, thumbnails map[string]string) error
}

// PostgresAvatarRepository — реализация AvatarRepository поверх pgx.
type PostgresAvatarRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresAvatarRepository создаёт репозиторий поверх готового пула соединений.
func NewPostgresAvatarRepository(pool *pgxpool.Pool) *PostgresAvatarRepository {
	return &PostgresAvatarRepository{pool: pool}
}

func (r *PostgresAvatarRepository) Create(ctx context.Context, a *domain.Avatar) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}

	const query = `
		INSERT INTO avatars (id, user_id, file_name, mime_type, size_bytes, s3_key, upload_status, processing_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query,
		a.ID, a.UserID, a.FileName, a.MimeType, a.SizeBytes, a.S3Key, a.UploadStatus, a.ProcessingStatus,
	).Scan(&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert avatar: %w", err)
	}

	return nil
}

func (r *PostgresAvatarRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	const query = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
		       upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL
	`

	return r.scanOne(r.pool.QueryRow(ctx, query, id))
}

func (r *PostgresAvatarRepository) ListByUserID(ctx context.Context, userID string) ([]domain.Avatar, error) {
	const query = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
		       upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list avatars by user_id: %w", err)
	}
	defer rows.Close()

	var avatars []domain.Avatar
	for rows.Next() {
		a, err := scanAvatar(rows)
		if err != nil {
			return nil, fmt.Errorf("scan avatar row: %w", err)
		}
		avatars = append(avatars, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate avatar rows: %w", err)
	}

	return avatars, nil
}

func (r *PostgresAvatarRepository) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	const query = `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
		       upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`

	return r.scanOne(r.pool.QueryRow(ctx, query, userID))
}

func (r *PostgresAvatarRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE avatars
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("soft delete avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAvatarNotFound
	}

	return nil
}

func (r *PostgresAvatarRepository) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status domain.ProcessingStatus) error {
	const query = `
		UPDATE avatars
		SET processing_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	tag, err := r.pool.Exec(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("update processing status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAvatarNotFound
	}

	return nil
}

func (r *PostgresAvatarRepository) UpdateThumbnails(ctx context.Context, id uuid.UUID, thumbnails map[string]string) error {
	payload, err := json.Marshal(thumbnails)
	if err != nil {
		return fmt.Errorf("marshal thumbnails: %w", err)
	}

	const query = `
		UPDATE avatars
		SET thumbnail_s3_keys = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`

	tag, err := r.pool.Exec(ctx, query, id, payload)
	if err != nil {
		return fmt.Errorf("update thumbnails: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAvatarNotFound
	}

	return nil
}

// row — минимальный общий интерфейс pgx.Row/pgx.Rows, нужный нам только для Scan.
// Это позволяет использовать одну функцию сканирования и для QueryRow (один результат),
// и для Query (несколько результатов).
type row interface {
	Scan(dest ...any) error
}

func scanAvatar(rw row) (*domain.Avatar, error) {
	var a domain.Avatar
	var thumbnails []byte

	err := rw.Scan(
		&a.ID, &a.UserID, &a.FileName, &a.MimeType, &a.SizeBytes, &a.S3Key, &thumbnails,
		&a.UploadStatus, &a.ProcessingStatus, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(thumbnails) > 0 {
		if err := json.Unmarshal(thumbnails, &a.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("unmarshal thumbnails: %w", err)
		}
	}

	return &a, nil
}

// scanOne — общая обёртка для запросов, ожидающих ровно одну строку
// (GetByID, GetLatestByUserID): переводит pgx.ErrNoRows в ErrAvatarNotFound.
func (r *PostgresAvatarRepository) scanOne(rw row) (*domain.Avatar, error) {
	a, err := scanAvatar(rw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAvatarNotFound
		}
		return nil, fmt.Errorf("get avatar: %w", err)
	}

	return a, nil
}
