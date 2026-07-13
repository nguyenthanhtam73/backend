// Package repository will hold persistence implementations (GORM, caches).
package repository

import (
	"fmt"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewPostgres opens a GORM connection using the configured URL.
func NewPostgres(cfg *config.Config) (*gorm.DB, error) {
	if cfg.Database.URL == "" {
		return nil, fmt.Errorf("database url is empty")
	}

	db, err := gorm.Open(postgres.Open(cfg.Database.URL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return db, nil
}

// AutoMigrate runs schema migrations for core domain models (dev/small deploys).
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.User{},
		&domain.SkinProfile{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
		&domain.AffiliateClick{},
		&domain.ProgressLog{},
		&domain.AIUserFeedback{},
		&domain.Feedback{},
		&domain.BetaSignup{},
		&domain.UsageEvent{},
	)
}
