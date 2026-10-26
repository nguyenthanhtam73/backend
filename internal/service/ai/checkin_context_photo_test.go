package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

func TestBuildCheckInContext_CloseUpAndTouch(t *testing.T) {
	t.Parallel()
	check := &domain.SkinCheck{
		UserNote: "hôm nay",
		PhotoContext: photoContextJSON(t, []dto.PhotoMetaImage{
			{Index: 0, Kind: dto.PhotoKindCloseup, Zone: "left_cheek"},
		}, &dto.SkinContextInput{Firmness: "firm", Duration: "months", Pain: "none", Extra: "không đổi"}),
		ClimateContext: []byte(`{"ui_locale":"vi"}`),
	}
	got := BuildCheckInContext(check)
	if !strings.Contains(got, "Photo 1: close-up of left_cheek (not full face)") {
		t.Fatalf("missing close-up line:\n%s", got)
	}
	if !strings.Contains(got, "User reports: firm, months, not painful (không đổi)") {
		t.Fatalf("missing touch line:\n%s", got)
	}
	hint := BuildCheckInVisionHint(check)
	if !strings.Contains(hint, "Image 1 is a CLOSE-UP of left_cheek — only describe that zone.") {
		t.Fatalf("vision hint:\n%s", hint)
	}
	if !strings.Contains(hint, "cứng như hạt cát") || !strings.Contains(hint, "nhiều tháng") {
		t.Fatalf("vision hint missing touch answers:\n%s", hint)
	}
}
