package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
	"github.com/dadiary/backend/pkg/alert"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// systemActorEmail is stored on plan_change_logs for SePay-driven upgrades
// when SubscriptionService is not wired (legacy / tests).
const systemActorEmail = "sepay@system.dadiary"

// Service orchestrates SePay checkout creation and IPN fulfilment.
type Service struct {
	db     *gorm.DB
	cfg    *config.Config
	orders *repository.PaymentOrderRepository
	users  *repository.GormUserRepository
	logs   *repository.PlanChangeLogRepository
	// subs applies renew/cancel lifecycle inside the IPN transaction.
	subs *subscriptionuc.Service
	// alerter is optional ops alerting (Slack / Telegram / console).
	alerter alert.Alerter
	// monitor tracks fail-rate / webhook-error thresholds (optional).
	monitor *Monitor
}

// NewService constructs the payment usecase. Nil deps → ErrUnavailable at call time.
func NewService(
	db *gorm.DB,
	cfg *config.Config,
	orders *repository.PaymentOrderRepository,
	users *repository.GormUserRepository,
	logs *repository.PlanChangeLogRepository,
) *Service {
	return &Service{db: db, cfg: cfg, orders: orders, users: users, logs: logs}
}

// AttachSubscription wires SubscriptionService for ORDER_PAID → HandleRenewal
// and SePay cancel notifications → CancelSubscription. Safe to call once at boot.
func (s *Service) AttachSubscription(subs *subscriptionuc.Service) {
	if s == nil {
		return
	}
	s.subs = subs
}

// AttachAlerter wires optional ops alerts for webhook failures. Safe to call once at boot.
func (s *Service) AttachAlerter(a alert.Alerter) {
	if s == nil {
		return
	}
	s.alerter = a
}

// AttachMonitor wires fail-rate / webhook-error monitoring. Safe to call once at boot.
func (s *Service) AttachMonitor(m *Monitor) {
	if s == nil {
		return
	}
	s.monitor = m
}

func (s *Service) ready() error {
	if s == nil || s.db == nil || s.orders == nil || s.users == nil {
		return ErrUnavailable
	}
	if s.cfg == nil || !s.cfg.SePay.Configured() {
		return ErrNotConfigured
	}
	return nil
}

