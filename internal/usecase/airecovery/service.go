package airecovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/service/analysis"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	"github.com/google/uuid"
)

const (
	defaultConcurrency = 2
	maxConcurrency     = 3
	defaultStopAfter   = 5
	// DefaultPause is the gap between starting provider calls on a real run.
	DefaultPause = 2 * time.Second
)

// Outcome is the result of one replay attempt.
type Outcome int

const (
	OutcomeDone Outcome = iota
	OutcomeSkipped
	OutcomeFailed
	OutcomeProviderError
)

// Counts is one population (photo check-ins or starter routines).
type Counts struct {
	Found       int `json:"found"`
	Done        int `json:"done"`
	StillFailed int `json:"still_failed"`
	Skipped     int `json:"skipped"`
}

// Result is the operator summary. Totals are checks plus starters.
// When Stopped is true, found - done - still_failed - skipped is the number
// of candidates left unattempted so the run would not keep calling a down provider.
type Result struct {
	DryRun      bool   `json:"dry_run"`
	Since       string `json:"since"`
	Stopped     bool   `json:"stopped"`
	Found       int    `json:"found"`
	Done        int    `json:"done"`
	StillFailed int    `json:"still_failed"`
	Skipped     int    `json:"skipped"`
	Checks      Counts `json:"checks"`
	Starters    Counts `json:"starters"`
}

// Options controls one replay pass. Apply false is a dry-run that only counts.
type Options struct {
	Since       time.Time
	Apply       bool
	Concurrency int
	Pause       time.Duration
	StopAfter   int
	// Limit caps each population (checks and starters). Zero means no cap.
	Limit int
	Now   time.Time
}

// ErrInvalidOptions is a caller mistake (bad since, negative limit).
var ErrInvalidOptions = errors.New("invalid replay options")

type checkProcessor interface {
	Process(ctx context.Context, skinCheckID uuid.UUID, deviceType string) error
}

type starterReplayer interface {
	ReplayMissingStarter(ctx context.Context, userID uuid.UUID) error
}

// Service lists failed work and replays it oldest first.
type Service struct {
	checks    *repository.GormSkinCheckRepository
	profiles  *repository.GormSkinProfileRepository
	processor checkProcessor
	starters  starterReplayer

	listChecks    func(ctx context.Context, since, now time.Time, limit int) ([]uuid.UUID, error)
	replayCheck   func(ctx context.Context, id uuid.UUID) (Outcome, error)
	listStarters  func(ctx context.Context, since, now time.Time, limit int) ([]uuid.UUID, error)
	replayStarter func(ctx context.Context, id uuid.UUID) (Outcome, error)
	sleep         func(time.Duration)
}

// NewService wires the production repositories and the shared analysis pipeline.
// processor and starters may be nil for a dry-run.
func NewService(
	checks *repository.GormSkinCheckRepository,
	profiles *repository.GormSkinProfileRepository,
	processor checkProcessor,
	starters starterReplayer,
) *Service {
	s := &Service{
		checks:    checks,
		profiles:  profiles,
		processor: processor,
		starters:  starters,
		sleep:     time.Sleep,
	}
	s.listChecks = s.defaultListChecks
	s.replayCheck = s.defaultReplayCheck
	s.listStarters = s.defaultListStarters
	s.replayStarter = s.defaultReplayStarter
	return s
}

