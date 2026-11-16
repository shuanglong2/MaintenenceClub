package files

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	maxPixels   = 48_000_000
	maxEdge     = 2000
	jpegQuality = 82
)

var imageSlots = make(chan struct{}, 1)

type prepared struct {
	body []byte
	kind string
	ext  string
}

func prepareUpload(body []byte, kind, ext string) (prepared, error) {
	if !isImage(kind) {
		return prepared{body: body, kind: kind, ext: ext}, nil
	}
	cfg, format, err := imageConfig(body, kind)
	if err != nil {
		return prepared{}, ErrBadFile
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 20000 || cfg.Height > 20000 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return prepared{}, ErrPixels
	}
	if format == "gif" && cfg.Width <= maxEdge && cfg.Height <= maxEdge {
		return prepared{body: body, kind: "image/gif", ext: ".gif"}, nil
	}
	select {
	case imageSlots <- struct{}{}:
		defer func() { <-imageSlots }()
	default:
		imageSlots <- struct{}{}
		defer func() { <-imageSlots }()
	}
	img, err := decodeImage(body, format)
	if err != nil {
		return prepared{}, ErrBadFile
	}
	if format == "jpeg" {
		img = applyOrientation(img, orientation(body))
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		nw, nh := fit(width, height, maxEdge)
		dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		img = dst
	}
	var buf bytes.Buffer
	if hasAlpha(img) {
		if err := png.Encode(&buf, img); err != nil {
			return prepared{}, err
		}
		return prepared{body: buf.Bytes(), kind: "image/png", ext: ".png"}, nil
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return prepared{}, err
	}
	return prepared{body: buf.Bytes(), kind: "image/jpeg", ext: ".jpg"}, nil
}

func isImage(kind string) bool {
	switch kind {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func imageConfig(body []byte, kind string) (image.Config, string, error) {
	switch kind {
	case "image/jpeg":
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(body))
		return cfg, "jpeg", err
	case "image/png":
		cfg, err := png.DecodeConfig(bytes.NewReader(body))
		return cfg, "png", err
	case "image/gif":
		cfg, err := gif.DecodeConfig(bytes.NewReader(body))
		return cfg, "gif", err
	case "image/webp":
		cfg, err := webp.DecodeConfig(bytes.NewReader(body))
		return cfg, "webp", err
	default:
		return image.Config{}, "", ErrBadFile
	}
}

func decodeImage(body []byte, format string) (image.Image, error) {
	var (
		img image.Image
		err error
	)
	reader := bytes.NewReader(body)
	switch format {
	case "jpeg":
		img, err = jpeg.Decode(reader)
	case "png":
		img, err = png.Decode(reader)
	case "gif":
		img, err = gif.Decode(reader)
	case "webp":
		img, err = webp.Decode(reader)
	default:
		return nil, ErrBadFile
	}
	if err != nil {
		return nil, err
	}
	return img, nil
}

func fit(width, height, edge int) (int, int) {
	if width >= height {
		return edge, max(1, height*edge/width)
	}
	return max(1, width*edge/height), edge
}

func hasAlpha(img image.Image) bool {
	switch src := img.(type) {
	case *image.NRGBA:
		return channelUsed(src.Pix, 4)
	case *image.RGBA:
		return channelUsed(src.Pix, 4)
	case *image.NRGBA64:
		return channelUsed(src.Pix, 8)
	case *image.RGBA64:
		return channelUsed(src.Pix, 8)
	case *image.YCbCr, *image.Gray, *image.CMYK:
		return false
	default:
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				_, _, _, a := img.At(x, y).RGBA()
				if a != 0xFFFF {
					return true
				}
			}
		}
		return false
	}
}

func channelUsed(pix []byte, stride int) bool {
	off := stride - 1
	for ; off < len(pix); off += stride {
		if pix[off] != 255 {
			return true
		}
	}
	return false
}

func applyOrientation(img image.Image, way int) image.Image {
	switch way {
	case 2:
		return flipH(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipV(img)
	case 5:
		return transpose(img)
	case 6:
		return rotate90(img)
	case 7:
		return transverse(img)
	case 8:
		return rotate270(img)
	default:
		return img
	}
}

func rotate90(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate270(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate180(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipH(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipV(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func transpose(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func transverse(src image.Image) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func readAllLimit(r io.Reader, max int64) ([]byte, error) {
	limited := io.LimitReader(r, max+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, ErrTooBig
	}
	return body, nil
}
