package lobby

import "testing"

func TestProfanityAndNickname(t *testing.T) {
	allowed := []string{
		"Mây Hồng",
		"Bánh Flan",
		"Cún Con",
		"hello",
		"classic",
		"salon",
		"melon",
		"Nguyễn",
		"Nguyễn Văn",
		"addition",
		"condition",
		"cocktail",
		"peacock",
		"Bé Da",
		"O'Brien",
	}
	for _, name := range allowed {
		if Profane(name) {
			t.Errorf("Profane(%q) = true", name)
		}
		got, ok := CleanNickname("  " + name + "  ")
		if !ok {
			t.Errorf("CleanNickname(%q) rejected", name)
			continue
		}
		if got != name && name != "  " {
			// surrounding spaces are trimmed; internal form stays
			if got != name {
				t.Errorf("CleanNickname(%q) = %q", name, got)
			}
		}
	}

	blocked := []string{
		"địt",
		"Địt",
		"d.i.t",
		"d1t",
		"đ!t",
		"lồn",
		"l0n",
		"l.o.n",
		"cặc",
		"buồi",
		"đụ má",
		"vcl",
		"ĐCM",
		"fuck",
		"F.U.C.K",
		"f*ck",
		"fuuuuck",
		"f0ck",
		"sh1t",
		"b1tch",
		"a$$hole",
		"n1gger",
		"f\u200buck",
	}
	for _, name := range blocked {
		if !Profane(name) {
			t.Errorf("Profane(%q) = false", name)
		}
		if _, ok := CleanNickname(name); ok {
			t.Errorf("CleanNickname(%q) accepted", name)
		}
	}

	if _, ok := CleanNickname(""); ok {
		t.Fatal("empty nickname accepted")
	}
	if _, ok := CleanNickname("   "); ok {
		t.Fatal("blank nickname accepted")
	}
	long := ""
	for i := 0; i < MaxNicknameRunes+1; i++ {
		long += "a"
	}
	if _, ok := CleanNickname(long); ok {
		t.Fatal("long nickname accepted")
	}
	if _, ok := CleanNickname("hello@example.com"); ok {
		t.Fatal("email-shaped nickname accepted")
	}
	if _, ok := CleanNickname("hi\nthere"); ok {
		t.Fatal("newline nickname accepted")
	}
	got, ok := CleanNickname("  mây   hồng ")
	if !ok || got != "mây hồng" {
		t.Fatalf("collapsed spaces: %q %v", got, ok)
	}
}
