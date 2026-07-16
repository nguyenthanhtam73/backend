package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SkinCheckWriter creates skin checks and related AI analysis rows.
type SkinCheckWriter interface {
	CreateWithAnalysis(ctx context.Context, check *domain.SkinCheck, analysis *domain.SkinAnalysis) error
}

// SkinCheckReader loads checks for handlers and background jobs.
type SkinCheckReader interface {
	GetByIDForOwner(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*domain.SkinCheck, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.SkinCheck, error)
}

// SkinAnalysisSaver persists analysis updates from the AI worker.
type SkinAnalysisSaver interface {
	SaveAnalysis(ctx context.Context, a *domain.SkinAnalysis) error
}

// GormSkinCheckRepository implements skin check persistence.
type GormSkinCheckRepository struct {
	db *gorm.DB
}

// NewSkinCheckRepository returns a repository backed by GORM.
func NewSkinCheckRepository(db *gorm.DB) *GormSkinCheckRepository {
	return &GormSkinCheckRepository{db: db}
}

func (r *GormSkinCheckRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// CreateWithAnalysis persists skin check and pending analysis atomically.
//
// When ctx already carries a transaction (see repository.WithTx), work joins
// that transaction so callers can commit SkinCheck + Streak together. Otherwise
// a dedicated transaction is opened for just this write.
func (r *GormSkinCheckRepository) CreateWithAnalysis(ctx context.Context, check *domain.SkinCheck, analysis *domain.SkinAnalysis) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	run := func(tx *gorm.DB) error {
		if err := tx.Create(check).Error; err != nil {
			return err
		}
		analysis.SkinCheckID = check.ID
		if err := tx.Create(analysis).Error; err != nil {
			return err
		}
		return nil
	}
	if existing := TxFromContext(ctx); existing != nil {
		return run(existing)
	}
	return db.WithContext(ctx).Transaction(run)
}

// GetByIDForOwner returns a check with analysis if owned by ownerID.
func (r *GormSkinCheckRepository) GetByIDForOwner(ctx context.Context, id uuid.UUID, ownerID uuid.UUID) (*domain.SkinCheck, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var s domain.SkinCheck
	tx := db.WithContext(ctx).Preload("Analysis").Where("id = ? AND user_id = ?", id, ownerID).First(&s)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &s, nil
}

