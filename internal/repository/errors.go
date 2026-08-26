package repository

import "errors"

// ErrAvatarNotFound возвращается репозиторием, если записи с таким
// id/user_id нет вовсе или она уже мягко удалена (deleted_at IS NOT NULL).
// Хендлеры выше по стеку по этой ошибке решают, что отдавать клиенту 404.
var ErrAvatarNotFound = errors.New("avatar not found")
