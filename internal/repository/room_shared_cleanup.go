package repository

import (
	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// deleteRoomSharedRows removes quest claims and nudges for rooms that are
// about to disappear. Missing tables are ignored so older test databases
// can still close a room.
func deleteRoomSharedRows(tx *gorm.DB, codes []string) error {
	if tx == nil || len(codes) == 0 {
		return nil
	}
	if tx.Migrator().HasTable(&domain.MascotRoomQuestClaim{}) {
		if err := tx.Where("room_code IN ?", codes).Delete(&domain.MascotRoomQuestClaim{}).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&domain.MascotRoomNudge{}) {
		if err := tx.Where("room_code IN ?", codes).Delete(&domain.MascotRoomNudge{}).Error; err != nil {
			return err
		}
	}
	return deleteRoomTreeRows(tx, codes)
}

func deleteRoomTreeRows(tx *gorm.DB, codes []string) error {
	if tx == nil || len(codes) == 0 {
		return nil
	}
	if err := revealPendingBlooms(tx, codes); err != nil {
		return err
	}
	models := []any{
		&domain.MascotRoomTreeReward{},
		&domain.MascotRoomTreeWater{},
		&domain.MascotRoomTree{},
		&domain.MascotRoomMemberLeave{},
		&domain.RoomBloomShard{},
	}
	for _, model := range models {
		if !tx.Migrator().HasTable(model) {
			continue
		}
		if err := tx.Where("room_code IN ?", codes).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

// deleteUserRoomSharedRows removes this account's chest claims, nudges,
// and coins. Claims in a room someone else still owns go away with the
// account. Wallet rows are not diary data; account deletion removes them
// so the user row can go.
func deleteUserRoomSharedRows(tx *gorm.DB, userID uuid.UUID) error {
	if tx == nil || userID == uuid.Nil {
		return nil
	}
	if tx.Migrator().HasTable(&domain.MascotRoomQuestClaim{}) {
		if err := tx.Where("user_id = ?", userID).Delete(&domain.MascotRoomQuestClaim{}).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&domain.MascotRoomNudge{}) {
		if err := tx.Where("sender_user_id = ? OR target_user_id = ?", userID, userID).Delete(&domain.MascotRoomNudge{}).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&domain.UserCoinLedger{}) {
		if err := tx.Where("user_id = ?", userID).Delete(&domain.UserCoinLedger{}).Error; err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&domain.UserCoinWallet{}) {
		if err := tx.Where("user_id = ?", userID).Delete(&domain.UserCoinWallet{}).Error; err != nil {
			return err
		}
	}
	for _, model := range []any{
		&domain.MascotRoomTreeReward{},
		&domain.MascotRoomTreeWater{},
		&domain.MascotRoomMemberLeave{},
		&domain.UserSeedBalance{},
		&domain.UserSeedLedger{},
		&domain.RoomBloomShard{},
		&domain.UserTreeReveal{},
	} {
		if !tx.Migrator().HasTable(model) {
			continue
		}
		if err := tx.Where("user_id = ?", userID).Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}
