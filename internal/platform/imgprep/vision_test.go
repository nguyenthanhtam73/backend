package imgprep

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestLimitForVisionAPI_smallJPEGUnchanged(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 600))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	in := buf.Bytes()
	out, err := LimitForVisionAPI(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, out) {
		t.Fatal("expected small jpeg to pass through unchanged")
	}
}

// Clients already downscale + JPEG-encode before upload. Re-encoding that JPEG here
// would add a second generation loss and wipe the micro-texture cues (raised vs flat
// bumps) the morphology rules read, so within-limits JPEGs must pass through byte-for-byte
// even when they are chunky.
func TestLimitForVisionAPI_noSecondGenerationLoss(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, VisionMaxEdge, 960))
	// Noise keeps the encoder from compressing a flat image down to a few KB.
	for y := 0; y < 960; y++ {
		for x := 0; x < VisionMaxEdge; x++ {
			v := uint8((x*7 + y*13) % 256)
			src.Set(x, y, color.RGBA{R: v, G: uint8((x * 3) % 256), B: uint8((y * 5) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	in := buf.Bytes()
	if len(in) <= 700_000 {
		t.Skipf("encoder produced %d bytes; test needs a payload above the old 700KB cutoff", len(in))
	}
	out, err := LimitForVisionAPI(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, out) {
		t.Fatalf("within-limits jpeg was re-encoded (%d → %d bytes) — that is the generation loss we removed", len(in), len(out))
	}
}

func TestLimitForVisionAPI_downscalesLarge(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4032, 3024))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	out, err := LimitForVisionAPI(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) >= len(buf.Bytes()) {
		t.Fatalf("expected smaller output, got %d vs %d", len(out), len(buf.Bytes()))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	long := cfg.Width
	if cfg.Height > long {
		long = cfg.Height
	}
	if long > VisionMaxEdge {
		t.Fatalf("long edge %d exceeds %d", long, VisionMaxEdge)
	}
}
