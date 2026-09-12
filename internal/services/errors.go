package services

import "errors"

var (
	// ErrAvatarNotFound — аватарки с таким id нет или она уже удалена.
	// Это "перевод" repository.ErrAvatarNotFound на уровень сервиса:
	// вызывающий код (хендлеры) не должен знать о деталях реализации
	// хранилища — ни что это PostgreSQL, ни структуру его ошибок.
	ErrAvatarNotFound = errors.New("avatar not found")
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
