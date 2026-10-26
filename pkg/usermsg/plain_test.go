package usermsg

import "testing"

func TestPlain_AuthAndIdempotent(t *testing.T) {
	got := Plain("invalid email or password")
	if got != "Email hoặc mật khẩu chưa đúng." {
		t.Fatalf("credentials: %q", got)
	}
	if again := Plain(got); again != got {
		t.Fatalf("second pass changed Vietnamese: %q", again)
	}

	// Registration already stores a Vietnamese message. Leave it alone.
	vi := "Email chưa đúng, ví dụ: ten@gmail.com"
	if Plain(vi) != vi {
		t.Fatalf("rewrote Vietnamese email message: %q", Plain(vi))
	}
}

func TestPlain_Patterns(t *testing.T) {
	if got := Plain("Too many attempts. Please wait 15 minutes and try again. Bạn đã thử quá nhiều lần. Vui lòng đợi 15 phút rồi thử lại."); got != "Bạn thử nhiều lần quá. Đợi 15 phút rồi thử lại nhé." {
		t.Fatalf("rate limit: %q", got)
	}
	if got := Plain("each image must be <= 10 MB"); got != "Mỗi ảnh tối đa 10 MB." {
		t.Fatalf("each image: %q", got)
	}
	if got := Plain("image must be <= 8 MB"); got != "Ảnh tối đa 8 MB." {
		t.Fatalf("image: %q", got)
	}
	if got := Plain("maximum 2 photos per check-in"); got != "Mỗi lần tối đa 2 ảnh." {
		t.Fatalf("photos: %q", got)
	}
	if got := Plain("invalid skin check payload: at least one image required"); got != "Thông tin check-in chưa đúng. Bạn kiểm tra lại nhé." {
		t.Fatalf("wrapped payload: %q", got)
	}
}

func TestPlain_UnknownEnglishIsPlain(t *testing.T) {
	got := Plain("pq: duplicate key value violates unique constraint")
	if got != Generic {
		t.Fatalf("jargon leaked: %q", got)
	}
	if Plain("") != "" {
		t.Fatal("empty should stay empty")
	}
}
