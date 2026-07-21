package repository

import (
	"context"
	"errors"
	"fmt"
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
