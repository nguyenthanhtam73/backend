package repository

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestGormLoggerConfig_ProductionOmitsValues(t *testing.T) {
	for _, env := range []string{"", "production", "prod", "staging", "test", " Production "} {
		cfg := gormLoggerConfig(env, 0)
		if !cfg.ParameterizedQueries {
			t.Fatalf("%q: ParameterizedQueries = false", env)
		}
		if cfg.LogLevel != gormlogger.Warn {
			t.Fatalf("%q: LogLevel = %v, want Warn", env, cfg.LogLevel)
		}
		if !cfg.IgnoreRecordNotFoundError {
			t.Fatalf("%q: IgnoreRecordNotFoundError = false", env)
		}
		if cfg.SlowThreshold != prodSQLSlowThreshold {
			t.Fatalf("%q: SlowThreshold = %s, want %s", env, cfg.SlowThreshold, prodSQLSlowThreshold)
		}
		if cfg.Colorful {
			t.Fatalf("%q: Colorful = true", env)
		}
	}
}

func TestGormLoggerConfig_DevKeepsValues(t *testing.T) {
	for _, env := range []string{"development", "dev", "debug", "local", " Development "} {
		cfg := gormLoggerConfig(env, 0)
		if cfg.ParameterizedQueries {
			t.Fatalf("%q: ParameterizedQueries = true", env)
		}
		if cfg.LogLevel != gormlogger.Info {
			t.Fatalf("%q: LogLevel = %v, want Info", env, cfg.LogLevel)
		}
		if cfg.IgnoreRecordNotFoundError {
			t.Fatalf("%q: IgnoreRecordNotFoundError = true", env)
		}
		if cfg.SlowThreshold != devSQLSlowThreshold {
			t.Fatalf("%q: SlowThreshold = %s, want %s", env, cfg.SlowThreshold, devSQLSlowThreshold)
		}
	}
}

func TestGormLoggerConfig_CustomSlowThreshold(t *testing.T) {
	cfg := gormLoggerConfig("production", 2*time.Second)
	if cfg.SlowThreshold != 2*time.Second {
		t.Fatalf("SlowThreshold = %s", cfg.SlowThreshold)
	}
	if !cfg.ParameterizedQueries || cfg.LogLevel != gormlogger.Warn {
		t.Fatalf("custom threshold changed policy: %+v", cfg)
	}
}

func TestSQLSlowThreshold_EnvOverride(t *testing.T) {
	t.Setenv("DADIARY_DATABASE_SLOW_THRESHOLD", "750ms")
	if got := sqlSlowThreshold("production"); got != 750*time.Millisecond {
		t.Fatalf("production override = %s", got)
	}
	if got := sqlSlowThreshold("development"); got != 750*time.Millisecond {
		t.Fatalf("development override = %s", got)
	}

	t.Setenv("DADIARY_DATABASE_SLOW_THRESHOLD", "nope")
	if got := sqlSlowThreshold("production"); got != prodSQLSlowThreshold {
		t.Fatalf("invalid production fallback = %s", got)
	}
	if got := sqlSlowThreshold("development"); got != devSQLSlowThreshold {
		t.Fatalf("invalid development fallback = %s", got)
	}

	t.Setenv("DADIARY_DATABASE_SLOW_THRESHOLD", "0s")
	if got := sqlSlowThreshold(""); got != prodSQLSlowThreshold {
		t.Fatalf("non-positive fallback = %s", got)
	}
}

type sqlLogBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *sqlLogBuf) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format, args...)
	l.b.WriteByte('\n')
}

func (l *sqlLogBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *sqlLogBuf) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.Reset()
}

type sqlLogUser struct {
	ID    uint   `gorm:"primaryKey"`
	Email string `gorm:"size:255"`
}

func (sqlLogUser) TableName() string { return "sql_log_users" }

func openLoggedSQLite(t *testing.T, w *sqlLogBuf, cfg gormlogger.Config) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:gormlog_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: gormlogger.New(w, cfg),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&sqlLogUser{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w.Reset()
	return db
}

func TestProductionSQLLog_PlaceholdersOnly(t *testing.T) {
	w := &sqlLogBuf{}
	// 1ns marks every successful query slow so the Warn logger actually prints it.
	cfg := gormLoggerConfig("production", time.Nanosecond)
	db := openLoggedSQLite(t, w, cfg)

	email := "secret.person@example.com"
	if err := db.Create(&sqlLogUser{Email: email}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	got := w.String()
	if strings.Contains(got, email) {
		t.Fatalf("bound value logged: %s", got)
	}
	if !strings.Contains(got, "INSERT") || !strings.Contains(got, "?") {
		t.Fatalf("expected placeholder SQL, got: %s", got)
	}
}

func TestProductionSQLLog_SkipsFastSuccess(t *testing.T) {
	w := &sqlLogBuf{}
	db := openLoggedSQLite(t, w, gormLoggerConfig("production", prodSQLSlowThreshold))
	if err := db.Create(&sqlLogUser{Email: "fast@example.com"}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := w.String(); got != "" {
		t.Fatalf("fast success logged: %s", got)
	}

	w.Reset()
	var missing sqlLogUser
	err := db.Where("email = ?", "absent@example.com").First(&missing).Error
	if err == nil {
		t.Fatal("expected record not found")
	}
	if got := w.String(); got != "" {
		t.Fatalf("record not found was logged: %s", got)
	}

	w.Reset()
	email := "secret.person@example.com"
	err = db.Exec("SELECT * FROM missing_sql_log WHERE email = ?", email).Error
	if err == nil {
		t.Fatal("expected query error")
	}
	got := w.String()
	if strings.Contains(got, email) {
		t.Fatalf("error log included bound email: %s", got)
	}
	if !strings.Contains(got, "missing_sql_log") || !strings.Contains(got, "?") {
		t.Fatalf("expected placeholder SQL on error, got: %s", got)
	}
}

func TestDevSQLLog_KeepsBoundValues(t *testing.T) {
	w := &sqlLogBuf{}
	db := openLoggedSQLite(t, w, gormLoggerConfig("development", 0))
	email := "local.dev@example.com"
	if err := db.Create(&sqlLogUser{Email: email}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	got := w.String()
	if !strings.Contains(got, email) {
		t.Fatalf("dev log dropped bound email: %s", got)
	}
}
