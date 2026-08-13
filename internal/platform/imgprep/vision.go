package imgprep

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// VisionMaxEdge keeps enough facial detail for skin analysis while cutting upload/API latency.
	VisionMaxEdge = 1280
	// VisionJPEGQuality balances file size and pore/redness cues for vision models.
	VisionJPEGQuality = 88
	// VisionPassThroughMaxBytes caps pass-through for JPEGs already within VisionMaxEdge.
	//
	// Clients already downscale + JPEG-encode before upload, so re-encoding a
	// within-limits JPEG here only adds a second generation loss — it destroys the
	// micro-texture cues (raised vs flat bumps, pore edges) the morphology rules
	// depend on while saving almost no bytes. Only absurdly large payloads at this
	// edge length are re-encoded.
	VisionPassThroughMaxBytes = 2_000_000
)

// LimitForVisionAPI downscales large photos to VisionMaxEdge and re-encodes as JPEG.
// JPEGs already within VisionMaxEdge pass through untouched to avoid a second
// lossy generation on top of the client-side compression.
func LimitForVisionAPI(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("imgprep: empty image")
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("imgprep: not an image (%s)", mime)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("imgprep: decode config: %w", err)
	}
	long := cfg.Width
	if cfg.Height > long {
		long = cfg.Height
	}
	if canPassThroughForVision(long, mime, len(data)) {
		return data, nil
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("imgprep: decode: %w", err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	maxDim := w
	if h > maxDim {
		maxDim = h
	}
	if canPassThroughForVision(maxDim, mime, len(data)) {
		return data, nil
	}

	scale := float64(VisionMaxEdge) / float64(maxDim)
	if scale > 1 {
		scale = 1
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: VisionJPEGQuality}); err != nil {
		return nil, fmt.Errorf("imgprep: encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// canPassThroughForVision reports whether a JPEG can be sent as-is (no re-encode).
func canPassThroughForVision(longEdge int, mime string, size int) bool {
	return longEdge <= VisionMaxEdge && mime == "image/jpeg" && size <= VisionPassThroughMaxBytes
}
