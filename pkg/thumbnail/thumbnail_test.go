package thumbnail

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// encodeTestImage рисует прямоугольник width×height (не обязательно квадрат —
// нам как раз важно проверить, что Generate умеет обрезать неквадратную
// картинку) и кодирует его в заданном формате, чтобы скормить Generate.
func encodeTestImage(t *testing.T, width, height int, format string) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}

	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "png":
		err = png.Encode(&buf, img)
	default:
		t.Fatalf("unsupported test format %q", format)
	}
	require.NoError(t, err)

	return buf.Bytes()
}

func TestGenerate_ProducesSquareJPEG(t *testing.T) {
	tests := []struct {
		name         string
		sourceFormat string
		width        int
		height       int
		size         int
	}{
		{name: "jpeg source, already square", sourceFormat: "jpeg", width: 200, height: 200, size: 100},
		{name: "png source, wide rectangle", sourceFormat: "png", width: 400, height: 100, size: 100},
		{name: "jpeg source, tall rectangle", sourceFormat: "jpeg", width: 120, height: 300, size: 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := encodeTestImage(t, tt.width, tt.height, tt.sourceFormat)

			out, err := Generate(bytes.NewReader(src), tt.size)
			require.NoError(t, err)
			require.NotEmpty(t, out)

			// Generate обещает JPEG на выходе независимо от формата на входе —
			// проверяем это, а заодно и итоговые размеры.
			decoded, err := jpeg.Decode(bytes.NewReader(out))
			require.NoError(t, err)

			bounds := decoded.Bounds()
			require.Equal(t, tt.size, bounds.Dx(), "unexpected width")
			require.Equal(t, tt.size, bounds.Dy(), "unexpected height")
		})
	}
}

func TestGenerate_InvalidData(t *testing.T) {
	_, err := Generate(bytes.NewReader([]byte("this is not an image")), 100)
	require.Error(t, err)
}
