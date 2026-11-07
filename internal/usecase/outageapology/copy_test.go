package outageapology

import (
	"strings"
	"testing"
)

func TestBuild_ExactCopy(t *testing.T) {
	has := Build("https://dadiary.vn/", "11111111-1111-1111-1111-111111111111")
	if has.Subject != Subject {
		t.Fatalf("subject %q", has.Subject)
	}
	if !strings.Contains(has.Text, BodyPrefix+HasResultSentence) {
		t.Fatalf("has-result body missing:\n%s", has.Text)
	}
	if !strings.Contains(has.Text, HasResultButton+": https://dadiary.vn/skin-checks/11111111-1111-1111-1111-111111111111") {
		t.Fatalf("has-result link missing:\n%s", has.Text)
	}
	if !strings.HasSuffix(strings.TrimSpace(has.Text), Signature) {
		t.Fatalf("signature missing:\n%s", has.Text)
	}
	if strings.Contains(has.Text, RecheckSentence) {
		t.Fatal("has-result mail includes the re-check sentence")
	}

	again := Build("https://dadiary.vn", "")
	if !strings.Contains(again.Text, BodyPrefix+RecheckSentence) {
		t.Fatalf("re-check body missing:\n%s", again.Text)
	}
	if !strings.Contains(again.Text, RecheckButton+": https://dadiary.vn/check-in") {
		t.Fatalf("re-check link missing:\n%s", again.Text)
	}
	if strings.Contains(again.Text, HasResultSentence) {
		t.Fatal("re-check mail includes the has-result sentence")
	}
}
