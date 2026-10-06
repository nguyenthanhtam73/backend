package userdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeleteAccount_PhotosIgnoreCancelledRequest(t *testing.T) {
	quietPhotoRetries(t)
	store := &scriptedStore{blockFirst: make(chan struct{}), release: make(chan struct{})}
	svc, _, userID, _ := newPhotoAccount(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	svc.onPhotosDone = func() { close(finished) }

	returned := make(chan error, 1)
	go func() {
		returned <- svc.DeleteAccount(ctx, userID, "password1")
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DeleteAccount waited on object storage")
	}
	select {
	case <-store.blockFirst:
	case <-time.After(2 * time.Second):
		t.Fatal("photo delete did not start")
	}
	cancel()
	close(store.release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("photo delete did not finish")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.ctxErrs) == 0 {
		t.Fatal("no delete calls")
	}
	for _, err := range store.ctxErrs {
		if err != nil {
			t.Fatalf("photo delete used a cancelled context: %v", err)
		}
	}
}

func TestDeleteAccount_PhotoDeleteRetriesThenSucceeds(t *testing.T) {
	quietPhotoRetries(t)
	store := &scriptedStore{failTimes: 2}
	svc, db, userID, key := newPhotoAccount(t, store)
	finished := make(chan struct{})
	svc.onPhotosDone = func() { close(finished) }
	if err := svc.DeleteAccount(context.Background(), userID, "password1"); err != nil {
		t.Fatal(err)
	}
	waitPhotos(t, finished)
	store.mu.Lock()
	calls := store.calls
	store.mu.Unlock()
	if calls != 4 {
		t.Fatalf("delete calls=%d want 4 (two failures, then the key, then the legacy prefix)", calls)
	}
	var n int64
	if err := db.Model(&domain.AccountDeleteOrphanKey{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("orphan rows=%d", n)
	}
	if !store.deleted(key) {
		t.Fatalf("key %s was not deleted", key)
	}
}

func TestDeleteAccount_PhotoDeleteLogsOrphanKey(t *testing.T) {
	quietPhotoRetries(t)
	store := &scriptedStore{failTimes: 20}
	svc, db, userID, key := newPhotoAccount(t, store)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	finished := make(chan struct{})
	svc.onPhotosDone = func() { close(finished) }
	if err := svc.DeleteAccount(context.Background(), userID, "password1"); err != nil {
		t.Fatal(err)
	}
	waitPhotos(t, finished)
	if !strings.Contains(buf.String(), "account_delete_orphan_key="+key) {
		t.Fatalf("log missing orphan prefix:\n%s", buf.String())
	}
	var rows []domain.AccountDeleteOrphanKey
	if err := db.Where("object_key = ?", key).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("orphan rows=%d", len(rows))
	}
	if rows[0].UserIDHash == "" || strings.Contains(rows[0].UserIDHash, userID.String()) {
		t.Fatalf("hash=%q", rows[0].UserIDHash)
	}
	if !strings.Contains(rows[0].LastError, "storage down") {
		t.Fatalf("last error=%q", rows[0].LastError)
	}
}

func quietPhotoRetries(t *testing.T) {
	t.Helper()
	prev := append([]time.Duration(nil), photoDeleteDelays...)
	photoDeleteDelays = []time.Duration{0, 0}
	t.Cleanup(func() { photoDeleteDelays = prev })
}

func waitPhotos(t *testing.T, finished <-chan struct{}) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("photo delete did not finish")
	}
}

func newPhotoAccount(t *testing.T, store *scriptedStore) (*Service, *gorm.DB, uuid.UUID, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:acctphoto_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("password1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := &domain.User{
		Email:        uuid.NewString() + "@dadiary.test",
		Username:     "p" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		PasswordHash: string(hash),
		IsActive:     true,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	key := "2026/10/03/check-in/photo__" + user.ID.String() + "/face.jpg"
	images, err := json.Marshal([]string{key})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.SkinCheck{
		UserID:     user.ID,
		ImageURLs:  images,
		CheckDate:  time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		Visibility: domain.CheckVisibilityPrivate,
	}).Error; err != nil {
		t.Fatal(err)
	}
	return NewService(repository.NewUserDataRepository(db), store, nil, nil), db, user.ID, key
}

type scriptedStore struct {
	mu         sync.Mutex
	failTimes  int
	calls      int
	ctxErrs    []error
	keys       []string
	blockFirst chan struct{}
	release    chan struct{}
}

func (s *scriptedStore) Save(context.Context, string, []byte, string) error { return nil }
func (s *scriptedStore) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("missing")
}
func (s *scriptedStore) Driver() string   { return "fake" }
func (s *scriptedStore) LocalDir() string { return "" }

func (s *scriptedStore) DeletePrefix(ctx context.Context, prefix string) error {
	s.mu.Lock()
	s.calls++
	n := s.calls
	fail := s.failTimes > 0
	if fail {
		s.failTimes--
	} else {
		s.keys = append(s.keys, prefix)
	}
	s.mu.Unlock()
	if s.blockFirst != nil && n == 1 {
		close(s.blockFirst)
		<-s.release
	}
	// Snapshot before return. The caller cancels its timeout context
	// when DeletePrefix returns, which is not the request being cancelled.
	s.mu.Lock()
	s.ctxErrs = append(s.ctxErrs, ctx.Err())
	s.mu.Unlock()
	if fail {
		return errors.New("storage down")
	}
	return ctx.Err()
}

func (s *scriptedStore) deleted(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, got := range s.keys {
		if got == key {
			return true
		}
	}
	return false
}
