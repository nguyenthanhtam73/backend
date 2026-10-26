package dto

import (
	"fmt"
	"time"
)

// FormatUserMemorySummary is one line on the "DaDiary nhớ gì về bạn" screen.
// A check-in on 22 October with one photo in the current year is
// "Ngày 22/10 · 1 ảnh". An older year keeps the year so the line stays clear.
func FormatUserMemorySummary(checkDate time.Time, photoCount int, now time.Time) string {
	d := checkDate.UTC()
	photos := "không có ảnh"
	switch {
	case photoCount == 1:
		photos = "1 ảnh"
	case photoCount > 1:
		photos = fmt.Sprintf("%d ảnh", photoCount)
	}
	if d.Year() != now.In(vietnamLocation()).Year() {
		return fmt.Sprintf("Ngày %d/%d/%d · %s", d.Day(), int(d.Month()), d.Year(), photos)
	}
	return fmt.Sprintf("Ngày %d/%d · %s", d.Day(), int(d.Month()), photos)
}

func vietnamLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		return time.FixedZone("ICT", 7*3600)
	}
	return loc
}
