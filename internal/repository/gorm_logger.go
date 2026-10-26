package repository

import (
	"log"
	"os"
	"strings"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

const (
	// prodSQLSlowThreshold is how long a successful query must take before
	// production logs it. Errors are still logged. Override with
	// DADIARY_DATABASE_SLOW_THRESHOLD (Go duration, for example 500ms or 1s).
	prodSQLSlowThreshold = 500 * time.Millisecond
	// devSQLSlowThreshold matches GORM's historical default so local slow
	// queries stay highlighted while every statement is printed.
	devSQLSlowThreshold = 200 * time.Millisecond
)

// newGormLogger builds the process SQL logger.
//
// Verbose logs (every statement, with bound values such as emails and ids)
// are enabled only when DADIARY_ENV / config env is an explicit local value:
// development, dev, debug, or local. Production, empty, and any other value
// log slow queries and errors only, with placeholders instead of values.
func newGormLogger(env string) gormlogger.Interface {
	return gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormLoggerConfig(env, sqlSlowThreshold(env)),
	)
}

// gormLoggerConfig is the SQL log policy for env. slow <= 0 selects the
// default threshold for that policy.
func gormLoggerConfig(env string, slow time.Duration) gormlogger.Config {
	if verboseSQLLogs(env) {
		if slow <= 0 {
			slow = devSQLSlowThreshold
		}
		return gormlogger.Config{
			SlowThreshold:             slow,
			LogLevel:                  gormlogger.Info,
			IgnoreRecordNotFoundError: false,
			ParameterizedQueries:      false,
			Colorful:                  true,
		}
	}
	if slow <= 0 {
		slow = prodSQLSlowThreshold
	}
	return gormlogger.Config{
		SlowThreshold:             slow,
		LogLevel:                  gormlogger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	}
}

// verboseSQLLogs reports whether env is an explicit local/debug setting.
// Anything else, including production and unset, stays on the safe logger.
func verboseSQLLogs(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "development", "dev", "debug", "local":
		return true
	default:
		return false
	}
}

func sqlSlowThreshold(env string) time.Duration {
	fallback := prodSQLSlowThreshold
	if verboseSQLLogs(env) {
		fallback = devSQLSlowThreshold
	}
	raw := strings.TrimSpace(os.Getenv("DADIARY_DATABASE_SLOW_THRESHOLD"))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
