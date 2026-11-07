package outageapology

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRun_DryRunCountsWithoutSending(t *testing.T) {
	db := openApologyDB(t)
	checks := repository.NewSkinCheckRepository(db)
	seedApologyUser(t, db, checks, "one@dadiary.vn", WindowStart.Add(time.Minute), domain.AnalysisStatusFailed)
	seedApologyUser(t, db, checks, "two@example.com", WindowStart.Add(time.Minute), domain.AnalysisStatusFailed)
	done := seedApologyUser(t, db, checks, "three@dadiary.vn", WindowStart.Add(time.Minute), domain.AnalysisStatusCompleted)
	_ = done
	deleted := seedApologyUser(t, db, checks, "four@dadiary.vn", WindowStart.Add(time.Minute), domain.AnalysisStatusFailed)
	if err := db.Delete(&domain.User{}, "id = ?", deleted).Error; err != nil {
		t.Fatal(err)
	}

	mail := &fakeMailer{}
	svc := NewService(checks, repository.NewEmailSendReceiptRepository(db), mail, "https://dadiary.vn", []string{"admin@dadiary.vn"})
	svc.sleep = func(time.Duration) {}
	res, err := svc.Run(context.Background(), ModeDryRun, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Recipients != 1 || res.Sent != 0 || mail.attempts() != 0 {
		t.Fatalf("dry-run = %+v attempts=%d", res, mail.attempts())
	}
}

func TestRun_ApplyIsIdempotentAndStopsAfterFiveErrors(t *testing.T) {
	db := openApologyDB(t)
	checks := repository.NewSkinCheckRepository(db)
	receipts := repository.NewEmailSendReceiptRepository(db)
	for i := 0; i < 7; i++ {
		seedApologyUser(t, db, checks, "user"+string(rune('a'+i))+"@dadiary.vn", WindowStart.Add(time.Duration(i)*time.Minute), domain.AnalysisStatusFailed)
	}
	mail := &fakeMailer{err: errSend}
	svc := NewService(checks, receipts, mail, "https://dadiary.vn", nil)
	svc.sleep = func(time.Duration) {}
	res, err := svc.Run(context.Background(), ModeApply, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || res.Failed != 5 || res.Sent != 0 || mail.attempts() != 5 || res.Recipients != 7 {
		t.Fatalf("stop = %+v attempts=%d", res, mail.attempts())
	}

	mail.err = nil
	second, err := svc.Run(context.Background(), ModeApply, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sent != 7 || mail.attempts() != 12 {
		t.Fatalf("retry should send the seven released rows, got %+v attempts=%d", second, mail.attempts())
	}
	third, err := svc.Run(context.Background(), ModeApply, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if third.Sent != 0 || third.Skipped != 7 || mail.attempts() != 12 {
		t.Fatalf("third run should not send again, got %+v attempts=%d", third, mail.attempts())
	}
	var n int64
	if err := db.Model(&domain.EmailSendReceipt{}).Where("kind = ?", ReceiptKind).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("markers = %d, want 7", n)
	}
}

func TestRun_TestSendsOneHasResultMailToFirstAdmin(t *testing.T) {
	db := openApologyDB(t)
	checks := repository.NewSkinCheckRepository(db)
	seedApologyUser(t, db, checks, "person@dadiary.vn", WindowStart.Add(time.Minute), domain.AnalysisStatusFailed)
	mail := &fakeMailer{}
	svc := NewService(checks, repository.NewEmailSendReceiptRepository(db), mail, "https://dadiary.vn", []string{"", "first-admin@dadiary.vn", "second-admin@dadiary.vn"})
	res, err := svc.Run(context.Background(), ModeTest, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Test || res.Sent != 1 || mail.attempts() != 1 {
		t.Fatalf("test = %+v attempts=%d", res, mail.attempts())
	}
	sent := mail.messages()
	if len(sent) != 1 || sent[0].To != "first-admin@dadiary.vn" {
		t.Fatalf("test recipient count = %d", len(sent))
	}
	if sent[0].Subject != Subject || sent[0].Text == "" {
		t.Fatal("test mail missing subject")
	}
	if !strings.Contains(sent[0].Text, HasResultSentence) || !strings.Contains(sent[0].Text, "/skin-checks/outage-apology-test") {
		t.Fatalf("test mail is not the has-result variant:\n%s", sent[0].Text)
	}
	var n int64
	if err := db.Model(&domain.EmailSendReceipt{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("test mode persisted %d markers", n)
	}
}

var errSend = errString("send failed")

type errString string

func (e errString) Error() string { return string(e) }

type fakeMailer struct {
	mu   sync.Mutex
	err  error
	sent []email.Message
}

func (f *fakeMailer) Configured() bool { return true }

func (f *fakeMailer) Send(_ context.Context, msg email.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	if f.err != nil {
		return "", f.err
	}
	return "re_test", nil
}

func (f *fakeMailer) attempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeMailer) messages() []email.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]email.Message, len(f.sent))
	copy(out, f.sent)
	return out
}

func openApologyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:apology_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}, &domain.SkinAnalysis{}, &domain.EmailSendReceipt{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedApologyUser(t *testing.T, db *gorm.DB, checks *repository.GormSkinCheckRepository, addr string, created time.Time, status domain.AnalysisStatus) uuid.UUID {
	t.Helper()
	user := &domain.User{
		Email:    addr,
		Username: "u" + uuid.NewString()[:8],
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	check := &domain.SkinCheck{
		UserID:     user.ID,
		ImageURLs:  []byte(`["checks/a.jpg"]`),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  created,
		CreatedAt:  created,
	}
	row := &domain.SkinAnalysis{Status: status}
	if err := checks.CreateWithAnalysis(context.Background(), check, row); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE skin_checks SET created_at = ? WHERE id = ?`, created, check.ID).Error; err != nil {
		t.Fatal(err)
	}
	return user.ID
}
