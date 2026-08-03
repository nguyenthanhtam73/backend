package main
import (
  "image"
  "image/color"
  "image/jpeg"
  "os"
)
func main() {
  // Synthetic skin-tone photo for vision smoke (not a real face).
  img := image.NewRGBA(image.Rect(0, 0, 640, 800))
  for y := 0; y < 800; y++ {
    for x := 0; x < 640; x++ {
      // Warm skin base + mild regional variation
      r := uint8(210 + (x%40)/4)
      g := uint8(160 + (y%30)/3)
      b := uint8(140 + ((x+y)%20)/2)
      if x > 220 && x < 420 && y > 180 && y < 320 {
        // Forehead slightly oilier / shinier
        r = 225; g = 175; b = 150
      }
      if x > 180 && x < 280 && y > 380 && y < 520 {
        // Cheek redness cue
        r = 220; g = 140; b = 135
      }
      if x > 300 && x < 360 && y > 340 && y < 480 {
        // Nose T-zone
        r = 230; g = 180; b = 145
      }
      img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
    }
  }
  f, err := os.Create(os.Args[1])
  if err != nil { panic(err) }
  defer f.Close()
  if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil { panic(err) }
}
