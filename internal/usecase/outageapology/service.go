package outageapology

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
)

const (
	// DefaultPause is the gap between real sends.
	DefaultPause = 200 * time.Millisecond
	// DefaultStopAfter stops the run after this many Resend errors.
	DefaultStopAfter = 5
)

// Mode selects dry-run, a single admin test, or the real send.
type Mode int

const (
	ModeDryRun Mode = iota
	ModeTest
	ModeApply
)

// Result is counts only. It never includes an email address.
type Result struct {
	DryRun     bool `json:"dry_run"`
	Test       bool `json:"test"`
	Recipients int  `json:"recipients"`
	Sent       int  `json:"sent"`
	Skipped    int  `json:"skipped"`
	Failed     int  `json:"failed"`
	Stopped    bool `json:"stopped"`
}

// Service sends the outage apology through the existing Resend sender.
type Service struct {
	checks   *repository.GormSkinCheckRepository
	receipts *repository.EmailSendReceiptRepository
	mail     email.Sender
	origin   string
	admins   []string
	sleep    func(time.Duration)
}

// NewService wires persistence and the Resend client. admins is DADIARY_ADMIN_EMAILS.
func NewService(
	checks *repository.GormSkinCheckRepository,
	receipts *repository.EmailSendReceiptRepository,
	mail email.Sender,
	origin string,
	admins []string,
) *Service {
	return &Service{
		checks:   checks,
		receipts: receipts,
		mail:     mail,
		origin:   origin,
		admins:   append([]string(nil), admins...),
		sleep:    time.Sleep,
	}
}

// Run lists the cohort and, depending on mode, counts, sends one admin test, or sends for real.
func (s *Service) Run(ctx context.Context, mode Mode, pause time.Duration, stopAfter int) (Result, error) {
	var zero Result
	if s == nil || s.checks == nil {
		return zero, fmt.Errorf("outage apology: not configured")
	}
	if stopAfter <= 0 {
		stopAfter = DefaultStopAfter
	}
	if pause < 0 {
		pause = 0
	}

	if mode == ModeTest {
		return s.sendTest(ctx)
	}

	rows, err := s.checks.ListCreatedBetween(ctx, WindowStart, WindowEnd)
	if err != nil {
		return zero, err
	}
	recipients := SelectRecipients(rows, WindowStart, WindowEnd)
	res := Result{DryRun: mode == ModeDryRun, Recipients: len(recipients)}
	if mode == ModeDryRun {
		slog.Info("outage-apology: dry-run", "recipients", res.Recipients)
		return res, nil
	}
	if s.mail == nil || !s.mail.Configured() {
		return zero, fmt.Errorf("outage apology: email sender not configured")
	}
	if s.receipts == nil {
		return zero, fmt.Errorf("outage apology: receipts not configured")
	}

	for i, rec := range recipients {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if i > 0 && pause > 0 && s.sleep != nil {
			s.sleep(pause)
		}
		claimed, err := s.receipts.TryClaim(ctx, rec.UserID, ReceiptKind)
		if err != nil {
			return res, err
		}
		if !claimed {
			res.Skipped++
			continue
		}
		checkID := ""
		if !rec.NeedsRecheck {
			checkID = rec.CompletedCheck.String()
		}
		msg := Build(s.origin, checkID)
		providerID, err := s.mail.Send(ctx, email.Message{
			To:      rec.Email,
			Subject: msg.Subject,
			Text:    msg.Text,
			HTML:    msg.HTML,
			Quiet:   true,
		})
		if err != nil {
			_ = s.receipts.Release(ctx, rec.UserID, ReceiptKind)
			res.Failed++
			slog.Warn("outage-apology: send failed", "failed", res.Failed)
			if res.Failed >= stopAfter {
				res.Stopped = true
				break
			}
			continue
		}
		if providerID != "" {
			_ = s.receipts.SetResendEmailID(ctx, rec.UserID, ReceiptKind, providerID)
		}
		res.Sent++
	}
	slog.Info("outage-apology: finished",
		"recipients", res.Recipients,
		"sent", res.Sent,
		"skipped", res.Skipped,
		"failed", res.Failed,
		"stopped", res.Stopped,
	)
	return res, nil
}

func (s *Service) sendTest(ctx context.Context) (Result, error) {
	var zero Result
	if s.mail == nil || !s.mail.Configured() {
		return zero, fmt.Errorf("outage apology: email sender not configured")
	}
	var to string
	for _, addr := range s.admins {
		if addr != "" {
			to = addr
			break
		}
	}
	if to == "" {
		return zero, fmt.Errorf("outage apology: no admin address configured")
	}
	msg := Build(s.origin, "outage-apology-test")
	if _, err := s.mail.Send(ctx, email.Message{
		To:      to,
		Subject: msg.Subject,
		Text:    msg.Text,
		HTML:    msg.HTML,
		Quiet:   true,
	}); err != nil {
		slog.Warn("outage-apology: test send failed")
		return Result{Test: true, Recipients: 1, Failed: 1}, err
	}
	slog.Info("outage-apology: test sent", "recipients", 1)
	return Result{Test: true, Recipients: 1, Sent: 1}, nil
}
