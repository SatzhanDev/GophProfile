package domain

// Routing key — "адрес" сообщения внутри exchange в RabbitMQ. Worker
// (появится в следующем инкременте) подпишется на очередь, привязанную
// к этим ключам, чтобы получать только нужные ему события.
const (
	RoutingKeyAvatarUploaded = "avatar.uploaded"
	RoutingKeyAvatarDeleted  = "avatar.deleted"
)

// AvatarUploadEvent публикуется сразу после того, как оригинал аватарки
// успешно сохранён в S3 и в БД появилась запись о нём. Worker по этому
// событию скачает оригинал и сгенерирует миниатюры.
type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}

// AvatarDeleteEvent публикуется после мягкого удаления записи в БД.
// S3Keys — все объекты, которые нужно реально стереть из хранилища:
// оригинал плюс все успевшие сгенерироваться миниатюры.
type AvatarDeleteEvent struct {
	AvatarID string   `json:"avatar_id"`
	S3Keys   []string `json:"s3_keys"`
}
