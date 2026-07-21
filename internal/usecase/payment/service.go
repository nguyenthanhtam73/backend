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
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// systemActorEmail is stored on plan_change_logs for SePay-driven upgrades.
const systemActorEmail = "sepay@system.dadiary"

// Service orchestrates SePay checkout creation and IPN fulfilment.
type Service struct {
	db     *gorm.DB
	cfg    *config.Config
	orders *repository.PaymentOrderRepository
	users  *repository.GormUserRepository
	logs   *repository.PlanChangeLogRepository
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

	slog.Info("payment: sepay checkout created",
		"user_id", userID.String(),
		"invoice", invoice,
		"plan_tier", string(tier),
		"interval", string(interval),
		"amount_vnd", amount,
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

// HandleSePayWebhook verifies auth, applies ORDER_PAID → plan upgrade (idempotent).
// Always designed so SePay can retry safely; returns nil when ack is OK.
func (s *Service) HandleSePayWebhook(
	ctx context.Context,
	secretHeader string,
	rawBody []byte,
) error {
	if err := s.ready(); err != nil {
		return err
	}
	if !VerifyIPNSecretKey(secretHeader, s.cfg.SePay.SecretKey) {
		return ErrUnauthorizedIPN
	}

	var payload IPNPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return fmt.Errorf("%w: invalid json", ErrInvalidRequest)
	}

	nt := strings.ToUpper(strings.TrimSpace(payload.NotificationType))
	// Acknowledge non-paid events without mutating plan.
	if nt != "" && nt != "ORDER_PAID" {
		slog.Info("payment: sepay ipn ignored",
			"notification_type", nt,
			"invoice", payload.Order.OrderInvoiceNumber,
		)
		return nil
	}

	invoice := strings.TrimSpace(payload.Order.OrderInvoiceNumber)
	if invoice == "" {
		return fmt.Errorf("%w: missing order_invoice_number", ErrInvalidRequest)
	}

	status := strings.ToUpper(strings.TrimSpace(payload.Order.OrderStatus))
	// CAPTURED / PAID / COMPLETED all mean money settled for one-time PURCHASE.
	paidOK := status == "CAPTURED" || status == "PAID" || status == "COMPLETED" || nt == "ORDER_PAID"
	if !paidOK {
		slog.Info("payment: sepay order not paid yet",
			"invoice", invoice,
			"order_status", status,
		)
		return nil
	}

	amountRaw := payload.Order.OrderAmount
	if amountRaw == "" {
		amountRaw = payload.Transaction.TransactionAmount
	}
	paidAmount, err := ParseAmountVND(amountRaw)
	if err != nil {
		return fmt.Errorf("%w: bad amount", ErrInvalidRequest)
	}

	existing, err := s.orders.GetByInvoiceNumber(ctx, invoice)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrOrderNotFound
	}
	// Idempotent fast-path.
	if existing.Status == domain.PaymentPaid {
		return nil
	}
	if paidAmount < existing.AmountVND {
		slog.Warn("payment: amount below order",
			"invoice", invoice,
			"expected", existing.AmountVND,
			"got", paidAmount,
		)
		return ErrAmountMismatch
	}

	return s.fulfillPaidOrder(ctx, existing, payload, string(rawBody))
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

		// 2) Lock user FOR UPDATE — serializes concurrent IPNs for different invoices
		//    on the same account (prevents Premium+ → Premium downgrade races).
		user, err := s.users.GetByIDForUpdateTx(tx, marked.UserID)
		if err != nil {
			return err
		}
		if user == nil {
			return ErrInvalidUser
		}

		fromStored := domain.NormalizePlanTier(user.PlanTier)
		fromEffective := domain.EffectivePlanTier(user, now)
		// Never downgrade via payment (e.g. Premium+ paying for Premium).
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
				ActorUserID: marked.UserID, // self-serve payment; no admin actor
				ActorEmail:  systemActorEmail,
				FromPlan:    fromStored,
				ToPlan:      to,
				Reason:      fmt.Sprintf("sepay:%s", marked.InvoiceNumber),
			}
			if err := s.logs.CreateTx(tx, &logRow); err != nil {
				return err
			}
		}

		slog.Info("payment: plan applied under lock",
			"user_id", marked.UserID.String(),
			"from", string(fromStored),
			"to", string(to),
			"expires_at", expires.Format(time.RFC3339),
			"invoice", marked.InvoiceNumber,
		)
		return nil
	})
	if err != nil {
		return err
	}

	slog.Info("payment: sepay fulfilled",
		"invoice", order.InvoiceNumber,
		"user_id", order.UserID.String(),
		"plan_tier", string(targetTier),
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