// CreatePayment persists a pending PaymentOrder, then returns signed SePay form fields
// + checkout URL. The FE (or a tiny HTML page) POSTs form_fields to checkout_url.
func (s *Service) CreatePayment(
	ctx context.Context,
	userID uuid.UUID,
	req dto.CreateSePayCheckoutRequest,
) (dto.CreateSePayCheckoutResponse, error) {
	var zero dto.CreateSePayCheckoutResponse
	if err := s.ready(); err != nil {
		return zero, err
	}
	if userID == uuid.Nil {
		return zero, ErrInvalidUser
	}

	tier, err := normalizePaidPlan(req.PlanTier)
	if err != nil {
		return zero, err
	}
	interval, err := normalizeBillingInterval(req.BillingInterval)
	if err != nil {
		return zero, err
	}
	amount, err := AmountForPlan(tier, interval)
	if err != nil {
		return zero, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if user == nil {
		return zero, ErrInvalidUser
	}

	invoice := newInvoiceNumber(userID)
	custom := customDataPayload{
		UserID:          userID.String(),
		PlanTier:        string(tier),
		BillingInterval: string(interval),
	}
	customJSON, _ := json.Marshal(custom)

	order := &domain.PaymentOrder{
		UserID:          userID,
		InvoiceNumber:   invoice,
		PlanTier:        tier,
		BillingInterval: interval,
		AmountVND:       amount,
		Currency:        "VND",
		Status:          domain.PaymentPending,
		Provider:        domain.PaymentProviderSePay,
		CustomData:      string(customJSON),
	}
	if err := s.orders.Create(ctx, order); err != nil {
		return zero, fmt.Errorf("create payment order: %w", err)
	}

	sepay := s.cfg.SePay
	successURL, errorURL, cancelURL := localizeCallbackURLs(sepay, req.Locale)
	desc := fmt.Sprintf("DaDiary %s (%s)", tier, interval)
	fields := BuildCheckoutFormFields(CheckoutFormInput{
		AmountVND:     amount,
		InvoiceNumber: invoice,
		Description:   desc,
		CustomerID:    userID.String(),
		PaymentMethod: strings.TrimSpace(req.PaymentMethod),
		SuccessURL:    successURL,
		ErrorURL:      errorURL,
		CancelURL:     cancelURL,
		CustomData:    string(customJSON),
	}, sepay.MerchantID, sepay.SecretKey)

	slog.Info("payment: created",
		"user_id", userID.String(),
		"order_id", order.ID.String(),
		"invoice", invoice,
		"plan", string(tier),
		"interval", string(interval),
		"amount", amount,
		"env", sepay.Env,
	)

	return dto.CreateSePayCheckoutResponse{
		OrderID:         order.ID.String(),
		InvoiceNumber:   invoice,
		PlanTier:        string(tier),
		BillingInterval: string(interval),
		AmountVND:       amount,
		Currency:        "VND",
		CheckoutURL:     CheckoutInitURL(sepay.Env),
		FormFields:      fields,
		Env:             sepay.NormalizedEnv(),
	}, nil
}

type customDataPayload struct {
	UserID          string `json:"user_id"`
	PlanTier        string `json:"plan_tier"`
	BillingInterval string `json:"billing_interval"`
}

// IPNPayload is the Payment Gateway notification body (ORDER_PAID, …).
type IPNPayload struct {
	Timestamp        int64  `json:"timestamp"`
	NotificationType string `json:"notification_type"`
	Order            struct {
		ID                 string          `json:"id"`
		OrderID            string          `json:"order_id"`
		OrderStatus        string          `json:"order_status"`
		OrderCurrency      string          `json:"order_currency"`
		OrderAmount        string          `json:"order_amount"`
		OrderInvoiceNumber string          `json:"order_invoice_number"`
		OrderDescription   string          `json:"order_description"`
		CustomData         json.RawMessage `json:"custom_data"`
	} `json:"order"`
	Transaction struct {
		ID                string `json:"id"`
		TransactionID     string `json:"transaction_id"`
		TransactionStatus string `json:"transaction_status"`
		TransactionAmount string `json:"transaction_amount"`
		PaymentMethod     string `json:"payment_method"`
	} `json:"transaction"`
	Customer struct {
		CustomerID string `json:"customer_id"`
	} `json:"customer"`
}

// HandleSePayWebhook verifies auth, applies ORDER_PAID → SubscriptionService.HandleRenewal
// (via ApplyRenewalTx in the same DB transaction), and cancel IPNs → CancelSubscription.
// Always designed so SePay can retry safely; returns nil when ack is OK.
func (s *Service) HandleSePayWebhook(
	ctx context.Context,
	secretHeader string,
	rawBody []byte,
) error {
	if err := s.ready(); err != nil {
		return s.webhookFail(ctx, "service_unavailable", err, nil, true)
	}
	if !VerifyIPNSecretKey(secretHeader, s.cfg.SePay.SecretKey) {
		return s.webhookFail(ctx, "signature_invalid", ErrUnauthorizedIPN, map[string]any{
			"body_bytes": len(rawBody),
		}, true)
	}

	var payload IPNPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return s.webhookFail(ctx, "invalid_json", fmt.Errorf("%w: invalid json", ErrInvalidRequest), map[string]any{
			"body_bytes": len(rawBody),
		}, false)
	}

	nt := strings.ToUpper(strings.TrimSpace(payload.NotificationType))
	orderStatus := strings.ToUpper(strings.TrimSpace(payload.Order.OrderStatus))
	invoice := strings.TrimSpace(payload.Order.OrderInvoiceNumber)

	slog.Info("payment: ipn received",
		"notification_type", nt,
		"order_status", orderStatus,
		"invoice", invoice,
		"order_id", firstNonEmpty(payload.Order.ID, payload.Order.OrderID),
		"amount", firstNonEmpty(payload.Order.OrderAmount, payload.Transaction.TransactionAmount),
		"customer_id", strings.TrimSpace(payload.Customer.CustomerID),
	)

	// Cancel / void notifications — mark subscription canceled_at (access until grace ends).
	if isSePayCancelNotification(nt, orderStatus) {
		if err := s.handleSePayCancel(ctx, payload); err != nil {
			return s.webhookFail(ctx, "cancel_failed", err, map[string]any{
				"invoice":           invoice,
				"notification_type": nt,
				"order_status":      orderStatus,
			}, true)
		}
		return nil
	}

	// Acknowledge other non-paid events without mutating plan — alert on unknown types.
	if nt != "" && nt != "ORDER_PAID" {
		fields := map[string]any{
			"notification_type": nt,
			"order_status":      orderStatus,
			"invoice":           invoice,
		}
		slog.Warn("payment: ipn unknown status",
			"notification_type", nt,
			"order_status", orderStatus,
			"invoice", invoice,
		)
		s.alertWebhook(ctx, "unknown_status", "unexpected SePay notification_type", alert.LevelError, fields)
		if s.monitor != nil {
			s.monitor.RecordWebhookError(ctx, "unknown_status", invoice)
		}
		return nil
	}

	if invoice == "" {
		return s.webhookFail(ctx, "missing_invoice", fmt.Errorf("%w: missing order_invoice_number", ErrInvalidRequest), map[string]any{
			"notification_type": nt,
			"order_status":      orderStatus,
		}, false)
	}

	// CAPTURED / PAID / COMPLETED all mean money settled for one-time PURCHASE.
	paidOK := orderStatus == "CAPTURED" || orderStatus == "PAID" || orderStatus == "COMPLETED" || nt == "ORDER_PAID"
	if !paidOK {
		if orderStatus != "" && !isKnownPendingStatus(orderStatus) {
			fields := map[string]any{
				"invoice":      invoice,
				"order_status": orderStatus,
			}
			slog.Warn("payment: ipn unknown order_status",
				"invoice", invoice,
				"order_status", orderStatus,
			)
			s.alertWebhook(ctx, "unknown_status", "unexpected SePay order_status", alert.LevelError, fields)
			if s.monitor != nil {
				s.monitor.RecordWebhookError(ctx, "unknown_status", invoice)
			}
			return nil
		}
		slog.Info("payment: sepay order not paid yet",
			"invoice", invoice,
			"order_status", orderStatus,
		)
		return nil
	}

	amountRaw := payload.Order.OrderAmount
	if amountRaw == "" {
		amountRaw = payload.Transaction.TransactionAmount
	}
	paidAmount, err := ParseAmountVND(amountRaw)
	if err != nil {
		return s.webhookFail(ctx, "bad_amount", fmt.Errorf("%w: bad amount", ErrInvalidRequest), map[string]any{
			"invoice": invoice,
			"amount":  amountRaw,
		}, false)
	}

	existing, err := s.orders.GetByInvoiceNumber(ctx, invoice)
	if err != nil {
		return s.webhookFail(ctx, "order_lookup_failed", err, map[string]any{
			"invoice": invoice,
		}, true)
	}
	if existing == nil {
		return s.webhookFail(ctx, "order_not_found", ErrOrderNotFound, map[string]any{
			"invoice": invoice,
			"amount":  paidAmount,
		}, false)
	}
	// Idempotent fast-path (order already paid → renewal history already written).
	if existing.Status == domain.PaymentPaid {
		slog.Info("payment: ipn already paid (noop)",
			"user_id", existing.UserID.String(),
			"order_id", existing.ID.String(),
			"invoice", invoice,
			"plan", string(existing.PlanTier),
			"amount", existing.AmountVND,
		)
		return nil
	}
	if paidAmount < existing.AmountVND {
		slog.Warn("payment: amount below order",
			"user_id", existing.UserID.String(),
			"order_id", existing.ID.String(),
			"invoice", invoice,
			"plan", string(existing.PlanTier),
			"amount", paidAmount,
			"expected", existing.AmountVND,
		)
		if s.monitor != nil {
			s.monitor.RecordFailure(ctx, "amount_mismatch", invoice)
		}
		return ErrAmountMismatch
	}

	if err := s.fulfillPaidOrder(ctx, existing, payload, string(rawBody)); err != nil {
		return s.webhookFail(ctx, "fulfill_failed", err, map[string]any{
			"user_id":  existing.UserID.String(),
			"order_id": existing.ID.String(),
			"invoice":  invoice,
			"plan":     string(existing.PlanTier),
			"amount":   existing.AmountVND,
		}, true)
	}
	return nil
}

