package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Tree gate reasons. The HTTP layer copies these into error codes and into
// the can_water reason on a successful read.
const (
	TreeReasonNeedsSkinCheck = "needs_skin_check"
	TreeReasonAlreadyWatered = "already_watered"
	TreeReasonBloomed        = "bloomed"
	TreeReasonRoomEnded      = "room_ended"
)

var (
	ErrTreeNeedsSkinCheck  = errors.New("needs a skin check today")
	ErrTreeAlreadyWatered  = errors.New("already watered today")
	ErrTreeBloomed         = errors.New("the tree has bloomed")
	ErrTreeTypeUnavailable = errors.New("this tree cannot be planted yet")
	ErrTreeTypeUnknown     = errors.New("unknown tree type")
	ErrTreeNotReal         = errors.New("only a real member can water")
	ErrTreeBadIdempotency  = errors.New("idempotency key is too long")
)

// TreeReward is one stage reward, for the ladder or for one member.
type TreeReward struct {
	Stage         int
	Name          string
	Coins         int
	ItemID        string
	ItemName      string
	PreviewOnly   bool
	RewardedCount int
}

// TreeState is the shared tree as one member should see it.
// Watered is display names only. Progress is the bar toward the next stage,
// including today's waters when today has not leveled yet.
// JustGranted is what this call paid the caller (empty on a plain read that
// did not level). Levels is every stage reached inside this call, in order.
type TreeState struct {
	Stage         int
	StageName     string
	TreeType      string
	TreeName      string
	Rarity        string
	ImageKey      string
	Waters        int
	Required      int
	Watered       []string
	Progress      float64
	CanWater      bool
	Reason        string
	Bloomed       bool
	Rewards       []TreeReward
	MyRewards     []TreeReward
	Balance       int64
	Already       bool
	JustGranted   []TreeReward
	UnseenRewards []TreeReward
	FreshWater    bool
	WatererName   string
	Levels        []int
	MyShard       *ShardView
	CanDraw       bool
	Draws         []BloomDraw
	BloomShards   map[uuid.UUID]ShardView
}

// TreeCatalogItem is one enabled type plus the name of its stage-7 piece.
type TreeCatalogItem struct {
	Key               string
	NameVI            string
	Rarity            string
	ImageKey          string
	SortOrder         int
	ExclusiveItemKey  string
	ExclusiveItemName string
	Enabled           bool
	Creatable         bool
	Revealed          bool
}

// RoomTreeRepository stores the shared tree, its waters, and its rewards.
type RoomTreeRepository struct {
	db *gorm.DB
}

// NewRoomTreeRepository returns the tree store.
func NewRoomTreeRepository(db *gorm.DB) *RoomTreeRepository {
	return &RoomTreeRepository{db: db}
}

func (r *RoomTreeRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// ListEnabled returns catalog rows the client may draw, lowest sort first.
func (r *RoomTreeRepository) ListEnabled(ctx context.Context) ([]TreeCatalogItem, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var rows []domain.TreeType
	if err := db.WithContext(ctx).Where("enabled = ?", true).Order("sort_order ASC, key ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	names, err := itemNames(db.WithContext(ctx), exclusiveKeys(rows))
	if err != nil {
		return nil, err
	}
	out := make([]TreeCatalogItem, len(rows))
	for i, row := range rows {
		out[i] = TreeCatalogItem{
			Key: row.Key, NameVI: row.NameVI, Rarity: row.Rarity, ImageKey: row.ImageKey,
			SortOrder: row.SortOrder, ExclusiveItemKey: row.ExclusiveItemKey,
			ExclusiveItemName: names[row.ExclusiveItemKey], Enabled: row.Enabled,
		}
	}
	return out, nil
}

// State reads the tree for a real member. A Vietnam day that has already
// ended is closed here, once, under the room lock. Any other read does not
// lock the room and does not write. An immediate level-up happens on water
// or on Sync, not on this read.
func (r *RoomTreeRepository) State(ctx context.Context, userID uuid.UUID, rawCode string, now time.Time) (TreeState, error) {
	var zero TreeState
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var room domain.MascotRoom
	err = db.WithContext(ctx).Where("code = ?", code).Take(&room).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, ErrMascotRoomNotFound
	}
	if err != nil {
		return zero, err
	}
	var tree domain.MascotRoomTree
	err = db.WithContext(ctx).Where("room_code = ?", code).Take(&tree).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
		tree = domain.MascotRoomTree{}
	}
	if err != nil {
		return zero, err
	}
	if needsDaySettle(room.CreatedAt, tree.SettledThrough, now) {
		return r.run(ctx, treeCall{userID: userID, now: now}, rawCode)
	}
	return r.readState(ctx, room, userID, now)
}

