package ai

import "testing"

func TestSanitizeShareTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{name: "accepts a clean question", in: "Da nám thì phác đồ điều trị như nào?", want: "Da nám thì phác đồ điều trị như nào?", wantOK: true},
		{name: "strips quotes", in: `"Liệu trình tái tạo da có hiệu quả thật không?"`, want: "Liệu trình tái tạo da có hiệu quả thật không?", wantOK: true},
		{name: "rejects brand", in: "DaDiary chữa nám hiệu quả", wantOK: false},
		{name: "rejects promise", in: "Cách chữa khỏi mụn tại nhà", wantOK: false},
		{name: "rejects too short", in: "Bị gì?", wantOK: false},
	}
	for _, c := range cases {
		got, ok := sanitizeShareTitle(c.in)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v want %v (got %q)", c.name, ok, c.wantOK, got)
			continue
		}
		if c.wantOK && got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeShareTitle_ClipsLength(t *testing.T) {
	t.Parallel()
	long := "Da mặt nổi rất nhiều nốt nhỏ màu da suốt mấy tháng nay không hết thì phải làm sao đây mọi người"
	got, ok := sanitizeShareTitle(long)
	if !ok {
		t.Fatal("expected a clipped title")
	}
	if n := len([]rune(got)); n > maxShareTitleRunes {
		t.Fatalf("title too long (%d): %q", n, got)
	}
}
