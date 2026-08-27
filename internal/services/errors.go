package services

import "errors"

var (
	// ErrFileTooLarge — файл больше MaxAvatarSize.
	ErrFileTooLarge = errors.New("file too large")
	// ErrUnsupportedType — MIME-тип не входит в allowedMimeTypes.
	ErrUnsupportedType = errors.New("unsupported file type")
	// ErrForbidden — пользователь пытается удалить чужую аватарку.
	ErrForbidden = errors.New("forbidden")
	// ErrThumbnailNotAvailable — запрошен размер, для которого миниатюра
	// ещё не сгенерирована воркером (или это вообще не существующий размер).
	ErrThumbnailNotAvailable = errors.New("thumbnail not available")
)
