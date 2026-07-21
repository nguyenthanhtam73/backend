package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

const (
	exportMaxSkinChecks = 200
	exportMaxRoutines   = 90
)

// ExportBundle loads a privacy-safe diary dump for GET /me/export.
func (r *UserDataRepository) ExportBundle(
	ctx context.Context,
	userID uuid.UUID,
) (dto.ExportUserDataResponse, error) {
	var out dto.ExportUserDataResponse
	db, err := r.dbOrErr()
	if err != nil {
		return out, err
	}
	if userID == uuid.Nil {
		return out, fmt.Errorf("user id required")
	}
	out.UserID = userID.String()
	out.SkinChecks = []dto.ExportSkinCheck{}
	out.Routines = []dto.ExportRoutineDay{}
	out.Wardrobe = []dto.ExportWardrobeItem{}

	var profile domain.SkinProfile
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&profile).Error; err == nil {
		var concerns []string
		_ = json.Unmarshal(profile.Concerns, &concerns)
		out.Profile = &dto.ExportSkinProfile{
			SkinType:         profile.SkinType,
			Concerns:         concerns,
			SensitivityNotes: profile.Notes,
		}
	}

	var streak domain.Streak
	if err := db.WithContext(ctx).Where("user_id = ?", userID).First(&streak).Error; err == nil {
		sr := dto.NewStreakResponse(&streak)
		out.Streak = &sr
	}

	var checks []domain.SkinCheck
	if err := db.WithContext(ctx).
		Preload("Analysis").
		Where("user_id = ?", userID).
		Order("check_date DESC").
		Limit(exportMaxSkinChecks).
		Find(&checks).Error; err != nil {
		return out, err
	}
	for i := range checks {
		c := &checks[i]
		conds, _ := dto.DecodeStringSlice(c.Conditions)
		syms, _ := dto.DecodeStringSlice(c.Symptoms)
		urls, _ := dto.DecodeStringSlice(c.ImageURLs)
		item := dto.ExportSkinCheck{
			ID:         c.ID.String(),
			CheckDate:  c.CheckDate.UTC().Format("2006-01-02"),
			Title:      c.Title,
			UserNote:   c.UserNote,
			Conditions: conds,
			Symptoms:   syms,
			ImageURLs:  urls,
		}
		if c.Analysis != nil {
			item.CoachNotes = c.Analysis.SummaryNotes
		}
		out.SkinChecks = append(out.SkinChecks, item)
	}

	var routines []domain.RoutineEntry
	since := time.Now().UTC().AddDate(0, 0, -exportMaxRoutines)
	if err := db.WithContext(ctx).
		Where("user_id = ? AND routine_date >= ?", userID, since).
		Order("routine_date DESC").
		Find(&routines).Error; err != nil {
		return out, err
	}
	for _, rt := range routines {
		var morning any
		var evening any
		_ = json.Unmarshal(rt.Morning, &morning)
		_ = json.Unmarshal(rt.Evening, &evening)
		out.Routines = append(out.Routines, dto.ExportRoutineDay{
			Date:    rt.RoutineDate.UTC().Format("2006-01-02"),
			Morning: morning,
			Evening: evening,
			Notes:   rt.Notes,
		})
	}

	var products []domain.SkincareProduct
	if err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&products).Error; err != nil {
		return out, err
	}
	for _, p := range products {
		item := dto.ExportWardrobeItem{
			Name:     p.Name,
			Brand:    p.Brand,
			Category: p.Category,
			Notes:    p.Notes,
		}
		if p.OpenedAt != nil {
			item.OpenedAt = p.OpenedAt.UTC().Format("2006-01-02")
		}
		out.Wardrobe = append(out.Wardrobe, item)
	}

	return out, nil
}
