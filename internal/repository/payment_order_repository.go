package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PaymentOrderRepository persists SePay (and future PSP) checkout orders.
type PaymentOrderRepository struct {
	db *gorm.DB
}

// NewPaymentOrderRepository returns a payment-order repository.
func NewPaymentOrderRepository(db *gorm.DB) *PaymentOrderRepository {
	return &PaymentOrderRepository{db: db}
}

func (r *PaymentOrderRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Create inserts a new pending order.
func (r *PaymentOrderRepository) Create(ctx context.Context, order *domain.PaymentOrder) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Create(order).Error
}

// GetByInvoiceNumber loads an order by merchant invoice number.
func (r *PaymentOrderRepository) GetByInvoiceNumber(
	ctx context.Context,
	invoice string,
) (*domain.PaymentOrder, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var row domain.PaymentOrder
	tx := db.WithContext(ctx).Where("invoice_number = ?", invoice).First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// GetByID loads an order by primary key.
func (r *PaymentOrderRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*domain.PaymentOrder, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var row domain.PaymentOrder
	tx := db.WithContext(ctx).Where("id = ?", id).First(&row)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &row, nil
}

// MarkPaidParams updates an order to paid (idempotent when already paid).
type MarkPaidParams struct {
	InvoiceNumber      string
	SePayOrderID       string
	SePayTransactionID string
	RawWebhook         string
	PaidAt             time.Time
}

// MarkPaidTx sets status=paid inside an open transaction (SELECT FOR UPDATE on the order).
// Returns the locked row and whether it was already paid (idempotent IPN replay).
func (r *PaymentOrderRepository) MarkPaidTx(
	tx *gorm.DB,
	p MarkPaidParams,
) (order *domain.PaymentOrder, alreadyPaid bool, err error) {
	if tx == nil {
		return nil, false, fmt.Errorf("transaction required")
	}
	var row domain.PaymentOrder
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("invoice_number = ?", p.InvoiceNumber).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if row.Status == domain.PaymentPaid {
		return &row, true, nil
	}

	paidAt := p.PaidAt
	if paidAt.IsZero() {
		paidAt = time.Now().UTC()
	}
	updates := map[string]any{
		"status":                domain.PaymentPaid,
		"paid_at":               paidAt,
		"se_pay_order_id":       p.SePayOrderID,
		"se_pay_transaction_id": p.SePayTransactionID,
		"updated_at":            time.Now().UTC(),
	}
	if p.RawWebhook != "" {
		updates["raw_webhook"] = p.RawWebhook
	}
	if err := tx.Model(&row).Updates(updates).Error; err != nil {
		return nil, false, err
	}
	row.Status = domain.PaymentPaid
	row.PaidAt = &paidAt
	row.SePayOrderID = p.SePayOrderID
	row.SePayTransactionID = p.SePayTransactionID
	if p.RawWebhook != "" {
		row.RawWebhook = p.RawWebhook
	}
	return &row, false, nil
}

// UpdateStatusTx sets a non-paid terminal status (failed / cancelled).
func (r *PaymentOrderRepository) UpdateStatusTx(
	tx *gorm.DB,
	invoice string,
	status domain.PaymentOrderStatus,
	rawWebhook string,
) error {
	if tx == nil {
		return fmt.Errorf("transaction required")
	}
	updates := map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC(),
	}
	if rawWebhook != "" {
		updates["raw_webhook"] = rawWebhook
	}
	return tx.Model(&domain.PaymentOrder{}).
		Where("invoice_number = ? AND status = ?", invoice, domain.PaymentPending).
		Updates(updates).Error
}

// PaymentDayStats aggregates payment_orders created on [dayStart, dayEnd).
type PaymentDayStats struct {
	TotalCreated int64
	PaidCount    int64
	FailedCount  int64 // failed + cancelled + expired
	RevenueVND   int64 // sum of amount_vnd for paid rows
}

// PaymentOrderListFilter controls admin recent-payments listing.
type PaymentOrderListFilter struct {
	Status string // empty = all; otherwise domain.PaymentOrderStatus value
	Limit  int
	Offset int
}

// ListRecent returns newest payment orders (optional status filter).
func (r *PaymentOrderRepository) ListRecent(
	ctx context.Context,
	filter PaymentOrderListFilter,
) ([]domain.PaymentOrder, int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	q := db.WithContext(ctx).Model(&domain.PaymentOrder{})
	if s := strings.TrimSpace(filter.Status); s != "" {
		q = q.Where("status = ?", s)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []domain.PaymentOrder
	err = q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// AggregateCreatedBetween returns checkout stats for orders created in [from, to).
func (r *PaymentOrderRepository) AggregateCreatedBetween(
	ctx context.Context,
	from, to time.Time,
) (PaymentDayStats, error) {
	var zero PaymentDayStats
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	from, to = from.UTC(), to.UTC()

	var total int64
	if err := db.WithContext(ctx).Model(&domain.PaymentOrder{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Count(&total).Error; err != nil {
		return zero, err
	}

	var paid int64
	if err := db.WithContext(ctx).Model(&domain.PaymentOrder{}).
		Where("created_at >= ? AND created_at < ? AND status = ?", from, to, domain.PaymentPaid).
		Count(&paid).Error; err != nil {
		return zero, err
	}

	var failed int64
	if err := db.WithContext(ctx).Model(&domain.PaymentOrder{}).
		Where("created_at >= ? AND created_at < ? AND status IN ?",
			from, to,
			[]domain.PaymentOrderStatus{domain.PaymentFailed, domain.PaymentCancelled, domain.PaymentExpired},
		).Count(&failed).Error; err != nil {
		return zero, err
	}

	var revenue int64
	if err := db.WithContext(ctx).Model(&domain.PaymentOrder{}).
		Select("COALESCE(SUM(amount_vnd), 0)").
		Where("created_at >= ? AND created_at < ? AND status = ?", from, to, domain.PaymentPaid).
		Scan(&revenue).Error; err != nil {
		return zero, err
	}

	return PaymentDayStats{
		TotalCreated: total,
		PaidCount:    paid,
		FailedCount:  failed,
		RevenueVND:   revenue,
	}, nil
}