// isKnownPendingStatus reports SePay statuses that are expected before capture.
func isKnownPendingStatus(orderStatus string) bool {
	switch orderStatus {
	case "PENDING", "PROCESSING", "AUTHORIZED", "CREATED", "INITIATED":
		return true
	default:
		return false
	}
}

// isSePayCancelNotification detects cancel / void IPNs from SePay PG.
// SePay one-time PURCHASE rarely emits these; we still honour them for lifecycle.
func isSePayCancelNotification(notificationType, orderStatus string) bool {
	switch notificationType {
	case "ORDER_CANCELLED", "ORDER_CANCELED", "PAYMENT_CANCELLED", "PAYMENT_CANCELED",
		"SUBSCRIPTION_CANCELLED", "SUBSCRIPTION_CANCELED", "ORDER_VOIDED":
		return true
	}
	switch orderStatus {
	case "CANCELLED", "CANCELED", "VOIDED", "REFUNDED":
		return true
	}
	return false
}

// handleSePayCancel maps a cancel IPN onto CancelSubscription (idempotent ack).
func (s *Service) handleSePayCancel(ctx context.Context, payload IPNPayload) error {
	if s.subs == nil {
		slog.Info("payment: sepay cancel ignored — subscription service not wired",
			"invoice", payload.Order.OrderInvoiceNumber,
		)
		return nil
	}

	userID, err := s.resolveIPNUserID(ctx, payload)
	if err != nil {
		return err
	}
	if userID == uuid.Nil {
		slog.Info("payment: sepay cancel — no user resolved",
			"invoice", payload.Order.OrderInvoiceNumber,
			"customer_id", payload.Customer.CustomerID,
		)
		return nil
	}

	_, err = s.subs.CancelSubscription(ctx, userID)
	if err == nil {
		slog.Info("payment: cancel",
			"user_id", userID.String(),
			"invoice", payload.Order.OrderInvoiceNumber,
			"source", "sepay_ipn",
		)
		return nil
	}
	// Already canceled / not active → ack so SePay stops retrying.
	if errors.Is(err, subscriptionuc.ErrAlreadyCanceled) ||
		errors.Is(err, subscriptionuc.ErrNotActive) ||
		errors.Is(err, subscriptionuc.ErrInvalidUser) {
		slog.Info("payment: cancel noop",
			"user_id", userID.String(),
			"invoice", payload.Order.OrderInvoiceNumber,
			"error", err.Error(),
		)
		return nil
	}
	return err
}

