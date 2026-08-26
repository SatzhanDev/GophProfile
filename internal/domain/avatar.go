// Package domain содержит доменные модели сервиса — структуры, которые
// не знают ни про HTTP, ни про PostgreSQL, ни про S3. Это просто данные
// и правила, общие для всего приложения.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// UploadStatus описывает состояние загрузки оригинала файла в S3.
type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusFailed    UploadStatus = "failed"
)

// ProcessingStatus описывает состояние асинхронной обработки (создание миниатюр).
type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

// Avatar — доменная модель аватарки. Один в один описывает строку таблицы
// avatars из миграции 000001_create_avatars_table.
type Avatar struct {
	ID     uuid.UUID
	UserID string

	FileName  string
	MimeType  string
	SizeBytes int64

	S3Key string
	// ThumbnailS3Keys — ключи миниатюр в S3, ключ мапы — размер ("100x100"),
	// значение — ключ объекта в S3 (например, "thumbnails/<id>/100x100.jpg").
	// Заполняется воркером после обработки, поэтому изначально nil.
	ThumbnailS3Keys map[string]string

	UploadStatus     UploadStatus
	ProcessingStatus ProcessingStatus

	CreatedAt time.Time
	UpdatedAt time.Time
	// DeletedAt — метка мягкого удаления. nil означает, что аватарка активна.
	DeletedAt *time.Time
}

// IsDeleted сообщает, была ли аватарка мягко удалена.
func (a *Avatar) IsDeleted() bool {
	return a.DeletedAt != nil
}
