package handlers

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
)

// extractUploadedFile достаёт файл из поля формы "file" и определяет его
// реальный MIME-тип по содержимому (magic bytes). Общий код для
// AvatarHandler.UploadAvatar (JSON API) и WebHandler.UploadSubmit
// (классическая HTML-форма) — оба должны сделать это совершенно одинаково.
//
// Вызывающий код отвечает за file.Close().
func extractUploadedFile(r *http.Request) (multipart.File, *multipart.FileHeader, string, error) {
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, "", err
	}

	sniff := make([]byte, 512)
	n, err := file.Read(sniff)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = file.Close()
		return nil, nil, "", err
	}
	detectedType := http.DetectContentType(sniff[:n])

	// multipart.File — это ещё и io.Seeker, перематываем в начало вместо
	// склейки уже прочитанных байт с остатком потока через MultiReader.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, nil, "", err
	}

	return file, header, detectedType, nil
}
