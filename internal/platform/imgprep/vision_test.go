package imgprep

import (
	"bytes"
	"image"
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
