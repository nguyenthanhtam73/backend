package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrAccountNotFound means the account row was already gone.
var ErrAccountNotFound = errors.New("account not found")

// FindUser loads the account, or (nil, nil) when it does not exist.
func (r *UserDataRepository) FindUser(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	var user domain.User
	tx := db.WithContext(ctx).Where("id = ?", userID).First(&user)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &user, nil
}

// DeleteAccount removes the account and everything that still identifies the
// person. Photo keys are returned so the caller can delete files after commit.
//
// Deleted (hard): users, refresh_sessions, skin_checks, skin_analyses,
// skin_profiles, routine_entries, skincare_products, ai_user_feedbacks,
// feedbacks, affiliate_clicks, progress_logs, push_subscriptions,
// push_send_receipts, checkin_reminder_flags, routine_suggest_jobs, streaks,
// user_usages, subscriptions, admin_skin_reviews owned by this account,
// beta_signups whose email matches.
//
// Anonymized: funnel_events (user_id NULL, props scrubbed), paywall_views
// (user_id NULL), email_send_receipts (user_id replaced), usage_events
// (user_id replaced), payment_orders (user_id NULL, custom data and webhook
// cleared; invoice number, amounts, and dates kept), payment_ops_events
// (invoice number unchanged), plan_change_logs (user ids NULL, this
// account's actor email cleared).
//
// Nullability of payment_orders.user_id and plan_change_logs user ids is
// applied once at startup (migration 024). This path only reads that state.
//
// There is no device-token table and no email-verification or password-reset
// token table. First-touch attribution columns live on users and go away
// with that row. Active Premium is removed with the account; nothing here
// issues a refund.
func (r *UserDataRepository) DeleteAccount(ctx context.Context, userID uuid.UUID) ([]string, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id required")
	}
	if err := accountDeletionSchemaReady(db); err != nil {
		return nil, err
	}

	var keys []string
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAccountNotFound
			}
			return err
		}
		listed, err := listAccountPhotoKeys(tx, &user)
		if err != nil {
			return err
		}
		keys = listed
		if err := anonymizeAccountRows(tx, &user); err != nil {
			return err
		}
		if err := deletePersonalRows(tx, userID, true); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&domain.RefreshSession{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&domain.Subscription{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("admin_user_id = ?", userID).Delete(&domain.AdminSkinReview{}).Error; err != nil {
			return err
		}
		email := strings.ToLower(strings.TrimSpace(user.Email))
		if email != "" {
			if err := tx.Where("LOWER(email) = ?", email).Delete(&domain.BetaSignup{}).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Where("id = ?", userID).Delete(&domain.User{}).Error
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func listAccountPhotoKeys(tx *gorm.DB, user *domain.User) ([]string, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	seen := map[string]struct{}{}
	addRaw := func(raw json.RawMessage) {
		rels, _ := dto.DecodeStringSlice(raw)
		for _, rel := range rels {
			addPhotoKey(seen, rel)
		}
	}
	add := func(raw string) { addPhotoKey(seen, raw) }

	var checks []domain.SkinCheck
	if err := tx.Unscoped().Select("image_urls").Where("user_id = ?", user.ID).Find(&checks).Error; err != nil {
		return nil, err
	}
	for i := range checks {
		addRaw(checks[i].ImageURLs)
	}
	var profiles []domain.SkinProfile
	if err := tx.Unscoped().Select("photo_urls").Where("user_id = ?", user.ID).Find(&profiles).Error; err != nil {
		return nil, err
	}
	for i := range profiles {
		addRaw(profiles[i].PhotoURLs)
	}
	var logs []domain.ProgressLog
	if err := tx.Unscoped().Select("image_urls").Where("user_id = ?", user.ID).Find(&logs).Error; err != nil {
		return nil, err
	}
	for i := range logs {
		addRaw(logs[i].ImageURLs)
	}
	var reviews []domain.AdminSkinReview
	if err := tx.Select("image_paths", "public_image_paths").Where("admin_user_id = ?", user.ID).Find(&reviews).Error; err != nil {
		return nil, err
	}
	for i := range reviews {
		addRaw(reviews[i].ImagePaths)
		addRaw(reviews[i].PublicImagePaths)
	}
	add(user.AvatarURL)

	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	return out, nil
}

func addPhotoKey(seen map[string]struct{}, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if i := strings.Index(raw, "/uploads/"); i >= 0 {
		raw = raw[i:]
	} else if strings.Contains(raw, "://") {
		return
	}
	key := storage.CleanKey(raw)
	if key == "" {
		return
	}
	seen[key] = struct{}{}
}

func anonymizeAccountRows(tx *gorm.DB, user *domain.User) error {
	email := strings.ToLower(strings.TrimSpace(user.Email))
	if err := anonymizeFunnelEvents(tx, user.ID, email); err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE paywall_views SET user_id = NULL WHERE user_id = ?`, user.ID).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		`UPDATE email_send_receipts SET user_id = ? WHERE user_id = ?`,
		uuid.New(), user.ID,
	).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		`UPDATE usage_events SET user_id = ? WHERE user_id = ?`,
		uuid.New(), user.ID,
	).Error; err != nil {
		return err
	}
	if err := anonymizePaymentOrders(tx, user.ID); err != nil {
		return err
	}
	return anonymizePlanChangeLogs(tx, user.ID, email)
}

func anonymizeFunnelEvents(tx *gorm.DB, userID uuid.UUID, email string) error {
	var events []domain.FunnelEvent
	if err := tx.Where("user_id = ?", userID).Find(&events).Error; err != nil {
		return err
	}
	for i := range events {
		props := scrubPersonal(string(events[i].Props), email, userID)
		if strings.TrimSpace(props) == "" {
			props = "{}"
		}
		if err := tx.Model(&domain.FunnelEvent{}).
			Where("id = ?", events[i].ID).
			Update("props", json.RawMessage(props)).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`UPDATE funnel_events SET user_id = NULL WHERE user_id = ?`, userID).Error
}

func anonymizePaymentOrders(tx *gorm.DB, userID uuid.UUID) error {
	// Raw SQL so soft-deleted orders are included. invoice_number stays so
	// SePay reconciliation and a late webhook can still find the row.
	// payment_ops_events keep that same invoice number.
	return tx.Exec(
		`UPDATE payment_orders SET user_id = NULL, custom_data = '', raw_webhook = '' WHERE user_id = ?`,
		userID,
	).Error
}

func anonymizePlanChangeLogs(tx *gorm.DB, userID uuid.UUID, email string) error {
	var logs []domain.PlanChangeLog
	q := tx.Where("user_id = ? OR actor_user_id = ?", userID, userID)
	if email != "" {
		q = tx.Where("user_id = ? OR actor_user_id = ? OR LOWER(actor_email) = ?", userID, userID, email)
	}
	if err := q.Find(&logs).Error; err != nil {
		return err
	}
	for i := range logs {
		cleaned := scrubPersonal(logs[i].Reason, email, userID)
		if cleaned == logs[i].Reason {
			continue
		}
		if err := tx.Model(&domain.PlanChangeLog{}).Where("id = ?", logs[i].ID).Update("reason", cleaned).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(`UPDATE plan_change_logs SET user_id = NULL WHERE user_id = ?`, userID).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		`UPDATE plan_change_logs SET actor_user_id = NULL, actor_email = '' WHERE actor_user_id = ?`,
		userID,
	).Error; err != nil {
		return err
	}
	if email == "" {
		return nil
	}
	return tx.Exec(
		`UPDATE plan_change_logs SET actor_email = '' WHERE LOWER(actor_email) = ?`,
		email,
	).Error
}

func scrubPersonal(raw, email string, userID uuid.UUID) string {
	if raw == "" {
		return raw
	}
	out := raw
	for _, part := range personalNeedles(email, userID) {
		out = strings.ReplaceAll(out, part, "")
	}
	return out
}

func personalNeedles(email string, userID uuid.UUID) []string {
	email = strings.TrimSpace(email)
	var parts []string
	if email != "" {
		parts = append(parts, email, strings.ToLower(email))
	}
	if userID != uuid.Nil {
		id := userID.String()
		parts = append(parts, id, strings.ReplaceAll(id, "-", ""))
		short := strings.ReplaceAll(id, "-", "")
		if len(short) > 8 {
			short = short[:8]
		}
		parts = append(parts, strings.ToUpper(short), short)
	}
	return parts
}
