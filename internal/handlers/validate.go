package handlers

import "regexp"

// userIDPattern — разумный "опаковый" идентификатор: буквы, цифры и
// несколько безопасных спецсимволов, которые встречаются в юзернеймах
// и email-адресах (в оригинальной идее GophProfile аватарка ищется именно
// по email). Пробелы, слэши и прочие символы, которые могли бы что-то
// значить для URL/заголовков/логов ниже по цепочке, запрещены.
var userIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._@+-]{1,255}$`)

// isValidUserID проверяет формат идентификатора пользователя.
// 255 символов — не случайное число, а предел колонки user_id
// в таблице avatars (VARCHAR(255)), см. migrations/000001_....up.sql.
func isValidUserID(id string) bool {
	return userIDPattern.MatchString(id)
}
