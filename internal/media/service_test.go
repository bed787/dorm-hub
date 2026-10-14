package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestImageNormalization(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2400, 100))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var input bytes.Buffer
	if err := png.Encode(&input, src); err != nil {
		t.Fatal(err)
	}
	data, w, h, err := Process(input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if w != 1920 || h != 80 {
		t.Fatalf("unexpected dimensions %dx%d", w, h)
	}
	if _, err = jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}
func TestImagesRejectInvalidData(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("<svg onload=alert(1)></svg>"), make([]byte, MaxFileSize+1)} {
		if _, _, _, err := Process(data); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}