// MarkRewardsSeen stamps seen_at on this member's stage rewards.
// An empty stages list marks every unseen reward. Other members are unchanged.
func (r *RoomTreeRepository) MarkRewardsSeen(ctx context.Context, userID uuid.UUID, rawCode string, stages []int, now time.Time) (TreeState, error) {
	var zero TreeState
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var state TreeState
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var room domain.MascotRoom
		err := tx.Where("code = ?", code).Take(&room).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrMascotRoomNotFound
		}
		if err != nil {
			return err
		}
		member, real, err := callerMember(tx, code, userID)
		if err != nil {
			return err
		}
		if !member {
			return ErrMascotRoomNotMember
		}
		if !real {
			return ErrTreeNotReal
		}
		var tree domain.MascotRoomTree
		err = tx.Where("room_code = ?", code).Take(&tree).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		settled := ""
		if err == nil {
			settled = tree.SettledThrough
		}
		if needsDaySettle(room.CreatedAt, settled, now) {
			room, err = lockMascotRoom(tx, code)
			if err != nil {
				return err
			}
			if _, err = advanceRoom(tx, room, userID, now); err != nil {
				return err
			}
		}
		q := tx.Model(&domain.MascotRoomTreeReward{}).
			Where("room_code = ? AND user_id = ? AND seen_at IS NULL", code, userID)
		if len(stages) > 0 {
			q = q.Where("stage IN ?", stages)
		}
		if err := q.Update("seen_at", now.UTC()).Error; err != nil {
			return err
		}
		work, err := loadTreeWork(tx, room, now, false)
		if err != nil {
			return err
		}
		state, err = buildTreeState(tx, room, work, userID, now)
		return err
	})
	if err != nil {
		return zero, err
	}
	return state, nil
}

func (r *RoomTreeRepository) readState(ctx context.Context, room domain.MascotRoom, userID uuid.UUID, now time.Time) (TreeState, error) {
	var zero TreeState
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	tx := db.WithContext(ctx)
	member, real, err := callerMember(tx, room.Code, userID)
	if err != nil {
		return zero, err
	}
	if !member {
		return zero, ErrMascotRoomNotMember
	}
	if !real {
		return zero, ErrTreeNotReal
	}
	work, err := loadTreeWork(tx, room, now, false)
	if err != nil {
		return zero, err
	}
	return buildTreeState(tx, room, work, userID, now)
}

// Water records one real member's daily water. A full day levels immediately.
// The same idempotency key returns the first result and does not pay again.
// Day-close levels are committed before the water is accepted or refused, so
// a rejected tap does not roll back a stage the room already earned.
func (r *RoomTreeRepository) Water(ctx context.Context, userID uuid.UUID, rawCode, idempotencyKey string, now time.Time) (TreeState, error) {
	key := strings.TrimSpace(idempotencyKey)
	if len(key) > 80 {
		return TreeState{}, ErrTreeBadIdempotency
	}
	state, err := r.State(ctx, userID, rawCode, now)
	if err != nil {
		return TreeState{}, err
	}
	if !roomIsOpenForWater(state, key) {
		return state, waterGateError(state)
	}
	watered, err := r.insertWater(ctx, userID, rawCode, key, now)
	if err != nil {
		return state, err
	}
	watered.Levels = append(append([]int{}, state.Levels...), watered.Levels...)
	watered.JustGranted = append(append([]TreeReward{}, state.JustGranted...), watered.JustGranted...)
	if watered.MyShard == nil {
		watered.MyShard = state.MyShard
	}
	if len(watered.BloomShards) == 0 {
		watered.BloomShards = state.BloomShards
	}
	return watered, nil
}

// Sync closes days and applies a required-count drop after a membership change.
// It does not water. A missing room (the last member left) is not an error.
func (r *RoomTreeRepository) Sync(ctx context.Context, rawCode string, now time.Time) (TreeState, error) {
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return TreeState{}, nil
	}
	db, err := r.dbOrErr()
	if err != nil {
		return TreeState{}, err
	}
	var state TreeState
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room, err := lockMascotRoom(tx, code)
		if errors.Is(err, ErrMascotRoomNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		view, err := advanceRoom(tx, room, uuid.Nil, now)
		if err != nil {
			return err
		}
		state = view
		return nil
	})
	if err != nil {
		return TreeState{}, err
	}
	return state, nil
}

type treeCall struct {
	userID uuid.UUID
	now    time.Time
	water  bool
	key    string
}

func (r *RoomTreeRepository) run(ctx context.Context, call treeCall, rawCode string) (TreeState, error) {
	var zero TreeState
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	if call.userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var state TreeState
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room, err := lockMascotRoom(tx, code)
		if err != nil {
			return err
		}
		member, real, err := callerMember(tx, code, call.userID)
		if err != nil {
			return err
		}
		if !member {
			return ErrMascotRoomNotMember
		}
		if !real {
			return ErrTreeNotReal
		}
		view, err := advanceRoom(tx, room, call.userID, call.now)
		if err != nil {
			return err
		}
		state = view
		return nil
	})
	if err != nil {
		return zero, err
	}
	return state, nil
}

