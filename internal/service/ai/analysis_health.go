package ai

import (
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/dadiary/backend/internal/platform/httpx"
)

const (
	// LogAnalysisDown is the greppable line emitted once when skin-check or
	// starter-routine calls start failing as an outage.
	LogAnalysisDown = "AI_ANALYSIS_DOWN"
	// LogAnalysisRecovered is the greppable line emitted once the next call
	// succeeds after LogAnalysisDown.
	LogAnalysisRecovered = "AI_ANALYSIS_RECOVERED"

	// analysisDownThreshold is consecutive failures that announce an outage
	// even when the error is not a credit or quota failure.
	analysisDownThreshold = 3
)

// analysisHealth is the process-local streak of skin-check and starter-routine
// provider failures. It resets on process restart.
type analysisHealth struct {
	mu          sync.Mutex
	consecutive int
	down        bool
	announced   bool
}

var health analysisHealth

// NoteAnalysisFailure records one failed skin-check analysis or starter-routine
// generation. A credit/quota error, or analysisDownThreshold failures in a row,
// logs AI_ANALYSIS_DOWN once until a later success.
func NoteAnalysisFailure(err error) {
	credit := IsCreditOrQuotaError(err)
	health.mu.Lock()
	defer health.mu.Unlock()
	health.consecutive++
	if health.announced {
		return
	}
	if !credit && health.consecutive < analysisDownThreshold {
		return
	}
	health.down = true
	health.announced = true
	slog.Error(LogAnalysisDown,
		"consecutive", health.consecutive,
		"credit_or_quota", credit,
	)
}

// NoteAnalysisSuccess records one completed skin-check analysis or starter
// routine. If an outage was announced, it logs AI_ANALYSIS_RECOVERED once.
func NoteAnalysisSuccess() {
	health.mu.Lock()
	defer health.mu.Unlock()
	wasDown := health.announced || health.down
	health.consecutive = 0
	health.down = false
	health.announced = false
	if !wasDown {
		return
	}
	slog.Info(LogAnalysisRecovered)
}

// AnalysisDown reports whether the in-process tracker has announced an outage
// that has not yet recovered. Check-in responses use this to set analysis_pending.
func AnalysisDown() bool {
	health.mu.Lock()
	defer health.mu.Unlock()
	return health.down
}

func resetAnalysisHealth() {
	health.mu.Lock()
	defer health.mu.Unlock()
	health.consecutive = 0
	health.down = false
	health.announced = false
}

// IsCreditOrQuotaError reports provider credit-balance and hard-quota failures
// (Anthropic "credit balance is too low", OpenAI credit_balance_exhausted /
// insufficient_quota). Retrying those does not help.
func IsCreditOrQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return IsCreditOrQuotaText(err.Error())
}

// IsCreditOrQuotaText is the string form of IsCreditOrQuotaError.
func IsCreditOrQuotaText(msg string) bool {
	if httpx.CreditExhaustedBody(msg) {
		return true
	}
	low := strings.ToLower(msg)
	return strings.Contains(low, "out of credits") || strings.Contains(low, "billing_not_active")
}

// IsProviderFailure reports an error from the model provider (HTTP status,
// credit, rate limit, circuit open). Local persistence errors are not included,
// so a replay run only stops on provider trouble.
func IsProviderFailure(err error) bool {
	if err == nil {
		return false
	}
	if IsCreditOrQuotaError(err) || httpx.IsRetryable(err) {
		return true
	}
	var se *httpx.StatusError
	if errors.As(err, &se) {
		return true
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "text coach"):
		return true
	case strings.Contains(low, "circuit breaker"):
		return true
	case strings.Contains(low, "anthropic"):
		return true
	case strings.Contains(low, "openai"):
		return true
	case strings.Contains(low, "http 4"), strings.Contains(low, "http 5"):
		return true
	default:
		return false
	}
}
