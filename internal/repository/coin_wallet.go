package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func creditCoins(tx *gorm.DB, userID uuid.UUID, amount int64, key, reason string, now time.Time) (int64, error) {
	if tx == nil || userID == uuid.Nil {
		return 0, fmt.Errorf("coin credit requires a user")
	}
	if amount <= 0 || key == "" {
		return 0, fmt.Errorf("coin credit requires a positive amount")
	}
	row := domain.UserCoinLedger{
		ID:        key,
		UserID:    userID,
		Amount:    amount,
		Reason:    reason,
		CreatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected > 0 {
		wallet := domain.UserCoinWallet{
			UserID:    userID,
			Balance:   amount,
			UpdatedAt: now.UTC(),
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"balance":    gorm.Expr("user_coin_wallets.balance + ?", amount),
				"updated_at": now.UTC(),
			}),
		}).Create(&wallet).Error
		if err != nil {
			return 0, err
		}
	}
	return coinBalance(tx, userID)
}

func coinBalance(tx *gorm.DB, userID uuid.UUID) (int64, error) {
	var row domain.UserCoinWallet
	err := tx.Where("user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.Balance, nil
}