func (s *Service) resolveIPNUserID(ctx context.Context, payload IPNPayload) (uuid.UUID, error) {
	if raw := strings.TrimSpace(payload.Customer.CustomerID); raw != "" {
		if id, err := uuid.Parse(raw); err == nil && id != uuid.Nil {
			return id, nil
		}
	}
	invoice := strings.TrimSpace(payload.Order.OrderInvoiceNumber)
	if invoice == "" {
		return uuid.Nil, nil
	}
	order, err := s.orders.GetByInvoiceNumber(ctx, invoice)
	if err != nil {
		return uuid.Nil, err
	}
	if order == nil {
		return uuid.Nil, nil
	}
	return order.UserID, nil
}

func (s *Service) fulfillPaidOrder(
	ctx context.Context,
	order *domain.PaymentOrder,
	payload IPNPayload,
	raw string,
) error {
	targetTier := domain.NormalizePlanTier(order.PlanTier)
	if !targetTier.IsPaidPlan() {
		return fmt.Errorf("%w: order plan_tier invalid", ErrInvalidRequest)
	}

	now := time.Now().UTC()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1) Lock payment_order FOR UPDATE — serializes duplicate IPNs for same invoice.
		marked, alreadyPaid, err := s.orders.MarkPaidTx(tx, repository.MarkPaidParams{
			InvoiceNumber:      order.InvoiceNumber,
			SePayOrderID:       firstNonEmpty(payload.Order.ID, payload.Order.OrderID),
			SePayTransactionID: firstNonEmpty(payload.Transaction.TransactionID, payload.Transaction.ID),
			RawWebhook:         raw,
			PaidAt:             now,
		})
		if err != nil {
			return err
		}
		if marked == nil {
			return ErrOrderNotFound
		}
		if alreadyPaid {
			// Idempotent replay: do not touch user (avoids double-extending expiry).
			slog.Info("payment: ipn already paid (noop)",
				"invoice", marked.InvoiceNumber,
				"user_id", marked.UserID.String(),
			)
			return nil
		}

		// 2) Renew / first paid upgrade via SubscriptionService (same tx).
		if s.subs != nil {
			_, err := s.subs.ApplyRenewalTx(tx, subscriptionuc.RenewalInput{
				UserID:          marked.UserID,
				PlanTier:        targetTier,
				BillingInterval: marked.BillingInterval,
				ExternalRef:     marked.InvoiceNumber, // idempotency key
				Provider:        domain.SubProviderSePay,
				Now:             now,
			})
			return err
		}

		// Legacy fallback when SubscriptionService is not attached (unit tests).
		return s.applyPlanLegacyTx(tx, marked, targetTier, now)
	})
	if err != nil {
		return err
	}

	slog.Info("payment: fulfill success",
		"user_id", order.UserID.String(),
		"order_id", order.ID.String(),
		"invoice", order.InvoiceNumber,
		"plan", string(targetTier),
		"amount", order.AmountVND,
		"interval", string(order.BillingInterval),
		"via_subscription", s.subs != nil,
	)
	if s.monitor != nil {
		s.monitor.RecordSuccess(ctx, order.InvoiceNumber)
	}
	s.notifyPaymentSuccess(ctx, order, targetTier)
	return nil
}

