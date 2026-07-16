package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

type txCtxKey struct{}

// WithTx returns a child context that carries an open GORM transaction.
// Repository helpers should prefer this handle over opening a nested tx.
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// TxFromContext returns the transaction bound by WithTx, or nil.
func TxFromContext(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return nil
	}
	tx, _ := ctx.Value(txCtxKey{}).(*gorm.DB)
	return tx
}

// DBFromContext returns the active transaction when present, otherwise fallback
// (usually the repository's root *gorm.DB) with the request context attached.
func DBFromContext(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}
	if fallback == nil {
		return nil
	}
	return fallback.WithContext(ctx)
}

// TxRunner runs work inside a single database transaction and exposes that
// transaction to repositories via context (see WithTx / DBFromContext).
type TxRunner interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// GormTxRunner is the default TxRunner backed by GORM.
type GormTxRunner struct {
	db *gorm.DB
}

// NewTxRunner constructs a TxRunner. db may be nil (WithinTransaction then no-ops badly —
// callers must only wire a live connection).
func NewTxRunner(db *gorm.DB) *GormTxRunner {
	return &GormTxRunner{db: db}
}

// WithinTransaction opens one transaction and injects it into ctx for nested repo calls.
func (r *GormTxRunner) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("database not configured")
	}
	if fn == nil {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithTx(ctx, tx))
	})
}
