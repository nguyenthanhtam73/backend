package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand"
	"os"
)

func main() {
	const W, H = 800, 1000
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	rng := rand.New(rand.NewSource(42))

	// Skin base (warm medium tone) with mild cheek/nose/forehead zones.
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			r := 210 + (x+y)%12
			g := 155 + (x*3+y)%10
			b := 130 + (y*2+x)%8
			// Mild T-zone shine cue
			if x > 320 && x < 480 && y > 180 && y < 520 {
				r += 12
				g += 8
				b += 4
			}
			// Cheek redness wash (left cheek)
			if dist(x, y, 240, 520) < 140 {
				r += 18
				g -= 8
				b -= 4
			}
			img.Set(x, y, color.RGBA{uint8(clamp(r)), uint8(clamp(g)), uint8(clamp(b)), 255})
		}
	}

	// Dark spots / PIH on cheeks
	darkSpots := [][3]int{{220, 480, 10}, {260, 560, 8}, {500, 500, 9}, {545, 540, 7}}
	for _, s := range darkSpots {
		blob(img, s[0], s[1], s[2], color.RGBA{150, 95, 70, 255}, 0.85)
	}

	// Inflamed acne papules (red) — dense cluster on left cheek + chin + forehead
	papules := [][3]int{
		{230, 500, 14}, {255, 530, 11}, {210, 545, 9}, {275, 505, 10},
		{245, 575, 12}, {290, 555, 8},
		{390, 720, 13}, {420, 745, 10}, {360, 740, 9},
		{350, 220, 10}, {380, 250, 8}, {410, 230, 9},
		{450, 480, 11}, {470, 510, 9},
	}
	for _, p := range papules {
		// red inflamed halo
		blob(img, p[0], p[1], p[2]+4, color.RGBA{210, 90, 85, 255}, 0.45)
		// papule center
		blob(img, p[0], p[1], p[2], color.RGBA{190, 60, 55, 255}, 0.9)
		// whitehead tip on some
		if rng.Intn(3) == 0 {
			blob(img, p[0]-1, p[1]-1, max(2, p[2]/4), color.RGBA{245, 230, 220, 255}, 0.95)
		}
	}

	// Pore-like dots on nose
	for i := 0; i < 40; i++ {
		x := 360 + rng.Intn(80)
		y := 400 + rng.Intn(120)
		img.Set(x, y, color.RGBA{120, 90, 70, 255})
	}

	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 92}); err != nil {
		panic(err)
	}
}

func dist(x, y, cx, cy int) float64 {
	dx := float64(x - cx)
	dy := float64(y - cy)
	return math.Hypot(dx, dy)
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func blob(img *image.RGBA, cx, cy, rad int, c color.RGBA, alpha float64) {
	for y := cy - rad; y <= cy+rad; y++ {
		for x := cx - rad; x <= cx+rad; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			d := dist(x, y, cx, cy)
			if d > float64(rad) {
				continue
			}
			fall := 1 - d/float64(rad)
			a := alpha * fall
			r0, g0, b0, _ := img.At(x, y).RGBA()
			nr := uint8((1-a)*float64(r0>>8) + a*float64(c.R))
			ng := uint8((1-a)*float64(g0>>8) + a*float64(c.G))
			nb := uint8((1-a)*float64(b0>>8) + a*float64(c.B))
			img.Set(x, y, color.RGBA{nr, ng, nb, 255})
		}
	}
}
