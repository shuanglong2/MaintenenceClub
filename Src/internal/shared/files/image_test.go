package files

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestOrientationRotate(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	src.Set(1, 0, color.RGBA{B: 255, A: 255})
	turned := rotate90(src)
	if turned.Bounds().Dx() != 1 || turned.Bounds().Dy() != 2 {
		t.Fatalf("%v", turned.Bounds())
	}
	r, _, _, _ := turned.At(0, 0).RGBA()
	_, _, b, _ := turned.At(0, 1).RGBA()
	if r>>8 != 255 || b>>8 != 255 {
		t.Fatal("方向不对")
	}
}

func TestReadOrientation(t *testing.T) {
	// JPEG APP1, little-endian TIFF, orientation 6.
	payload := []byte("Exif\x00\x00II*\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00\x00\x00\x00\x00\x00\x00")
	size := len(payload) + 2
	buf := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte(size >> 8), byte(size)}
	buf = append(buf, payload...)
	buf = append(buf, 0xFF, 0xD9)
	if got := orientation(buf); got != 6 {
		t.Fatalf("方向 %d", got)
	}
}

func TestPrepareShrinks(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2100, 20))
	for x := 0; x < 2100; x++ {
		src.Set(x, 0, color.RGBA{R: uint8(x), G: 20, B: 40, A: 255})
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	out, err := prepareUpload(buf.Bytes(), "image/jpeg", ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out.body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > maxEdge || cfg.Height > maxEdge {
		t.Fatalf("%d x %d", cfg.Width, cfg.Height)
	}
}

func TestPrepareKeepsPNGAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.NRGBA{R: 255, A: 10})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	out, err := prepareUpload(buf.Bytes(), "image/png", ".png")
	if err != nil {
		t.Fatal(err)
	}
	if out.kind != "image/png" {
		t.Fatal(out.kind)
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	if _, err := SafeJoin("/data", "../etc/passwd"); err == nil {
		t.Fatal("应拒绝穿越")
	}
	if _, err := SafeJoin("/data", `docs\..\secret`); err == nil {
		t.Fatal("应拒绝反斜杠")
	}
	got, err := SafeJoin("/data", "docs/2026/11/abc.jpg")
	if err != nil || got == "" {
		t.Fatal(err)
	}
}

func TestRejectMismatchedType(t *testing.T) {
	if _, ok := allowedExt("photo.png", "image/jpeg"); ok {
		t.Fatal("扩展名与内容不一致")
	}
	if _, ok := allowedExt("note.exe", "text/plain"); ok {
		t.Fatal("不应接收 exe")
	}
}
