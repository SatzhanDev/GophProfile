// Package placeholder рисует изображение-заглушку для пользователей без
// своей аватарки — ровно то, что требует продуктовое описание GophProfile:
// "если аватарки нет — вернуть стандартную заглушку", а не ошибку.
//
// Картинка генерируется в памяти через стандартный image/png — никаких
// внешних файлов или библиотек, поэтому нечего класть в S3 и нечего сломать
// отсутствующим файлом в докер-образе.
package placeholder

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Generate рисует простой силуэт человека size×size пикселей на сером фоне
// и возвращает готовый PNG-файл в виде байтов.
func Generate(size int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	background := color.RGBA{R: 0xd9, G: 0xd9, B: 0xd9, A: 0xff} // светло-серый фон
	silhouette := color.RGBA{R: 0xa8, G: 0xa8, B: 0xa8, A: 0xff} // силуэт чуть темнее

	center := size / 2
	headRadius := size / 5
	headCenterY := size * 2 / 5
	shouldersTop := size * 3 / 5

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := background

			// Голова — просто множество точек на расстоянии <= headRadius
			// от своего центра (уравнение окружности через теорему Пифагора).
			dx, dy := x-center, y-headCenterY
			switch {
			case dx*dx+dy*dy <= headRadius*headRadius:
				c = silhouette
			case y >= shouldersTop:
				// Плечи — трапеция, расширяющаяся книзу.
				halfWidth := (y - shouldersTop) + headRadius
				if x >= center-halfWidth && x <= center+halfWidth {
					c = silhouette
				}
			}

			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
