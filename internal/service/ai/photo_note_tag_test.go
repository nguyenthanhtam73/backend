package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhotoNoteTag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		lim, unc, want string
	}{
		{"", "", photoNoteTagNone},
		{"   ", "  ", photoNoteTagNone},
		{"ảnh hơi mờ, thiếu sáng", "", photoNoteTagBlur},
		{"BLURRY", "", photoNoteTagBlur},
		{"mo", "", photoNoteTagBlur},
		{"", "mo", photoNoteTagOther},
		{"thiếu sáng", "hơi mờ", photoNoteTagBlur},
		{"tối quá", "", photoNoteTagDark},
		{"toi", "", photoNoteTagDark},
		{"", "toi", photoNoteTagOther},
		{"thiếu sáng", "", photoNoteTagDark},
		{"too dark", "", photoNoteTagDark},
		{"bad lighting", "", photoNoteTagDark},
		{"low light near the cheek", "", photoNoteTagDark},
		{"overexposed", "", photoNoteTagDark},
		{"hard to see in low light", "", photoNoteTagDark},
		{"crop sát", "", photoNoteTagCrop},
		{"bị cắt mất má", "", photoNoteTagCrop},
		{"cannot read, image is cropped", "", photoNoteTagCrop},
		{"shadow on the cheek", "", photoNoteTagShadow},
		{"bóng che nửa má", "", photoNoteTagShadow},
		{"shadow but blurry", "", photoNoteTagBlur},
		{"khó đọc nốt", "", photoNoteTagUnreadable},
		{"hard to read", "", photoNoteTagUnreadable},
		{"cannot read the bump", "", photoNoteTagUnreadable},
		{"không đọc được", "", photoNoteTagUnreadable},
		{"góc chụp hẹp", "", photoNoteTagOther},
		{"user@secret.example https://cdn.example/uploads/checks/face.jpg", "", photoNoteTagOther},
		{"MỜ", "", photoNoteTagBlur},
		{"Cắt khung", "", photoNoteTagCrop},
	}
	for _, tc := range cases {
		if got := PhotoNoteTag(tc.lim, tc.unc); got != tc.want {
			t.Fatalf("PhotoNoteTag(%q, %q)=%q want %q", tc.lim, tc.unc, got, tc.want)
		}
		for _, leaked := range []string{"@", "http://", "https://", "uploads/", ".jpg", "user@secret.example"} {
			if strings.Contains(PhotoNoteTag(tc.lim, tc.unc), leaked) {
				t.Fatalf("tag echoed %q from %q / %q", leaked, tc.lim, tc.unc)
			}
		}
	}
}

func TestPhotoNoteTagFromVision(t *testing.T) {
	t.Parallel()
	blur := visionWithNotes(t, "ảnh hơi mờ user@dadiary.vn https://cdn.example/a.jpg", "see https://secret.example/token")
	if got := PhotoNoteTagFromVision(blur); got != photoNoteTagBlur {
		t.Fatalf("limitations blur: %s", got)
	}
	dark := visionWithNotes(t, "", "thiếu sáng")
	if got := PhotoNoteTagFromVision(dark); got != photoNoteTagDark {
		t.Fatalf("uncertainty only: %s", got)
	}
	// Keywords on other fields are not a photo note.
	otherFields := `{"photo_assessment":{"lighting":"dark shadow crop blur low light","angle_clarity":"ok","limitations":""},"visible_observations":["má hơi mờ user@x.com"],"uncertainty_note":""}`
	if got := PhotoNoteTagFromVision(otherFields); got != photoNoteTagNone {
		t.Fatalf("non-note fields tagged %s", got)
	}
	if got := PhotoNoteTagFromVision("ảnh mờ user@x.com https://cdn.example/a.jpg"); got != photoNoteTagNone {
		t.Fatalf("non-json tagged %s", got)
	}
	if got := PhotoNoteTagFromVision(""); got != photoNoteTagNone {
		t.Fatalf("empty tagged %s", got)
	}
	if got := PhotoNoteTagFromVision("{"); got != photoNoteTagNone {
		t.Fatalf("broken json tagged %s", got)
	}
}

func TestPhotoNoteTag_AgreesWithPhotoLimited(t *testing.T) {
	t.Parallel()
	for _, needle := range photoNoteKeywordNeedles() {
		raw := visionWithNotes(t, "xx "+needle+" yy", "")
		limited, note := CheckInVisionPhotoLimited(raw)
		tag := PhotoNoteTagFromVision(raw)
		if !limited || note == "" {
			t.Fatalf("needle %q did not limit the photo", needle)
		}
		switch tag {
		case photoNoteTagBlur, photoNoteTagDark, photoNoteTagCrop, photoNoteTagShadow, photoNoteTagUnreadable:
		default:
			t.Fatalf("needle %q tag=%s", needle, tag)
		}
	}
	plain := visionWithNotes(t, "góc chụp hẹp", "chỉ thấy một vùng")
	if limited, _ := CheckInVisionPhotoLimited(plain); limited {
		t.Fatal("non-keyword note was limited")
	}
	if got := PhotoNoteTagFromVision(plain); got != photoNoteTagOther {
		t.Fatalf("non-keyword tag=%s", got)
	}
	clean := visionWithNotes(t, "", "")
	if limited, _ := CheckInVisionPhotoLimited(clean); limited {
		t.Fatal("empty note was limited")
	}
	if got := PhotoNoteTagFromVision(clean); got != photoNoteTagNone {
		t.Fatalf("empty tag=%s", got)
	}
}

func TestPhotoNoteKeywordNeedles_UnchangedSet(t *testing.T) {
	t.Parallel()
	want := []string{
		"mờ", "mo ", "tối", "toi ", "thiếu sáng", "crop", "cắt", "blur", "dark", "lighting",
		"shadow", "bóng", "khó đọc", "hard to", "cannot", "không đọc", "low light", "overexpos",
	}
	got := photoNoteKeywordNeedles()
	if len(got) != len(want) {
		t.Fatalf("needles %q", got)
	}
	seen := map[string]struct{}{}
	for _, n := range got {
		seen[n] = struct{}{}
	}
	for _, n := range want {
		if _, ok := seen[n]; !ok {
			t.Fatalf("missing needle %q in %q", n, got)
		}
	}
}

func visionWithNotes(t *testing.T, limitations, uncertainty string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"photo_assessment": map[string]string{
			"lighting":      "ok",
			"angle_clarity": "straight on",
			"limitations":   limitations,
		},
		"visible_observations": []string{"má: nốt user@secret.example https://cdn.example/uploads/checks/face.jpg"},
		"uncertainty_note":     uncertainty,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