func (r *RoomTreeRepository) insertWater(ctx context.Context, userID uuid.UUID, rawCode, key string, now time.Time) (TreeState, error) {
	var zero TreeState
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	var state TreeState
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room, err := lockMascotRoom(tx, code)
		if err != nil {
			return err
		}
		if !roomIsActive(room, now) {
			return ErrMascotRoomEnded
		}
		member, real, err := callerMember(tx, code, userID)
		if err != nil {
			return err
		}
		if !member {
			return ErrMascotRoomNotMember
		}
		if !real {
			return ErrTreeNotReal
		}
		work, err := loadTreeWork(tx, room, now, true)
		if err != nil {
			return err
		}
		if err := takeWater(tx, room, work, userID, key, now); err != nil {
			return err
		}
		if work.already {
			view, err := buildTreeState(tx, room, work, userID, now)
			if err != nil {
				return err
			}
			view.Already = true
			state = view
			return nil
		}
		if err := levelIfFull(tx, room, work, now); err != nil {
			return err
		}
		if err := saveTree(tx, work, now); err != nil {
			return err
		}
		view, err := buildTreeState(tx, room, work, userID, now)
		if err != nil {
			return err
		}
		view.FreshWater = true
		state = view
		return nil
	})
	if err != nil {
		return zero, err
	}
	return state, nil
}

func roomIsOpenForWater(state TreeState, key string) bool {
	if state.Reason == "" {
		return true
	}
	// A retry of the accepted tap reads the first result, including after bloom.
	if key != "" && (state.Reason == TreeReasonAlreadyWatered || state.Reason == TreeReasonBloomed) {
		return true
	}
	return false
}

func waterGateError(state TreeState) error {
	switch state.Reason {
	case TreeReasonRoomEnded:
		return ErrMascotRoomEnded
	case TreeReasonBloomed:
		return ErrTreeBloomed
	case TreeReasonAlreadyWatered:
		return ErrTreeAlreadyWatered
	case TreeReasonNeedsSkinCheck:
		return ErrTreeNeedsSkinCheck
	default:
		return ErrTreeAlreadyWatered
	}
}

// advanceRoom closes finished Vietnam days and levels immediately when today's
// waters already cover the required count (a leave can do that).
func advanceRoom(tx *gorm.DB, room domain.MascotRoom, viewer uuid.UUID, now time.Time) (TreeState, error) {
	work, err := loadTreeWork(tx, room, now, true)
	if err != nil {
		return TreeState{}, err
	}
	if err := closePastDays(tx, room, work, now); err != nil {
		return TreeState{}, err
	}
	if err := levelIfFull(tx, room, work, now); err != nil {
		return TreeState{}, err
	}
	if err := saveTree(tx, work, now); err != nil {
		return TreeState{}, err
	}
	return buildTreeState(tx, room, work, viewer, now)
}

type treeWork struct {
	tree        domain.MascotRoomTree
	stints      []memberStint
	items       map[string]domain.MascotItem
	typ         domain.TreeType
	waters      []domain.MascotRoomTreeWater
	today       string
	levels      []int
	granted     []TreeReward
	bloomShards map[uuid.UUID]ShardView
	fresh       bool
	already     bool
	name        string
}

func loadTreeWork(tx *gorm.DB, room domain.MascotRoom, now time.Time, create bool) (*treeWork, error) {
	stints, err := loadStints(tx, room.Code)
	if err != nil {
		return nil, err
	}
	var tree domain.MascotRoomTree
	if create {
		tree, err = ensureTree(tx, room.Code, now)
	} else {
		err = tx.Where("room_code = ?", room.Code).Take(&tree).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
			tree = domain.MascotRoomTree{RoomCode: room.Code, Stage: 1, ProgressNum: 0, ProgressDen: 1}
		}
	}
	if err != nil {
		return nil, err
	}
	typ, err := loadTreeType(tx, room.TreeType)
	if err != nil {
		return nil, err
	}
	items, err := loadItemMap(tx)
	if err != nil {
		return nil, err
	}
	today := vnDateKey(now)
	waters, err := loadWaters(tx, room.Code, today)
	if err != nil {
		return nil, err
	}
	return &treeWork{
		tree: tree, stints: stints, items: items, typ: typ, waters: waters, today: today,
	}, nil
}

func closePastDays(tx *gorm.DB, room domain.MascotRoom, work *treeWork, now time.Time) error {
	start := vnMidnight(room.CreatedAt)
	if work.tree.SettledThrough != "" {
		settled, err := time.ParseInLocation("2006-01-02", work.tree.SettledThrough, streaktime.Location)
		if err != nil {
			return err
		}
		start = settled.Add(24 * time.Hour)
	}
	end := vnMidnight(now)
	for day := start; day.Before(end); day = day.Add(24 * time.Hour) {
		if err := closeOneDay(tx, room, work, day); err != nil {
			return err
		}
	}
	return nil
}