// ListRecentForCoach returns recent check-ins for the AI coach (minimal columns, excludes excludeID).
// Used to personalize tone and spot trends without re-loading full photo URLs.
func (r *GormSkinCheckRepository) ListRecentForCoach(ctx context.Context, userID uuid.UUID, excludeID uuid.UUID, limit int) ([]domain.SkinCheck, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	q := db.WithContext(ctx).
		Select("id", "check_date", "conditions", "symptoms", "title", "user_note", "created_at").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit)
	if excludeID != uuid.Nil {
		q = q.Where("id <> ?", excludeID)
	}
	var rows []domain.SkinCheck
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListRecentWithAnalysis returns recent check-ins WITH their AI analysis row
// preloaded so the user-memory builder can show "previous AI line" hints to
// the coach. Compared to ListRecentForCoach this loads a few more columns
// (image URLs for photo count, all summary fields on Analysis) so the model
// can see what it told the user last time.
//
// limit is bounded to [1, 10]; pass 0 for the default (6).
func (r *GormSkinCheckRepository) ListRecentWithAnalysis(ctx context.Context, userID uuid.UUID, excludeID uuid.UUID, limit int) ([]domain.SkinCheck, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 6
	}
	if limit > 10 {
		limit = 10
	}
	q := db.WithContext(ctx).
		Preload("Analysis").
		Select("id", "user_id", "check_date", "conditions", "symptoms", "title", "user_note", "image_urls", "created_at").
		Where("user_id = ?", userID).
		Order("check_date DESC, created_at DESC").
		Limit(limit)
	if excludeID != uuid.Nil {
		q = q.Where("id <> ?", excludeID)
	}
	var rows []domain.SkinCheck
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetByID loads a check by primary key with Analysis preloaded.
func (r *GormSkinCheckRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.SkinCheck, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var s domain.SkinCheck
	tx := db.WithContext(ctx).Preload("Analysis").Where("id = ?", id).First(&s)
	if tx.Error != nil {
		if errors.Is(tx.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, tx.Error
	}
	return &s, nil
}

// CountForUser returns the total number of skin checks the user has
// recorded (not soft-deleted). Used by the user-memory builder to decide
// whether to emit a monthly digest of older history.
func (r *GormSkinCheckRepository) CountForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return 0, err
	}
	conn := DBFromContext(ctx, db)
	var count int64
	if err := conn.
		Model(&domain.SkinCheck{}).
		Where("user_id = ?", userID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListDistinctCheckDates returns unique SkinCheck calendar days (UTC date) for
// a user, ascending. Used by streak reconcile to replay history.
func (r *GormSkinCheckRepository) ListDistinctCheckDates(
	ctx context.Context,
	userID uuid.UUID,
) ([]time.Time, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	conn := DBFromContext(ctx, db)
	var dates []time.Time
	if err := conn.
		Model(&domain.SkinCheck{}).
		Where("user_id = ?", userID).
		Distinct("check_date").
		Order("check_date ASC").
		Pluck("check_date", &dates).Error; err != nil {
		return nil, err
	}
	return dates, nil
}

// FirstCheckDate returns MIN(check_date) for the user, or (nil, nil) when they
// have never checked in. Used as the "app start" boundary for streak mini-history.
func (r *GormSkinCheckRepository) FirstCheckDate(
	ctx context.Context,
	userID uuid.UUID,
) (*time.Time, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	conn := DBFromContext(ctx, db)
	var first sql.NullTime
	err = conn.
		Model(&domain.SkinCheck{}).
		Select("MIN(check_date)").
		Where("user_id = ?", userID).
		Scan(&first).Error
	if err != nil {
		return nil, err
	}
	if !first.Valid {
		return nil, nil
	}
	t := first.Time.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &day, nil
}

// MonthlyDigestRow is a single bucket in the older-history digest: how many
// checks happened in this month and which tags/symptoms dominated.
type MonthlyDigestRow struct {
	Month       string   // "2025-04"
	CheckCount  int      // number of checks in this month
	TopTags     []string // up to 3 most-frequent conditions
	TopSymptoms []string // up to 3 most-frequent symptoms
}

// ListMonthlyDigest aggregates older check-ins (those NOT among the most
// recent `skipRecent` rows) into per-month buckets ordered newest month first.
//
// Implementation notes:
//   - We load the rows we need (date + conditions + symptoms only) and do
//     the grouping in Go. SQL grouping over JSONB arrays would be possible
//     but would lock us to Postgres-specific operators; in-Go is portable
//     and fast enough at our scale (a few hundred rows at most per user).
//   - `monthsLimit` caps how many month buckets we emit; pass 0 for default 6.
//   - When the user has < skipRecent + 1 rows, an empty slice is returned —
//     the builder upstream then simply omits the section.
func (r *GormSkinCheckRepository) ListMonthlyDigest(
	ctx context.Context,
	userID uuid.UUID,
	skipRecent int,
	monthsLimit int,
) ([]MonthlyDigestRow, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if skipRecent < 0 {
		skipRecent = 0
	}
	if monthsLimit <= 0 {
		monthsLimit = 6
	}
	if monthsLimit > 24 {
		monthsLimit = 24
	}

	var rows []domain.SkinCheck
	q := db.WithContext(ctx).
		Select("check_date", "conditions", "symptoms").
		Where("user_id = ?", userID).
		Order("check_date DESC, created_at DESC").
		Offset(skipRecent).
		Limit(1000)
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	type bucket struct {
		count    int
		tagCount map[string]int
		symCount map[string]int
	}
	buckets := make(map[string]*bucket)
	order := make([]string, 0, monthsLimit+4)

	for _, c := range rows {
		month := c.CheckDate.UTC().Format("2006-01")
		b, ok := buckets[month]
		if !ok {
			b = &bucket{tagCount: map[string]int{}, symCount: map[string]int{}}
			buckets[month] = b
			order = append(order, month)
		}
		b.count++
		tags, _ := dto.DecodeStringSlice(c.Conditions)
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			b.tagCount[t]++
		}
		syms, _ := dto.DecodeStringSlice(c.Symptoms)
		for _, s := range syms {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			b.symCount[s]++
		}
	}

	if len(order) > monthsLimit {
		order = order[:monthsLimit]
	}
	out := make([]MonthlyDigestRow, 0, len(order))
	for _, m := range order {
		b := buckets[m]
		out = append(out, MonthlyDigestRow{
			Month:       m,
			CheckCount:  b.count,
			TopTags:     topNKeys(b.tagCount, 3),
			TopSymptoms: topNKeys(b.symCount, 3),
		})
	}
	return out, nil
}

// topNKeys returns the top-N keys of a count map, ordered by descending count
// then by key (alphabetical) for stability across calls.
func topNKeys(m map[string]int, n int) []string {
	if len(m) == 0 || n <= 0 {
		return nil
	}
	type kv struct {
		k string
		v int
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		items = append(items, kv{k, v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	if n > len(items) {
		n = len(items)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, items[i].k)
	}
	return out
}

// ListForOwner returns the user's skin check-ins ordered by check_date DESC (most
// recent first) with the AI analysis preloaded. Used by the Progress Timeline page.
//
//   - If `since` is the zero time, no lower-bound filter is applied (return all history).
//   - `limit` caps the result; pass 0 or a negative number for "no cap" up to a hard 500 ceiling.
//
// We preload Analysis once instead of N+1 queries so the timeline renders fast even
// with hundreds of entries.
func (r *GormSkinCheckRepository) ListForOwner(ctx context.Context, userID uuid.UUID, since time.Time, limit int) ([]domain.SkinCheck, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := db.WithContext(ctx).
		Preload("Analysis").
		Where("user_id = ?", userID).
		Order("check_date DESC, created_at DESC").
		Limit(limit)
	if !since.IsZero() {
		q = q.Where("check_date >= ?", since)
	}
	var rows []domain.SkinCheck
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SaveAnalysis updates an existing analysis row.
func (r *GormSkinCheckRepository) SaveAnalysis(ctx context.Context, a *domain.SkinAnalysis) error {
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Save(a).Error
}
