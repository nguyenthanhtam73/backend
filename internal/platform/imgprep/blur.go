package imgprep

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strings"

	"golang.org/x/image/draw"
)

const (
	// ShareBlurDownscale is the intermediate size factor for a mild privacy blur
	// (downscale → upscale). ~12% keeps skin tone/shape readable without sharp identity cues.
	ShareBlurDownscale = 0.12
	// ShareBlurJPEGQuality for public share thumbnails.
	ShareBlurJPEGQuality = 78
)

// SoftBlurForShare applies a mild privacy blur suitable for public share pages.
// Implementation: decode → heavy downscale → upscale back → JPEG (no new deps).
func SoftBlurForShare(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("imgprep: empty image")
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("imgprep: not an image (%s)", mime)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("imgprep: decode: %w", err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("imgprep: invalid dimensions")
	}

	sw := int(float64(w) * ShareBlurDownscale)
	sh := int(float64(h) * ShareBlurDownscale)
	if sw < 4 {
		sw = 4
	}
	if sh < 4 {
		sh = 4
	}

	small := image.NewRGBA(image.Rect(0, 0, sw, sh))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, bounds, draw.Over, nil)

	// Soften further with a tiny 3×3 box blur on the downscaled buffer.
	boxBlurRGBA(small, 1)

	outImg := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(outImg, outImg.Bounds(), small, small.Bounds(), draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, outImg, &jpeg.Options{Quality: ShareBlurJPEGQuality}); err != nil {
		return nil, fmt.Errorf("imgprep: encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// boxBlurRGBA is a cheap separable box blur (radius in pixels) on an RGBA image.
func boxBlurRGBA(img *image.RGBA, radius int) {
	if img == nil || radius < 1 {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tmp := image.NewRGBA(b)

	// Horizontal
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a, n uint32
			for dx := -radius; dx <= radius; dx++ {
				xx := x + dx
				if xx < 0 {
					xx = 0
				} else if xx >= w {
					xx = w - 1
				}
				c := img.RGBAAt(b.Min.X+xx, b.Min.Y+y)
				r += uint32(c.R)
				g += uint32(c.G)
				bl += uint32(c.B)
				a += uint32(c.A)
				n++
			}
			tmp.SetRGBA(b.Min.X+x, b.Min.Y+y, color.RGBA{
				R: uint8(r / n),
				G: uint8(g / n),
				B: uint8(bl / n),
				A: uint8(a / n),
			})
		}
	}
	// Vertical
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a, n uint32
			for dy := -radius; dy <= radius; dy++ {
				yy := y + dy
				if yy < 0 {
					yy = 0
				} else if yy >= h {
					yy = h - 1
				}
				c := tmp.RGBAAt(b.Min.X+x, b.Min.Y+yy)
				r += uint32(c.R)
				g += uint32(c.G)
				bl += uint32(c.B)
				a += uint32(c.A)
				n++
			}
			img.SetRGBA(b.Min.X+x, b.Min.Y+y, color.RGBA{
				R: uint8(r / n),
				G: uint8(g / n),
				B: uint8(bl / n),
				A: uint8(a / n),
			})
		}
	}
}
