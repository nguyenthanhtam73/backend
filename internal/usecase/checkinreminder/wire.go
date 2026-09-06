package checkinreminder

import (
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"gorm.io/gorm"
)

// AttachFromConfig wires Resend + unsubscribe + optional push from app config.
func AttachFromConfig(
	s *Service,
	cfg *config.Config,
	db *gorm.DB,
	push *pushuc.Service,
) {
	if s == nil {
		return
	}
	jobOn := true
	emailOn := false
	checkInURL := "https://dadiary.vn/check-in"
	apiOrigin := ""
	jwtSecret := ""
	apiKey, from := "", ""
	if cfg != nil {
		jobOn = cfg.CheckInReminder.Enabled
		emailOn = cfg.HasEmailESP()
		checkInURL = cfg.CheckInURL()
		apiOrigin = cfg.PublicAPIOrigin()
		jwtSecret = cfg.JWT.Secret
		apiKey = cfg.Email.ResendAPIKey
		from = cfg.Email.From
	}
	s.SetJobEnabled(jobOn)
	s.SetEmailConfigured(emailOn)

	var receipts *repository.EmailSendReceiptRepository
	if db != nil {
		receipts = repository.NewEmailSendReceiptRepository(db)
	}
	s.AttachOutbound(
		email.NewResendClient(apiKey, from),
		receipts,
		push,
		email.NewUnsubscribeSigner(jwtSecret, apiOrigin),
		checkInURL,
	)
}