func closeOneDay(tx *gorm.DB, room domain.MascotRoom, work *treeWork, day time.Time) error {
	key := day.In(streaktime.Location).Format("2006-01-02")
	work.tree.SettledThrough = key
	if work.tree.Stage >= domain.TreeStageCount && work.tree.LeveledOn != key {
		return nil
	}
	waters, err := loadWaters(tx, room.Code, key)
	if err != nil {
		return err
	}
	required := requiredAt(work.stints, room.CreatedAt, day, day.Add(24*time.Hour), len(waters))
	banked := newFrac(work.tree.ProgressNum, work.tree.ProgressDen)
	if work.tree.LeveledOn == key {
		// The stage was taken during the day. Keep only the leftover.
		if required > 0 && len(waters) > 0 {
			banked = withToday(banked, len(waters), required)
			if banked.ge1() {
				banked = banked.sub1()
			}
			work.tree.ProgressNum = banked.n
			work.tree.ProgressDen = banked.d
		}
		return nil
	}
	if work.tree.Stage >= domain.TreeStageCount || required <= 0 || len(waters) == 0 {
		return nil
	}
	banked = withToday(banked, len(waters), required)
	if banked.ge1() {
		work.tree.Stage++
		banked = banked.sub1()
		work.tree.LeveledOn = key
		work.levels = append(work.levels, work.tree.Stage)
		granted, err := grantStage(tx, room, work, day.Add(24*time.Hour-time.Nanosecond), work.tree.Stage)
		if err != nil {
			return err
		}
		work.granted = append(work.granted, granted...)
	}
	work.tree.ProgressNum = banked.n
	work.tree.ProgressDen = banked.d
	return nil
}

func levelIfFull(tx *gorm.DB, room domain.MascotRoom, work *treeWork, now time.Time) error {
	if work.tree.Stage >= domain.TreeStageCount || work.tree.LeveledOn == work.today {
		return nil
	}
	required := requiredAt(work.stints, room.CreatedAt, vnMidnight(now), now, len(work.waters))
	banked := newFrac(work.tree.ProgressNum, work.tree.ProgressDen)
	combined := withToday(banked, len(work.waters), required)
	if !combined.ge1() {
		return nil
	}
	// One stage only. The start-of-day bank stays stored; the leftover is
	// written when this Vietnam day closes, and the bar shows it now.
	work.tree.Stage++
	work.tree.LeveledOn = work.today
	work.levels = append(work.levels, work.tree.Stage)
	granted, err := grantStage(tx, room, work, now, work.tree.Stage)
	if err != nil {
		return err
	}
	work.granted = append(work.granted, granted...)
	return nil
}

func takeWater(tx *gorm.DB, room domain.MascotRoom, work *treeWork, userID uuid.UUID, key string, now time.Time) error {
	var existing domain.MascotRoomTreeWater
	err := tx.Where("room_code = ? AND user_id = ? AND vn_date = ?", room.Code, userID, work.today).
		Take(&existing).Error
	if err == nil {
		if key != "" && key == existing.IdempotencyKey {
			work.already = true
			return nil
		}
		if work.tree.Stage >= domain.TreeStageCount {
			return ErrTreeBloomed
		}
		return ErrTreeAlreadyWatered
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if work.tree.Stage >= domain.TreeStageCount {
		return ErrTreeBloomed
	}
	checked, err := hasSkinCheck(tx, userID, now)
	if err != nil {
		return err
	}
	if !checked {
		return ErrTreeNeedsSkinCheck
	}
	row := domain.MascotRoomTreeWater{
		RoomCode: room.Code, UserID: userID, VNDate: work.today,
		IdempotencyKey: key, CreatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_code"}, {Name: "user_id"}, {Name: "vn_date"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTreeAlreadyWatered
	}
	work.fresh = true
	waters, err := loadWaters(tx, room.Code, work.today)
	if err != nil {
		return err
	}
	work.waters = waters
	return nil
}

func grantStage(tx *gorm.DB, room domain.MascotRoom, work *treeWork, at time.Time, stage int) ([]TreeReward, error) {
	spec, ok := domain.TreeStageByNumber(stage, work.typ.ExclusiveItemKey, work.items[work.typ.ExclusiveItemKey].Name)
	if !ok {
		return nil, fmt.Errorf("unknown tree stage %d", stage)
	}
	members := currentRealAt(work.stints, at)
	var granted []TreeReward
	for _, userID := range members {
		row := domain.MascotRoomTreeReward{
			RoomCode: room.Code, UserID: userID, Stage: stage,
			Coins: spec.Coins, ItemID: spec.ItemID, CreatedAt: at.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "room_code"}, {Name: "user_id"}, {Name: "stage"},
			},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		if spec.Coins > 0 {
			ledgerKey := fmt.Sprintf("tree:%s:%d:%d:%s", room.Code, room.CreatedAt.UTC().UnixNano(), stage, userID.String())
			if _, err := creditCoins(tx, userID, int64(spec.Coins), ledgerKey, domain.CoinReasonTreeStage, at); err != nil {
				return nil, err
			}
		}
		if spec.ItemID != "" {
			item, ok := work.items[spec.ItemID]
			if !ok {
				return nil, fmt.Errorf("tree item %s is not in the catalog", spec.ItemID)
			}
			if err := grantTreeItem(tx, userID, item, room.Code, at); err != nil {
				return nil, err
			}
		}
		granted = append(granted, TreeReward{
			Stage: stage, Name: spec.Name, Coins: spec.Coins, ItemID: spec.ItemID, ItemName: spec.ItemName,
			PreviewOnly: spec.ItemID != "" && work.items[spec.ItemID].PreviewOnly,
		})
	}
	if stage == domain.TreeStageCount {
		shards, err := grantBloomShards(tx, room, members, at)
		if err != nil {
			return nil, err
		}
		work.bloomShards = shards
	}
	return granted, nil
}