// Run counts candidates and, when Apply is set, replays them oldest first.
func (s *Service) Run(ctx context.Context, opt Options) (Result, error) {
	var zero Result
	if s == nil {
		return zero, fmt.Errorf("%w: service unavailable", ErrInvalidOptions)
	}
	opt, err := normalize(opt)
	if err != nil {
		return zero, err
	}
	if opt.Apply && (s.replayCheck == nil || s.replayStarter == nil) {
		return zero, fmt.Errorf("%w: replay is not configured", ErrInvalidOptions)
	}

	checks, err := s.listChecks(ctx, opt.Since, opt.Now, opt.Limit)
	if err != nil {
		return zero, err
	}
	starters, err := s.listStarters(ctx, opt.Since, opt.Now, opt.Limit)
	if err != nil {
		return zero, err
	}

	res := Result{
		DryRun:   !opt.Apply,
		Since:    opt.Since.UTC().Format(time.RFC3339),
		Checks:   Counts{Found: len(checks)},
		Starters: Counts{Found: len(starters)},
	}
	if !opt.Apply {
		fold(&res)
		slog.Info("ai-replay: dry-run",
			"found", res.Found,
			"checks", res.Checks.Found,
			"starters", res.Starters.Found,
		)
		return res, nil
	}

	gate := &stopGate{max: opt.StopAfter}
	res.Checks = s.runQueue(ctx, checks, opt, gate, s.replayCheck)
	res.Checks.Found = len(checks)
	if !gate.stopped() {
		res.Starters = s.runQueue(ctx, starters, opt, gate, s.replayStarter)
	}
	res.Starters.Found = len(starters)
	res.Stopped = gate.stopped()
	fold(&res)
	slog.Info("ai-replay: finished",
		"found", res.Found,
		"done", res.Done,
		"still_failed", res.StillFailed,
		"skipped", res.Skipped,
		"stopped", res.Stopped,
	)
	return res, nil
}

func fold(res *Result) {
	res.Found = res.Checks.Found + res.Starters.Found
	res.Done = res.Checks.Done + res.Starters.Done
	res.StillFailed = res.Checks.StillFailed + res.Starters.StillFailed
	res.Skipped = res.Checks.Skipped + res.Starters.Skipped
}

func normalize(opt Options) (Options, error) {
	if opt.Since.IsZero() {
		opt.Since = domain.AIAnalysisOutageSince
	}
	opt.Since = opt.Since.UTC()
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	if opt.Limit < 0 {
		return opt, fmt.Errorf("%w: limit must be >= 0", ErrInvalidOptions)
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = defaultConcurrency
	}
	if opt.Concurrency > maxConcurrency {
		opt.Concurrency = maxConcurrency
	}
	if opt.StopAfter <= 0 {
		opt.StopAfter = defaultStopAfter
	}
	if opt.Pause < 0 {
		opt.Pause = 0
	}
	return opt, nil
}