// notifyPaymentSuccess pings ops (Telegram/Slack) for each paid fulfill.
// UniqueSuffix=invoice so every real payment notifies (not shared 15m spam bucket).
// Entire notify (optional email lookup + alert.Send) runs in a background goroutine
// so the IPN handler returns immediately.
func (s *Service) notifyPaymentSuccess(_ context.Context, order *domain.PaymentOrder, plan domain.PlanTier) {
	if s == nil || order == nil {
		return
	}
	userID := order.UserID
	invoice := order.InvoiceNumber
	orderID := order.ID.String()
	amount := order.AmountVND
	interval := string(order.BillingInterval)
	planStr := string(plan)
	users := s.users
	alerter := s.alerter

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("payment: success notify panic", "recover", fmt.Sprint(r))
			}
		}()
		userLabel := userID.String()
		if users != nil {
			if u, err := users.GetByID(context.Background(), userID); err == nil && u != nil {
				if e := strings.TrimSpace(u.Email); e != "" {
					userLabel = e
				}
			}
		}
		msg := fmt.Sprintf(
			"User %s nâng cấp %s thành công, amount %d VND (%s)",
			userLabel, planStr, amount, interval,
		)
		alert.Send(context.Background(), alerter, alert.Event{
			Key:          alert.KeyPaymentSuccess,
			UniqueSuffix: invoice,
			Title:        "Payment success",
			Level:        alert.LevelInfo,
			Message:      msg,
			Detail:       "invoice=" + invoice,
			Fields: map[string]any{
				"reason":   alert.KeyPaymentSuccess,
				"user_id":  userID.String(),
				"user":     userLabel,
				"plan":     planStr,
				"amount":   amount,
				"interval": interval,
				"invoice":  invoice,
				"order_id": orderID,
			},
		})
	}()
}

// webhookFail logs Error and optionally sends an ops alert, then returns err unchanged.
// Alert on signature_invalid, unknown_status (via alertWebhook), and internal/5xx paths.
func (s *Service) webhookFail(ctx context.Context, reason string, err error, fields map[string]any, sendAlert bool) error {
	if fields == nil {
		fields = map[string]any{}
	}
	if err != nil {
		fields["error"] = err.Error()
	}
	fields["reason"] = reason

	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	slog.Error("payment: webhook failed", attrs...)

	invoice, _ := fields["invoice"].(string)
	if sendAlert {
		msg := reason
		if err != nil {
			msg = err.Error()
		}
		s.alertWebhook(ctx, reason, msg, alert.LevelError, fields)
		if s.monitor != nil {
			s.monitor.RecordWebhookError(ctx, reason, invoice)
		}
	} else if s.monitor != nil {
		s.monitor.RecordFailure(ctx, reason, invoice)
	}
	return err
}

