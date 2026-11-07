package airecovery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestRun_DryRunCountsWithoutCallingProviders(t *testing.T) {
	checks := []uuid.UUID{uuid.New(), uuid.New()}
	starters := []uuid.UUID{uuid.New()}
	var calls int
	svc := &Service{
		sleep: func(time.Duration) { t.Fatal("dry-run should not pause") },
		listChecks: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return checks, nil
		},
		listStarters: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return starters, nil
		},
		replayCheck: func(context.Context, uuid.UUID) (Outcome, error) {
			calls++
			return OutcomeDone, nil
		},
		replayStarter: func(context.Context, uuid.UUID) (Outcome, error) {
			calls++
			return OutcomeDone, nil
		},
	}
	res, err := svc.Run(context.Background(), Options{Apply: false, Since: domain.AIAnalysisOutageSince})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("dry-run called providers %d times", calls)
	}
	if !res.DryRun || res.Found != 3 || res.Done != 0 || res.StillFailed != 0 || res.Skipped != 0 || res.Stopped {
		t.Fatalf("dry-run counts = %+v", res)
	}
	if res.Checks.Found != 2 || res.Starters.Found != 1 {
		t.Fatalf("split counts = checks %+v starters %+v", res.Checks, res.Starters)
	}
}

func TestRun_StopAfterConsecutiveProviderErrors(t *testing.T) {
	ids := make([]uuid.UUID, 8)
	for i := range ids {
		ids[i] = uuid.New()
	}
	var mu sync.Mutex
	var called []uuid.UUID
	providerErr := errors.New(`text coach (skin-check): openai http 429: {"error":{"code":"credit_balance_exhausted"}}`)
	svc := &Service{
		sleep: func(time.Duration) {},
		listChecks: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return ids, nil
		},
		listStarters: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return []uuid.UUID{uuid.New(), uuid.New()}, nil
		},
		replayCheck: func(_ context.Context, id uuid.UUID) (Outcome, error) {
			mu.Lock()
			called = append(called, id)
			mu.Unlock()
			return OutcomeProviderError, providerErr
		},
		replayStarter: func(context.Context, uuid.UUID) (Outcome, error) {
			t.Error("starters should not run after the check queue stops")
			return OutcomeDone, nil
		},
	}
	res, err := svc.Run(context.Background(), Options{
		Apply:       true,
		Since:       domain.AIAnalysisOutageSince,
		Concurrency: 1,
		Pause:       0,
		StopAfter:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped {
		t.Fatal("expected the run to stop")
	}
	if len(called) != 5 {
		t.Fatalf("provider calls = %d, want 5 (oldest first, then stop)", len(called))
	}
	for i := range called {
		if called[i] != ids[i] {
			t.Fatalf("call %d = %s, want oldest-first %s", i, called[i], ids[i])
		}
	}
	if res.Found != 10 || res.StillFailed != 5 || res.Done != 0 || res.Checks.Found != 8 || res.Starters.Found != 2 {
		t.Fatalf("counts = %+v", res)
	}
}

func TestRun_SuccessResetsConsecutiveProviderErrors(t *testing.T) {
	ids := make([]uuid.UUID, 9)
	for i := range ids {
		ids[i] = uuid.New()
	}
	// Four provider errors, one success, four provider errors. Stop-after 5
	// must not trip because the success breaks the streak.
	pattern := []Outcome{
		OutcomeProviderError, OutcomeProviderError, OutcomeProviderError, OutcomeProviderError,
		OutcomeDone,
		OutcomeProviderError, OutcomeProviderError, OutcomeProviderError, OutcomeProviderError,
	}
	var n int
	svc := &Service{
		sleep: func(time.Duration) {},
		listChecks: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return ids, nil
		},
		listStarters: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return nil, nil
		},
		replayCheck: func(context.Context, uuid.UUID) (Outcome, error) {
			out := pattern[n]
			n++
			if out == OutcomeProviderError {
				return out, errors.New("text coach http 503")
			}
			return out, nil
		},
		replayStarter: func(context.Context, uuid.UUID) (Outcome, error) {
			return OutcomeDone, nil
		},
	}
	res, err := svc.Run(context.Background(), Options{
		Apply: true, Concurrency: 1, Pause: 0, StopAfter: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped || n != len(pattern) || res.Done != 1 || res.StillFailed != 8 {
		t.Fatalf("stopped=%v calls=%d counts=%+v", res.Stopped, n, res)
	}
}

func TestRun_LocalFailureBreaksProviderStreak(t *testing.T) {
	ids := make([]uuid.UUID, 9)
	for i := range ids {
		ids[i] = uuid.New()
	}
	pattern := []Outcome{
		OutcomeProviderError, OutcomeProviderError, OutcomeProviderError, OutcomeProviderError,
		OutcomeFailed,
		OutcomeProviderError, OutcomeProviderError, OutcomeProviderError, OutcomeProviderError,
	}
	var n int
	svc := &Service{
		sleep: func(time.Duration) {},
		listChecks: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return ids, nil
		},
		listStarters: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
			return nil, nil
		},
		replayCheck: func(context.Context, uuid.UUID) (Outcome, error) {
			out := pattern[n]
			n++
			return out, errors.New("could not save completed analysis")
		},
		replayStarter: func(context.Context, uuid.UUID) (Outcome, error) {
			return OutcomeDone, nil
		},
	}
	res, err := svc.Run(context.Background(), Options{
		Apply: true, Concurrency: 1, Pause: 0, StopAfter: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped || n != len(pattern) || res.StillFailed != 9 {
		t.Fatalf("a local failure should break the streak, stopped=%v calls=%d counts=%+v", res.Stopped, n, res)
	}
}

func TestRun_SecondPassSkipsCompletedWork(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	done := map[uuid.UUID]bool{}
	var mu sync.Mutex
	var providerCalls int

	list := func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) {
		mu.Lock()
		defer mu.Unlock()
		out := make([]uuid.UUID, 0, len(ids))
		for _, id := range ids {
			if !done[id] {
				out = append(out, id)
			}
		}
		return out, nil
	}
	svc := &Service{
		sleep:        func(time.Duration) {},
		listChecks:   list,
		listStarters: func(context.Context, time.Time, time.Time, int) ([]uuid.UUID, error) { return nil, nil },
		replayCheck: func(_ context.Context, id uuid.UUID) (Outcome, error) {
			mu.Lock()
			defer mu.Unlock()
			if done[id] {
				return OutcomeSkipped, nil
			}
			providerCalls++
			done[id] = true
			return OutcomeDone, nil
		},
		replayStarter: func(context.Context, uuid.UUID) (Outcome, error) { return OutcomeSkipped, nil },
	}
	opt := Options{Apply: true, Concurrency: 1, Pause: 0, StopAfter: 5}
	first, err := svc.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Done != 2 || first.Skipped != 0 || providerCalls != 2 {
		t.Fatalf("first pass = %+v calls=%d", first, providerCalls)
	}
	second, err := svc.Run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if second.Found != 0 || second.Done != 0 || providerCalls != 2 {
		t.Fatalf("second pass should not call providers, counts=%+v calls=%d", second, providerCalls)
	}
}