func grantTreeItem(tx *gorm.DB, userID uuid.UUID, item domain.MascotItem, roomCode string, now time.Time) error {
	code := roomCode
	row := domain.UserMascotItem{
		UserID: userID, ItemID: item.ID, Source: domain.MascotSourceRoomReward,
		SourceRoomID: &code, AcquiredAt: now.UTC(), PreviewOnly: item.PreviewOnly,
	}
	res := tx.Omit("User", "Item").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoNothing: true,
	}).Create(&row)
	return res.Error
}

func buildTreeState(tx *gorm.DB, room domain.MascotRoom, work *treeWork, viewer uuid.UUID, now time.Time) (TreeState, error) {
	today := work.today
	waters := work.waters
	if vnDateKey(now) == today {
		reloaded, err := loadWaters(tx, room.Code, today)
		if err != nil {
			return TreeState{}, err
		}
		waters = reloaded
		work.waters = reloaded
	}
	required := requiredAt(work.stints, room.CreatedAt, vnMidnight(now), now, len(waters))
	names, err := waterNames(tx, room.Code, waters)
	if err != nil {
		return TreeState{}, err
	}
	checked := false
	watered := false
	if viewer != uuid.Nil {
		checked, err = hasSkinCheck(tx, viewer, now)
		if err != nil {
			return TreeState{}, err
		}
		for _, row := range waters {
			if row.UserID == viewer {
				watered = true
			}
		}
	}
	ended := !roomIsActive(room, now)
	bloomed := work.tree.Stage >= domain.TreeStageCount
	can, reason := waterPermission(ended, bloomed, watered, checked)
	banked := newFrac(work.tree.ProgressNum, work.tree.ProgressDen)
	leveledToday := work.tree.LeveledOn == today
	ladder := domain.TreeStages(work.typ.ExclusiveItemKey, work.items[work.typ.ExclusiveItemKey].Name)
	rewards := make([]TreeReward, len(ladder))
	stageName := ""
	for i, row := range ladder {
		rewards[i] = TreeReward{
			Stage: row.Stage, Name: row.Name, Coins: row.Coins, ItemID: row.ItemID, ItemName: row.ItemName,
			PreviewOnly: row.ItemID != "" && work.items[row.ItemID].PreviewOnly,
		}
		if row.Stage == work.tree.Stage {
			stageName = row.Name
		}
	}
	counts, err := stageRewardCounts(tx, room.Code)
	if err != nil {
		return TreeState{}, err
	}
	for i := range rewards {
		rewards[i].RewardedCount = counts[rewards[i].Stage]
	}
	mine := []TreeReward{}
	var balance int64
	if viewer != uuid.Nil {
		mine, err = myRewards(tx, room.Code, viewer, rewards)
		if err != nil {
			return TreeState{}, err
		}
		balance, err = coinBalance(tx, viewer)
		if err != nil {
			return TreeState{}, err
		}
	}
	display := ""
	if viewer != uuid.Nil {
		display, err = memberDisplayName(tx, room.Code, viewer)
		if err != nil {
			return TreeState{}, err
		}
		work.name = display
	}
	just := []TreeReward{}
	if viewer != uuid.Nil {
		seenStage := map[int]struct{}{}
		for _, row := range work.granted {
			if _, seen := seenStage[row.Stage]; seen {
				continue
			}
			for _, member := range mine {
				if member.Stage == row.Stage {
					row.RewardedCount = counts[row.Stage]
					just = append(just, row)
					seenStage[row.Stage] = struct{}{}
					break
				}
			}
		}
	}
	if names == nil {
		names = []string{}
	}
	unseen := []TreeReward{}
	if viewer != uuid.Nil {
		unseen, err = unseenRewards(tx, room.Code, viewer, rewards)
		if err != nil {
			return TreeState{}, err
		}
	}
	canDraw, mineShard, draws, err := bloomBoard(tx, room.Code, viewer)
	if err != nil {
		return TreeState{}, err
	}
	return TreeState{
		Stage: work.tree.Stage, StageName: stageName,
		TreeType: work.typ.Key, TreeName: work.typ.NameVI, Rarity: work.typ.Rarity, ImageKey: work.typ.ImageKey,
		Waters: len(waters), Required: required, Watered: names,
		Progress: shownProgress(banked, len(waters), required, leveledToday),
		CanWater: can, Reason: reason, Bloomed: bloomed,
		Rewards: rewards, MyRewards: mine, Balance: balance,
		JustGranted: just, UnseenRewards: unseen, Levels: append([]int(nil), work.levels...),
		WatererName: display, BloomShards: work.bloomShards, MyShard: mineShard,
		CanDraw: canDraw, Draws: draws,
	}, nil
}