func (s *Service) alertWebhook(ctx context.Context, reason, message string, level alert.Level, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["reason"] = reason
	// Shared Key bucket = anti-spam (e.g. all signature_invalid share 15m).
	// Pass UniqueSuffix (invoice) only when each distinct event must page ops.
	detail := ""
	if inv, ok := fields["invoice"].(string); ok && inv != "" {
		detail = "invoice=" + inv
	}
	alert.Send(ctx, s.alerter, alert.Event{
		Key:     reason,
		Title:   "SePay webhook: " + reason,
		Level:   level,
		Message: message,
		Detail:  detail,
		Fields:  fields,
	})
}

// applyPlanLegacyTx upgrades plan_tier without subscriptions history (tests / fallback).
func (s *Service) applyPlanLegacyTx(
	tx *gorm.DB,
	marked *domain.PaymentOrder,
	targetTier domain.PlanTier,
	now time.Time,
) error {
	user, err := s.users.GetByIDForUpdateTx(tx, marked.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrInvalidUser
	}

	fromStored := domain.NormalizePlanTier(user.PlanTier)
	fromEffective := domain.EffectivePlanTierWithGrace(user, now, domain.DefaultGraceDays)
	to := higherPlan(fromEffective, targetTier)
	if !to.IsPaidPlan() {
		to = targetTier
	}
	expires := domain.ComputePlanExpiry(marked.BillingInterval, now, user.PlanExpiresAt)

	updated, err := s.users.UpdatePlanTierTx(tx, marked.UserID, to, &expires)
	if err != nil {
		return err
	}
	if updated == nil {
		return ErrInvalidUser
	}

	if s.logs != nil && fromStored != to {
		logRow := domain.PlanChangeLog{
			UserID:      marked.UserID,
			ActorUserID: marked.UserID,
			ActorEmail:  systemActorEmail,
			FromPlan:    fromStored,
			ToPlan:      to,
			Reason:      fmt.Sprintf("sepay:%s", marked.InvoiceNumber),
		}
		if err := s.logs.CreateTx(tx, &logRow); err != nil {
			return err
		}
	}

	slog.Info("payment: plan applied under lock (legacy)",
		"user_id", marked.UserID.String(),
		"from", string(fromStored),
		"to", string(to),
		"expires_at", expires.Format(time.RFC3339),
		"invoice", marked.InvoiceNumber,
	)
	return nil
}

func higherPlan(a, b domain.PlanTier) domain.PlanTier {
	rank := func(t domain.PlanTier) int {
		switch domain.NormalizePlanTier(t) {
		case domain.PlanPremiumPlus:
			return 2
		case domain.PlanPremium:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return domain.NormalizePlanTier(b)
	}
	return domain.NormalizePlanTier(a)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func newInvoiceNumber(userID uuid.UUID) string {
	// Short, unique, SePay-safe (alphanumeric + underscore/hyphen).
	short := strings.ReplaceAll(userID.String(), "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("DD-%s-%d-%s",
		strings.ToUpper(short),
		time.Now().UTC().Unix(),
		strings.ToUpper(uuid.New().String()[:8]),
	)
}

// MapError converts usecase errors to (HTTP status, code, message) for handlers.
func MapError(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, ErrUnauthorizedIPN):
		return 401, "unauthorized", "invalid sepay secret"
	case errors.Is(err, ErrNotConfigured):
		return 503, "sepay_not_configured", "SePay is not configured"
	case errors.Is(err, ErrUnavailable):
		return 503, "service_unavailable", "payment service unavailable"
	case errors.Is(err, ErrInvalidRequest):
		return 400, "invalid_request", err.Error()
	case errors.Is(err, ErrOrderNotFound):
		return 404, "order_not_found", "payment order not found"
	case errors.Is(err, ErrAmountMismatch):
		return 409, "amount_mismatch", "paid amount does not cover order"
	case errors.Is(err, ErrInvalidUser):
		return 401, "unauthorized", "invalid user"
	default:
		return 500, "payment_failed", "payment processing failed"
	}
}
