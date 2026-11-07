package analysis

import (
	"fmt"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/pkg/usermsg"
)

func TestStoredAnalysisError_CreditAndOrdinary(t *testing.T) {
	ai.NoteAnalysisSuccess()
	t.Cleanup(func() {
		// Leave the process-local tracker healthy for any later test in this package.
		for ai.AnalysisDown() {
			ai.NoteAnalysisSuccess()
		}
	})

	credit := `anthropic messages http 400: Your credit balance is too low to access the Anthropic API`
	if got := storedAnalysisError(credit); got != domain.DeferredAnalysisMessage {
		t.Fatalf("credit error stored %q", got)
	}
	ordinary := "could not save completed analysis"
	if got := storedAnalysisError(ordinary); got != usermsg.Plain(ordinary) {
		t.Fatalf("ordinary error stored %q", got)
	}
	ai.NoteAnalysisFailure(fmt.Errorf("%s", credit))
	if !ai.AnalysisDown() {
		t.Fatal("credit failure should mark the pipeline down")
	}
	if got := storedAnalysisError("text coach (skin-check): openai http 503"); got != domain.DeferredAnalysisMessage {
		t.Fatalf("provider error while down stored %q", got)
	}
}