func waterPermission(ended, bloomed, watered, checked bool) (bool, string) {
	switch {
	case ended:
		return false, TreeReasonRoomEnded
	case bloomed:
		return false, TreeReasonBloomed
	case watered:
		return false, TreeReasonAlreadyWatered
	case !checked:
		return false, TreeReasonNeedsSkinCheck
	default:
		return true, ""
	}
}

func saveTree(tx *gorm.DB, work *treeWork, now time.Time) error {
	if work.tree.ProgressDen <= 0 {
		work.tree.ProgressDen = 1
	}
	return tx.Model(&domain.MascotRoomTree{}).Where("room_code = ?", work.tree.RoomCode).Updates(map[string]any{
		"stage":           work.tree.Stage,
		"progress_num":    work.tree.ProgressNum,
		"progress_den":    work.tree.ProgressDen,
		"leveled_on":      work.tree.LeveledOn,
		"settled_through": work.tree.SettledThrough,
		"updated_at":      now.UTC(),
	}).Error
}

func ensureTree(tx *gorm.DB, code string, now time.Time) (domain.MascotRoomTree, error) {
	row := domain.MascotRoomTree{
		RoomCode: code, Stage: 1, ProgressNum: 0, ProgressDen: 1, UpdatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_code"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return domain.MascotRoomTree{}, res.Error
	}
	var stored domain.MascotRoomTree
	if err := tx.Where("room_code = ?", code).Take(&stored).Error; err != nil {
		return domain.MascotRoomTree{}, err
	}
	if stored.ProgressDen <= 0 {
		stored.ProgressDen = 1
	}
	if stored.Stage <= 0 {
		stored.Stage = 1
	}
	return stored, nil
}

func loadTreeType(tx *gorm.DB, key string) (domain.TreeType, error) {
	if strings.TrimSpace(key) == "" {
		key = domain.TreeTypeLeaf
	}
	var row domain.TreeType
	err := tx.Where("key = ?", key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.TreeType{}, ErrTreeTypeUnknown
	}
	if err != nil {
		return domain.TreeType{}, err
	}
	return row, nil
}

func loadWaters(tx *gorm.DB, code, day string) ([]domain.MascotRoomTreeWater, error) {
	var rows []domain.MascotRoomTreeWater
	err := tx.Where("room_code = ? AND vn_date = ?", code, day).
		Order("created_at ASC, user_id ASC").Find(&rows).Error
	return rows, err
}

func hasSkinCheck(tx *gorm.DB, userID uuid.UUID, now time.Time) (bool, error) {
	var n int64
	err := tx.Model(&domain.SkinCheck{}).
		Where("user_id = ? AND check_date = ?", userID, streaktime.DateOf(now)).
		Count(&n).Error
	return n > 0, err
}

func callerMember(tx *gorm.DB, code string, userID uuid.UUID) (member, real bool, err error) {
	var row domain.MascotRoomMember
	err = tx.Where("room_code = ? AND user_id = ?", code, userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, domain.RealRoomMember(row.Kind), nil
}

type memberStint struct {
	userID uuid.UUID
	joined time.Time
	left   *time.Time
	kind   string
}

func loadStints(tx *gorm.DB, code string) ([]memberStint, error) {
	var members []domain.MascotRoomMember
	if err := tx.Where("room_code = ?", code).Find(&members).Error; err != nil {
		return nil, err
	}
	out := make([]memberStint, 0, len(members))
	for _, row := range members {
		out = append(out, memberStint{userID: row.UserID, joined: row.JoinedAt, kind: row.Kind})
	}
	var leaves []domain.MascotRoomMemberLeave
	if err := tx.Where("room_code = ?", code).Find(&leaves).Error; err != nil {
		return nil, err
	}
	for _, row := range leaves {
		left := row.LeftAt
		out = append(out, memberStint{userID: row.UserID, joined: row.JoinedAt, left: &left, kind: row.Kind})
	}
	return out, nil
}

// requiredAt is how many waters the day needs.
// On the room's creation day the lock is the real members present at creation,
// because nobody was in the room at 00:00. Later days lock at that midnight.
// Someone who joins after the lock does not raise it. Someone from that set
// who left before `until` lowers it, but not below the waters already recorded.
// A day that locked at zero stays at zero.
func requiredAt(stints []memberStint, created, midnight, until time.Time, waters int) int {
	anchor := dayAnchor(created, midnight)
	locked := presentReal(stints, anchor)
	if len(locked) == 0 {
		return 0
	}
	left := map[uuid.UUID]struct{}{}
	for _, stint := range stints {
		if _, ok := locked[stint.userID]; !ok || stint.left == nil || !domain.RealRoomMember(stint.kind) {
			continue
		}
		if stint.left.After(anchor) && !stint.left.After(until) {
			left[stint.userID] = struct{}{}
		}
	}
	still := len(locked) - len(left)
	if still < 0 {
		still = 0
	}
	if still < len(locked) && waters > still {
		return waters
	}
	return still
}

func presentReal(stints []memberStint, at time.Time) map[uuid.UUID]struct{} {
	out := map[uuid.UUID]struct{}{}
	for _, stint := range stints {
		if !domain.RealRoomMember(stint.kind) || !joinedBy(stint.joined, at) {
			continue
		}
		if stint.left != nil && !stint.left.After(at) {
			continue
		}
		out[stint.userID] = struct{}{}
	}
	return out
}

// joinedBy reports whether this member was already in the room at `at`.
// A join in the same clock second counts. created_at is truncated to a
// second, and an older owner row kept the untruncated JoinedAt.
func joinedBy(joined, at time.Time) bool {
	if !joined.After(at) {
		return true
	}
	return joined.Truncate(time.Second).Equal(at.Truncate(time.Second))
}

func currentRealAt(stints []memberStint, at time.Time) []uuid.UUID {
	seen := presentReal(stints, at)
	out := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	// Stable enough for tests: sort by string.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].String() < out[j-1].String(); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func waterNames(tx *gorm.DB, code string, waters []domain.MascotRoomTreeWater) ([]string, error) {
	if len(waters) == 0 {
		return []string{}, nil
	}
	ids := make([]uuid.UUID, len(waters))
	for i, row := range waters {
		ids[i] = row.UserID
	}
	var users []domain.User
	if err := tx.Select("id", "display_name").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]string{}
	for _, user := range users {
		byID[user.ID] = strings.TrimSpace(user.DisplayName)
	}
	var members []domain.MascotRoomMember
	if err := tx.Select("user_id", "nickname").Where("room_code = ? AND user_id IN ?", code, ids).Find(&members).Error; err != nil {
		return nil, err
	}
	nicks := map[uuid.UUID]string{}
	for _, member := range members {
		nicks[member.UserID] = strings.TrimSpace(member.Nickname)
	}
	out := make([]string, len(waters))
	for i, row := range waters {
		name := byID[row.UserID]
		if name == "" {
			name = nicks[row.UserID]
		}
		if name == "" {
			name = "Bạn"
		}
		out[i] = name
	}
	return out, nil
}

