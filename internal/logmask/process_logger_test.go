package logmask

import (
	"bytes"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestProcessLoggerLogPrintfNoDeadlockAndMasksEmail installs the same logger
// main uses, then calls log.Printf. slog.SetDefault routes the standard log
// package into that handler; the call must return and the address must be masked.
func TestProcessLoggerLogPrintfNoDeadlockAndMasksEmail(t *testing.T) {
	const (
		raw    = "test.user@gmail.com"
		masked = "t***@gmail.com"
	)

	prev := slog.Default()
	flags := log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetFlags(flags)
		log.SetOutput(os.Stderr)
	})

	var buf bytes.Buffer
	slog.SetDefault(NewProcessLogger(&buf))

	done := make(chan struct{})
	go func() {
		log.Printf("user %s", raw)
		slog.Info("auth", "email", raw)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("log.Printf did not return within 2s (slog default handler deadlock)")
	}

	got := buf.String()
	if strings.Contains(got, raw) {
		t.Fatalf("raw email in log output:\n%s", got)
	}
	if strings.Count(got, masked) < 2 {
		t.Fatalf("want masked email from log.Printf and from the slog attr, got:\n%s", got)
	}
}
