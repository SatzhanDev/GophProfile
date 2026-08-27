// Package thumbnail умеет из произвольного изображения (JPEG/PNG/WebP)
// сделать квадратную миниатюру нужного размера — ровно то, что описано
// в ТЗ: "создание миниатюр 100x100, 300x300".
package thumbnail

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // регистрирует JPEG-декодер для image.Decode
	_ "image/png"  // регистрирует PNG-декодер для image.Decode
	"io"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"
)

func init() {
	// Стандартная библиотека не умеет декодировать WebP — регистрируем
	// декодер из golang.org/x/image вручную, тем же способом, каким
	// image/jpeg и image/png регистрируют себя сами через blank-импорт.
	// После этого image.Decode понимает все три формата одинаково.
	image.RegisterFormat("webp", "RIFF????WEBP", webp.Decode, webp.DecodeConfig)
}

// Generate декодирует изображение из r, обрезает его по центру до квадрата
// и уменьшает до size×size пикселей, возвращая результат как JPEG-байты.
//
// Кодируем миниатюры всегда в JPEG независимо от формата оригинала —
// компактно, поддерживается везде, а для маленькой картинки-иконки
// разница в качестве с PNG/WebP не будет заметна глазом.
func Generate(r io.Reader, size int) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// imaging.Fill одновременно обрезает изображение до нужных пропорций
	// (по центру, imaging.Center) и масштабирует его — то есть делает
	// ровно то, что мы обсуждали: сначала кроп в квадрат, потом ресайз.
	thumb := imaging.Fill(img, size, size, imaging.Center, imaging.Lanczos)

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumb, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}

	return buf.Bytes(), nil
}
