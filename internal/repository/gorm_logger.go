package repository

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/dadiary/backend/internal/logmask"
	gormlog "gorm.io/gorm/logger"
)

// newRedactingGormLogger is the process logger for Postgres. Level and slow-query
// threshold match gorm's Default (Warn, 200ms). SQL text and driver errors are
// passed through logmask so a duplicate-email insert or a slow check-in write
// cannot print the address, password hash, note, or photo key.
func newRedactingGormLogger() gormlog.Interface {
	return newRedactingGormLoggerWith(log.New(os.Stdout, "\r\n", log.LstdFlags), gormlog.Warn, true)
}

func newRedactingGormLoggerWith(w gormlog.Writer, level gormlog.LogLevel, colorful bool) gormlog.Interface {
	base := gormlog.New(w, gormlog.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: false,
		Colorful:                  colorful,
	})
	return redactingGormLogger{base: base}
}

type redactingGormLogger struct {
	base gormlog.Interface
}

func (l redactingGormLogger) LogMode(level gormlog.LogLevel) gormlog.Interface {
	return redactingGormLogger{base: l.base.LogMode(level)}
}

func (l redactingGormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	l.base.Info(ctx, logmask.Redact(msg), redactArgs(data)...)
}

func (l redactingGormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	l.base.Warn(ctx, logmask.Redact(msg), redactArgs(data)...)
}

func (l redactingGormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	l.base.Error(ctx, logmask.Redact(msg), redactArgs(data)...)
}

func (l redactingGormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.base.Trace(ctx, begin, func() (string, int64) {
		sql, rows := fc()
		return logmask.RedactSQL(sql), rows
	}, redactErr(err))
}

func redactArgs(data []interface{}) []interface{} {
	if len(data) == 0 {
		return data
	}
	out := make([]interface{}, len(data))
	for i, arg := range data {
		switch v := arg.(type) {
		case string:
			out[i] = logmask.RedactSQL(v)
		case error:
			out[i] = redactErr(v)
		default:
			out[i] = arg
		}
	}
	return out
}

func redactErr(err error) error {
	if err == nil {
		return nil
	}
	msg := logmask.Redact(err.Error())
	if msg == err.Error() {
		return err
	}
	return redactedError{error: err, msg: msg}
}

// redactedError keeps errors.Is working (record-not-found still follows the
// same IgnoreRecordNotFoundError setting) while Error() is what gorm prints.
type redactedError struct {
	error
	msg string
}

func (e redactedError) Error() string { return e.msg }

func (e redactedError) Unwrap() error { return e.error }
