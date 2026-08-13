package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// The analysis JSON is served verbatim on the unauthenticated share page, so anything
// operator-only stored inside it must be stripped there. skin_context is what the user
// reported about their own skin — collected to pick the right group, never to publish.
func TestFromDomainPublicSkinReview_DropsSkinContext(t *testing.T) {
	t.Parallel()
	analysis := AdminSkinReviewAnalysis{
		Overview:    "Má của mày đang có nhiều nốt nhỏ màu da nổi cao.",
		SkinType:    "unclear",
		SkinContext: "Sờ: cứng như hạt cát. Bao lâu: nhiều tháng. Cảm giác: không đau.",
		AttentionAreas: []AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "acne", Severity: "moderate", Note: "note"},
		},
		NonDiagnostic: "không thay khám bác sĩ",
	}
	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	row := &domain.AdminSkinReview{
		ID:          uuid.New(),
		PublicSlug:  "abc123",
		IsPublic:    true,
		Locale:      "vi",
		Analysis:    raw,
		SkinContext: "Sờ: cứng như hạt cát.",
	}

	pub := FromDomainPublicSkinReview(row, []string{"/uploads/x.jpg"})
	if pub.Analysis.SkinContext != "" {
		t.Fatalf("skin_context must not reach the public share payload, got %q", pub.Analysis.SkinContext)
	}
	out, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "hạt cát") {
		t.Fatalf("user-reported context leaked into the public JSON: %s", out)
	}
	// The observation itself must still be published.
	if !strings.Contains(pub.Analysis.Overview, "nốt nhỏ") {
		t.Fatalf("public analysis lost its overview: %q", pub.Analysis.Overview)
	}
}

// The admin console needs the context to stay visible — it explains the read.
func TestFromDomainAdminSkinReview_KeepsSkinContext(t *testing.T) {
	t.Parallel()
	row := &domain.AdminSkinReview{
		ID:          uuid.New(),
		Locale:      "vi",
		Analysis:    json.RawMessage(`{"overview":"x","attention_areas":[]}`),
		SkinContext: "Sờ: mềm, có cuống.",
	}
	got := FromDomainAdminSkinReview(row, nil)
	if got.SkinContext != "Sờ: mềm, có cuống." {
		t.Fatalf("admin payload should keep skin_context, got %q", got.SkinContext)
	}
}
