package activities

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/ansg191/job-temporal/internal/builder"
)

func makeSolidPNG(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fill := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}

	return buf.Bytes()
}

func decodePNGBounds(t *testing.T, data []byte) image.Rectangle {
	t.Helper()

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}

	return img.Bounds()
}

func TestCropPNG_Basic(t *testing.T) {
	t.Parallel()

	data := makeSolidPNG(100, 100)
	out, err := CropPNG(data, image.Rect(10, 10, 50, 50))
	if err != nil {
		t.Fatalf("CropPNG returned error: %v", err)
	}

	bounds := decodePNGBounds(t, out)
	if got, want := bounds.Dx(), 40; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	if got, want := bounds.Dy(), 40; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
}

func TestCropPNG_ClampToBounds(t *testing.T) {
	t.Parallel()

	data := makeSolidPNG(100, 100)
	out, err := CropPNG(data, image.Rect(80, 80, 150, 150))
	if err != nil {
		t.Fatalf("CropPNG returned error: %v", err)
	}

	bounds := decodePNGBounds(t, out)
	if got, want := bounds.Dx(), 20; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	if got, want := bounds.Dy(), 20; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
}

func TestCropPNG_FullImage(t *testing.T) {
	t.Parallel()

	data := makeSolidPNG(100, 100)
	out, err := CropPNG(data, image.Rect(0, 0, 100, 100))
	if err != nil {
		t.Fatalf("CropPNG returned error: %v", err)
	}

	bounds := decodePNGBounds(t, out)
	if got, want := bounds.Dx(), 100; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}
	if got, want := bounds.Dy(), 100; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
}

func TestCropPNG_InvalidPNG(t *testing.T) {
	t.Parallel()

	if _, err := CropPNG([]byte("not a png"), image.Rect(0, 0, 1, 1)); err == nil {
		t.Fatal("CropPNG returned nil error for invalid PNG")
	}
}

func TestCropRectsForLabel_SinglePage(t *testing.T) {
	t.Parallel()

	bbox := builder.LabelBBox{
		Pos:  builder.LabelPosition{Page: 1, X: "72pt", Y: "185.17pt"},
		Size: builder.LabelSize{Width: "468pt", Height: "160.81pt"},
	}

	rects, err := CropRectsForLabel(bbox, 192, 3)
	if err != nil {
		t.Fatalf("CropRectsForLabel returned error: %v", err)
	}
	if got, want := len(rects), 1; got != want {
		t.Fatalf("len(rects) = %d, want %d", got, want)
	}
	if got, want := rects[0].Page, 1; got != want {
		t.Fatalf("rect page = %d, want %d", got, want)
	}
}

func TestCropRectsForLabel_MultiPage(t *testing.T) {
	t.Parallel()

	bbox := builder.LabelBBox{
		Pos:  builder.LabelPosition{Page: 1, X: "72pt", Y: "533.19pt"},
		Size: builder.LabelSize{Width: "468pt", Height: "3291.63pt"},
	}

	rects, err := CropRectsForLabel(bbox, 192, 3)
	if err != nil {
		t.Fatalf("CropRectsForLabel returned error: %v", err)
	}
	if got, want := len(rects), 3; got != want {
		t.Fatalf("len(rects) = %d, want %d", got, want)
	}
	if got, want := rects[0].Page, 1; got != want {
		t.Fatalf("first rect page = %d, want %d", got, want)
	}
	if got, want := rects[2].Page, 3; got != want {
		t.Fatalf("last rect page = %d, want %d", got, want)
	}
}

func TestCropRectsForLabel_InvalidPPI(t *testing.T) {
	t.Parallel()

	bbox := builder.LabelBBox{}
	if _, err := CropRectsForLabel(bbox, 0, 3); err == nil {
		t.Fatal("CropRectsForLabel returned nil error for invalid ppi")
	}
}