func memberDisplayName(tx *gorm.DB, code string, userID uuid.UUID) (string, error) {
	names, err := waterNames(tx, code, []domain.MascotRoomTreeWater{{UserID: userID}})
	if err != nil || len(names) == 0 {
		return "", err
	}
	return names[0], nil
}

func myRewards(tx *gorm.DB, code string, userID uuid.UUID, ladder []TreeReward) ([]TreeReward, error) {
	var rows []domain.MascotRoomTreeReward
	if err := tx.Where("room_code = ? AND user_id = ?", code, userID).Order("stage ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	byStage := map[int]TreeReward{}
	for _, row := range ladder {
		byStage[row.Stage] = row
	}
	out := make([]TreeReward, 0, len(rows))
	for _, row := range rows {
		spec := byStage[row.Stage]
		out = append(out, TreeReward{
			Stage: row.Stage, Name: spec.Name, Coins: row.Coins, ItemID: row.ItemID, ItemName: spec.ItemName,
			PreviewOnly: spec.PreviewOnly, RewardedCount: spec.RewardedCount,
		})
	}
	return out, nil
}

func stageRewardCounts(tx *gorm.DB, code string) (map[int]int, error) {
	type counted struct {
		Stage int
		N     int
	}
	var rows []counted
	err := tx.Model(&domain.MascotRoomTreeReward{}).
		Select("stage, COUNT(*) AS n").
		Where("room_code = ?", code).
		Group("stage").
		Scan(&rows).Error
	out := map[int]int{}
	for _, row := range rows {
		out[row.Stage] = row.N
	}
	return out, err
}

func unseenRewards(tx *gorm.DB, code string, userID uuid.UUID, ladder []TreeReward) ([]TreeReward, error) {
	var rows []domain.MascotRoomTreeReward
	if err := tx.Where("room_code = ? AND user_id = ? AND seen_at IS NULL", code, userID).
		Order("stage ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	byStage := map[int]TreeReward{}
	for _, row := range ladder {
		byStage[row.Stage] = row
	}
	out := make([]TreeReward, 0, len(rows))
	for _, row := range rows {
		spec := byStage[row.Stage]
		out = append(out, TreeReward{
			Stage: row.Stage, Name: spec.Name, Coins: row.Coins, ItemID: row.ItemID, ItemName: spec.ItemName,
			PreviewOnly: spec.PreviewOnly, RewardedCount: spec.RewardedCount,
		})
	}
	return out, nil
}

func loadItemMap(tx *gorm.DB) (map[string]domain.MascotItem, error) {
	var rows []domain.MascotItem
	if err := tx.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]domain.MascotItem, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func itemNames(tx *gorm.DB, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []domain.MascotItem
	if err := tx.Select("id", "name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}

func exclusiveKeys(rows []domain.TreeType) []string {
	out := make([]string, 0, len(rows))
	seen := map[string]struct{}{}
	for _, row := range rows {
		if row.ExclusiveItemKey == "" {
			continue
		}
		if _, ok := seen[row.ExclusiveItemKey]; ok {
			continue
		}
		seen[row.ExclusiveItemKey] = struct{}{}
		out = append(out, row.ExclusiveItemKey)
	}
	return out
}

func vnDateKey(t time.Time) string {
	return t.In(streaktime.Location).Format("2006-01-02")
}

func vnMidnight(t time.Time) time.Time {
	local := t.In(streaktime.Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, streaktime.Location)
}

type frac struct{ n, d int64 }

func newFrac(n, d int64) frac {
	if d < 0 {
		n, d = -n, -d
	}
	if d == 0 {
		return frac{0, 1}
	}
	if n < 0 {
		n = 0
	}
	g := gcdInt(n, d)
	return frac{n / g, d / g}
}

func (f frac) add(n, d int64) frac {
	if f.d <= 0 {
		f = frac{0, 1}
	}
	if d <= 0 {
		return f
	}
	return newFrac(f.n*d+n*f.d, f.d*d)
}

func (f frac) ge1() bool { return f.d > 0 && f.n >= f.d }

func (f frac) sub1() frac {
	if !f.ge1() {
		return f
	}
	return newFrac(f.n-f.d, f.d)
}

func (f frac) float() float64 {
	if f.d == 0 {
		return 0
	}
	return float64(f.n) / float64(f.d)
}

func shownProgress(banked frac, waters, required int, leveledToday bool) float64 {
	combined := withToday(banked, waters, required)
	if leveledToday && combined.ge1() {
		return combined.sub1().float()
	}
	return combined.float()
}

// withToday adds today's capped water fraction to the start-of-day bank.
func withToday(banked frac, waters, required int) frac {
	if required <= 0 || waters <= 0 {
		return banked
	}
	add := waters
	if add > required {
		add = required
	}
	return banked.add(int64(add), int64(required))
}

// dayAnchor is when the day's required count locks.
// The creation day locks at room creation. Every later day locks at 00:00 VN.
func dayAnchor(created, midnight time.Time) time.Time {
	if vnDateKey(created) == vnDateKey(midnight) && created.After(midnight) {
		return created
	}
	return midnight
}

// needsDaySettle reports whether a Vietnam day before today is still open.
func needsDaySettle(created time.Time, settledThrough string, now time.Time) bool {
	start := vnMidnight(created)
	if settledThrough != "" {
		settled, err := time.ParseInLocation("2006-01-02", settledThrough, streaktime.Location)
		if err != nil {
			return true
		}
		start = settled.Add(24 * time.Hour)
	}
	return start.Before(vnMidnight(now))
}

func gcdInt(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

// SeedTreeCatalog inserts missing tree wardrobe rows and tree_types.
// Existing rows are left alone, including a later edit to enabled.
func SeedTreeCatalog(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	for _, item := range domain.TreeCatalogItems() {
		var n int64
		if err := db.Model(&domain.MascotItem{}).Where("id = ?", item.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		row := item
		if err := db.Create(&row).Error; err != nil && !IsUniqueConflict(err) {
			return err
		}
	}
	for _, typ := range domain.TreeTypeSeed() {
		var n int64
		if err := db.Model(&domain.TreeType{}).Where("key = ?", typ.Key).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		row := typ
		if err := db.Create(&row).Error; err != nil && !IsUniqueConflict(err) {
			return err
		}
	}
	return nil
}

// validateCreatableTree allows leaf when it is an enabled catalog row.
// A known other type is refused until seeds exist. An unknown key is refused.
// Tests that have not created tree_types may still open a leaf room.
func validateCreatableTree(db *gorm.DB, key string) error {
	if db == nil {
		return fmt.Errorf("database not configured")
	}
	has := db.Migrator().HasTable(&domain.TreeType{})
	if !domain.TreeTypeCreatable(key) {
		if !has {
			return ErrTreeTypeUnavailable
		}
		var n int64
		if err := db.Model(&domain.TreeType{}).Where("key = ?", key).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrTreeTypeUnknown
		}
		return ErrTreeTypeUnavailable
	}
	if !has {
		return nil
	}
	var n int64
	if err := db.Model(&domain.TreeType{}).Where("key = ? AND enabled = ?", key, true).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrTreeTypeUnavailable
	}
	return nil
}

// recordMemberLeave stores one ended stint. A repeat of the same stint
// does not insert a second row.
func recordMemberLeave(tx *gorm.DB, member domain.MascotRoomMember, at time.Time) error {
	if tx == nil || member.UserID == uuid.Nil || member.RoomCode == "" {
		return nil
	}
	kind := member.Kind
	if !domain.RealRoomMember(kind) && strings.TrimSpace(kind) == "" {
		kind = domain.RoomMemberUser
	}
	if strings.TrimSpace(kind) == "" {
		kind = domain.RoomMemberUser
	}
	row := domain.MascotRoomMemberLeave{
		RoomCode: member.RoomCode,
		UserID:   member.UserID,
		JoinedAt: member.JoinedAt.UTC(),
		LeftAt:   at.UTC(),
		Kind:     kind,
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_code"}, {Name: "user_id"}, {Name: "joined_at"}},
		DoNothing: true,
	}).Create(&row)
	return res.Error
}

func recordMemberLeaves(tx *gorm.DB, members []domain.MascotRoomMember, at time.Time) error {
	for _, member := range members {
		if err := recordMemberLeave(tx, member, at); err != nil {
			return err
		}
	}
	return nil
}