func (s *Service) defaultListChecks(ctx context.Context, since, now time.Time, limit int) ([]uuid.UUID, error) {
	if s.checks == nil {
		return nil, fmt.Errorf("%w: skin checks unavailable", ErrInvalidOptions)
	}
	rows, err := s.checks.ListCreatedSince(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0)
	for _, row := range rows {
		snap := CheckSnapshot{
			ID:        row.ID,
			CreatedAt: row.CreatedAt,
			ImageURLs: row.ImageURLs,
		}
		if row.Analysis != nil {
			snap.HasAnalysis = true
			snap.Status = row.Analysis.Status
			snap.AnalysisUpdatedAt = row.Analysis.UpdatedAt
		}
		if !CheckNeedsReplay(snap, since, now) {
			continue
		}
		out = append(out, row.ID)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Service) defaultReplayCheck(ctx context.Context, id uuid.UUID) (Outcome, error) {
	if s.checks == nil || s.processor == nil {
		return OutcomeFailed, fmt.Errorf("%w: analysis replay unavailable", ErrInvalidOptions)
	}
	row, err := s.checks.GetByID(ctx, id)
	if err != nil {
		return OutcomeFailed, err
	}
	if row == nil {
		return OutcomeSkipped, nil
	}
	snap := CheckSnapshot{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		ImageURLs: row.ImageURLs,
	}
	if row.Analysis != nil {
		snap.HasAnalysis = true
		snap.Status = row.Analysis.Status
		snap.AnalysisUpdatedAt = row.Analysis.UpdatedAt
	}
	if !CheckNeedsReplay(snap, row.CreatedAt, time.Now().UTC()) {
		return OutcomeSkipped, nil
	}
	// Missing analysis rows are created inside Process, then ClaimAnalysisRun
	// (the same UPDATE reanalyze's handoff uses) so a second process cannot
	// start the model.
	err = s.processor.Process(ctx, id, "")
	if err == nil {
		return OutcomeDone, nil
	}
	if errors.Is(err, analysis.ErrAnalysisBusy) {
		return OutcomeSkipped, nil
	}
	if ai.IsProviderFailure(err) {
		slog.Warn("ai-replay: check provider error", "credit_or_quota", ai.IsCreditOrQuotaError(err))
		return OutcomeProviderError, err
	}
	slog.Warn("ai-replay: check failed")
	return OutcomeFailed, err
}

func (s *Service) defaultListStarters(ctx context.Context, since, now time.Time, limit int) ([]uuid.UUID, error) {
	if s.profiles == nil {
		return nil, fmt.Errorf("%w: profiles unavailable", ErrInvalidOptions)
	}
	rows, err := s.profiles.ListTouchedSince(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0)
	for _, row := range rows {
		if !profileuc.StarterReplayCandidate(row.OnboardingSnapshot, row.UpdatedAt, now) {
			continue
		}
		out = append(out, row.UserID)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Service) defaultReplayStarter(ctx context.Context, userID uuid.UUID) (Outcome, error) {
	if s.starters == nil {
		return OutcomeFailed, fmt.Errorf("%w: starter replay unavailable", ErrInvalidOptions)
	}
	err := s.starters.ReplayMissingStarter(ctx, userID)
	if err == nil {
		return OutcomeDone, nil
	}
	if errors.Is(err, profileuc.ErrStarterPresent) || errors.Is(err, profileuc.ErrStarterBusy) {
		return OutcomeSkipped, nil
	}
	if ai.IsProviderFailure(err) {
		slog.Warn("ai-replay: starter provider error", "credit_or_quota", ai.IsCreditOrQuotaError(err))
		return OutcomeProviderError, err
	}
	slog.Warn("ai-replay: starter failed")
	return OutcomeFailed, err
}

type stopGate struct {
	mu          sync.Mutex
	consecutive int
	max         int
	stop        bool
}

func (g *stopGate) stopped() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stop
}

func (g *stopGate) observe(outcome Outcome) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch outcome {
	case OutcomeProviderError:
		g.consecutive++
		if g.consecutive >= g.max {
			g.stop = true
		}
	default:
		// A success, a skip, or a local failure ends the provider streak.
		g.consecutive = 0
	}
}

func (s *Service) runQueue(
	ctx context.Context,
	ids []uuid.UUID,
	opt Options,
	gate *stopGate,
	replay func(context.Context, uuid.UUID) (Outcome, error),
) Counts {
	var counts Counts
	if len(ids) == 0 {
		return counts
	}
	conc := opt.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, id := range ids {
		if ctx.Err() != nil || gate.stopped() {
			break
		}
		if i > 0 && opt.Pause > 0 && s.sleep != nil {
			s.sleep(opt.Pause)
		}
		if ctx.Err() != nil || gate.stopped() {
			break
		}
		sem <- struct{}{}
		if gate.stopped() {
			<-sem
			break
		}
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			// Shorter than the analysis lease (4m30s) and the starter lease (5m).
			itemCtx, cancel := context.WithTimeout(ctx, analysis.ProcessBudget)
			defer cancel()
			outcome, _ := replay(itemCtx, id)
			gate.observe(outcome)
			mu.Lock()
			switch outcome {
			case OutcomeDone:
				counts.Done++
			case OutcomeSkipped:
				counts.Skipped++
			default:
				counts.StillFailed++
			}
			mu.Unlock()
			<-sem
		}(id)
	}
	wg.Wait()
	return counts
}
