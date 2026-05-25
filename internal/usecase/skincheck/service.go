// Package skincheck implements daily skin check-in use cases.
package skincheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/service/moderation"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput       = errors.New("invalid skin check payload")
	ErrModerationRejected = errors.New("content did not pass moderation")
	ErrDatabase           = errors.New("database error")
)

// CreateInput is built by the HTTP layer after files are saved on disk.
type CreateInput struct {
	Title           string
	UserNote        string
	Conditions      []string
	Symptoms        []string
	ClimateContext  json.RawMessage // optional JSON object from client (weather snapshot, etc.)
	EnvironmentNote string
	Visibility      domain.CheckVisibility
	AbsImagePaths   []string
	RelImagePaths   []string
}

// Service orchestrates skin checks and AI analysis jobs.
type Service struct {
	cfg      *config.Config
	checks   *repository.GormSkinCheckRepository
	mod      *moderation.Service
	analyzer *analysis.Service
}

// NewService wires dependencies.
func NewService(
	cfg *config.Config,
	checks *repository.GormSkinCheckRepository,
	mod *moderation.Service,
	analyzer *analysis.Service,
) *Service {
	return &Service{
		cfg:      cfg,
		checks:   checks,
		mod:      mod,
		analyzer: analyzer,
	}
}

// Create runs moderation, persists SkinCheck + pending SkinAnalysis, runs AI synchronously, then returns the saved check with coach feedback.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (dto.CreateSkinCheckResponse, error) {
	var zero dto.CreateSkinCheckResponse
	if s == nil || s.checks == nil {
		return zero, fmt.Errorf("%w: persistence unavailable", ErrDatabase)
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("%w: user id required", ErrInvalidInput)
	}
	if len(in.RelImagePaths) == 0 || len(in.AbsImagePaths) == 0 {
		return zero, fmt.Errorf("%w: at least one image required", ErrInvalidInput)
	}
	if len(in.RelImagePaths) != len(in.AbsImagePaths) {
		return zero, fmt.Errorf("%w: image path mismatch", ErrInvalidInput)
	}

	vis := in.Visibility
	if vis == "" {
		vis = domain.CheckVisibilityPrivate
	}
	if vis != domain.CheckVisibilityPrivate && vis != domain.CheckVisibilityPublic {
		return zero, fmt.Errorf("%w: invalid visibility", ErrInvalidInput)
	}

	if s.mod != nil {
		text := strings.TrimSpace(in.Title + "\n" + in.UserNote + "\n" + in.EnvironmentNote)
		if len(in.Conditions) > 0 {
			text += "\n" + strings.Join(in.Conditions, " ")
		}
		if len(in.Symptoms) > 0 {
			text += "\n" + strings.Join(in.Symptoms, " ")
		}
		if err := s.mod.CheckSkinContent(ctx, text, in.AbsImagePaths); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "moderation") ||
				strings.Contains(strings.ToLower(err.Error()), "flagged") {
				return zero, fmt.Errorf("%w: %v", ErrModerationRejected, err)
			}
			return zero, err
		}
	}

	imgJSON, err := json.Marshal(in.RelImagePaths)
	if err != nil {
		return zero, err
	}
	var condJSON json.RawMessage
	if len(in.Conditions) > 0 {
		condJSON, err = json.Marshal(in.Conditions)
		if err != nil {
			return zero, err
		}
	}
	var symJSON json.RawMessage
	if len(in.Symptoms) > 0 {
		symJSON, err = json.Marshal(in.Symptoms)
		if err != nil {
			return zero, err
		}
	}
	var climateJSON json.RawMessage
	if len(in.ClimateContext) > 0 {
		climateJSON = append(json.RawMessage(nil), in.ClimateContext...)
	}

	now := time.Now().UTC()
	checkD := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	check := &domain.SkinCheck{
		UserID:          userID,
		Title:           strings.TrimSpace(in.Title),
		UserNote:        strings.TrimSpace(in.UserNote),
		ImageURLs:       imgJSON,
		Conditions:      condJSON,
		Symptoms:        symJSON,
		ClimateContext:  climateJSON,
		EnvironmentNote: strings.TrimSpace(in.EnvironmentNote),
		Visibility:      vis,
		CheckDate:       checkD,
	}

	analysisRow := &domain.SkinAnalysis{
		Status: domain.AnalysisStatusPending,
	}

	if err := s.checks.CreateWithAnalysis(ctx, check, analysisRow); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrDatabase, err)
	}

	publicURLs := make([]string, 0, len(in.RelImagePaths))
	for _, rel := range in.RelImagePaths {
		slash := strings.ReplaceAll(rel, "\\", "/")
		clean := strings.TrimLeft(path.Join("uploads", slash), "/")
		publicURLs = append(publicURLs, "/"+clean)
	}

	// Synchronous AI so the client receives coach JSON in the same response (daily check-in core loop).
	// Detached timeout: do not tie to Fiber's request deadline (often too short for vision + Claude).
	if s.analyzer != nil {
		aiCtx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		_ = s.analyzer.Process(aiCtx, check.ID)
	}

	reloaded, err := s.checks.GetByID(ctx, check.ID)
	if err != nil || reloaded == nil {
		return zero, fmt.Errorf("%w: reload skin check after analysis", ErrDatabase)
	}

	return dto.NewCreateSkinCheckResponse(reloaded, reloaded.Analysis, publicURLs), nil
}
