package repository

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	gormlog "gorm.io/gorm/logger"
)

func TestRedactingGormLogger_RegisterSQLOmitsEmailAndHash(t *testing.T) {
	const email = "thao.nguyen+ads@gmail.com"
	var buf bytes.Buffer
	l := newRedactingGormLoggerWith(log.New(&buf, "", 0), gormlog.Error, false)
	driverErr := errors.New(`ERROR: duplicate key value violates unique constraint "users_email_key" (SQLSTATE 23505) DETAIL: Key (email)=(` + email + `) already exists.`)
	l.Trace(context.Background(), time.Now(), func() (string, int64) {
		return `INSERT INTO "users" ("email","password_hash") VALUES ('` + email + `','$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy')`, 0
	}, driverErr)

	got := buf.String()
	if strings.Contains(got, email) || strings.Contains(got, "thao.nguyen") || strings.Contains(got, "$2a$") {
		t.Fatalf("register log leaked:\n%s", got)
	}
	if !strings.Contains(got, "t***@gmail.com") && !strings.Contains(got, "'***'") {
		t.Fatalf("expected a masked address or redacted literal, got:\n%s", got)
	}
}
