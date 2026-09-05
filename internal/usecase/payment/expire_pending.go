package payment

import (
	"context"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
)

// DefaultPendingOrderTTL is the local hygiene window for leftover SePay checkouts.
//
// Hypothesis (not a SePay-documented session lifetime — repo checklists and
// sepay.go only describe form POST + IPN; they do not state when SePay voids
// an unpaid PURCHASE). 72h is the conservative end of the 24–72h range so a
// same-day / next-day BANK_TRANSFER is less likely to be marked expired in
// our table while money can still settle. This only updates payment_orders;
// a later ORDER_PAID IPN still fulfills via MarkPaidTx.
const DefaultPendingOrderTTL = 72 * time.Hour

const (
	minPendingOrderTTLHours = 24
	maxPendingOrderTTLHours = 168
)

// PendingOrderTTL returns the configured local expiry window (clamped).
func PendingOrderTTL(cfg *config.Config) time.Duration {
	hours := int(DefaultPendingOrderTTL / time.Hour)
	if cfg != nil && cfg.PendingOrderExpiry.TTLHours > 0 {
		hours = cfg.PendingOrderExpiry.TTLHours
	}
	if hours < minPendingOrderTTLHours {
		hours = minPendingOrderTTLHours
	}
	if hours > maxPendingOrderTTLHours {
		hours = maxPendingOrderTTLHours
	}
	return time.Duration(hours) * time.Hour
}

// ExpireStalePendingResult is the outcome of one idempotent pending-order sweep.
type ExpireStalePendingResult struct {
	TTLHours     int
	Cutoff       time.Time
	Expired      int64
	PendingFresh int64
	PendingStale int64 // counted before the UPDATE (dry-run / CLI)
}

// ToDTO maps the sweep onto the admin API envelope.
func (r ExpireStalePendingResult) ToDTO() dto.ExpirePendingOrdersResponse {
	return dto.ExpirePendingOrdersResponse{
		TTLHours:           r.TTLHours,
		Cutoff:             r.Cutoff.UTC().Format(time.RFC3339),
		Expired:            r.Expired,
		PendingFresh:       r.PendingFresh,
		PendingStaleBefore: r.PendingStale,
	}
}

// ExpireStalePending marks leftover pending SePay orders older than the TTL as
// expired. Safe to re-run: only status=pending rows are updated. Does not call
// SePay and does not block a later paid IPN.
func (s *Service) ExpireStalePending(ctx context.Context, now time.Time) (ExpireStalePendingResult, error) {
	var out ExpireStalePendingResult
	if s == nil || s.db == nil || s.orders == nil {
		return out, ErrUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	ttl := PendingOrderTTL(s.cfg)
	cutoff := now.Add(-ttl)
	out.TTLHours = int(ttl / time.Hour)
	out.Cutoff = cutoff

	stale, err := s.orders.CountPendingCreatedBefore(ctx, cutoff)
	if err != nil {
		return out, err
	}
	out.PendingStale = stale

	n, err := s.orders.ExpirePendingOlderThan(ctx, cutoff)
	if err != nil {
		return out, err
	}
	out.Expired = n

	fresh, err := s.orders.CountPendingCreatedOnOrAfter(ctx, cutoff)
	if err != nil {
		return out, err
	}
	out.PendingFresh = fresh

	slog.Info("payment: expire stale pending",
		"ttl_hours", out.TTLHours,
		"cutoff", cutoff.Format(time.RFC3339),
		"expired", out.Expired,
		"pending_stale_before", out.PendingStale,
		"pending_fresh", out.PendingFresh,
	)
	return out, nil
}

// PreviewStalePending counts leftover pending orders without writing (CLI dry-run).
func (s *Service) PreviewStalePending(ctx context.Context, now time.Time) (ExpireStalePendingResult, error) {
	var out ExpireStalePendingResult
	if s == nil || s.orders == nil {
		return out, ErrUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	ttl := PendingOrderTTL(s.cfg)
	cutoff := now.Add(-ttl)
	out.TTLHours = int(ttl / time.Hour)
	out.Cutoff = cutoff

	stale, err := s.orders.CountPendingCreatedBefore(ctx, cutoff)
	if err != nil {
		return out, err
	}
	out.PendingStale = stale
	fresh, err := s.orders.CountPendingCreatedOnOrAfter(ctx, cutoff)
	if err != nil {
		return out, err
	}
	out.PendingFresh = fresh
	return out, nil
}
