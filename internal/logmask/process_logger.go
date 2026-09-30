package logmask

import (
	"io"
	"log/slog"
	"os"
)

// NewProcessLogger builds the process logger the API installs at startup.
// The text handler is created here, then wrapped. Do not wrap
// slog.Default().Handler(): that handler writes through the standard log
// package, and slog.SetDefault redirects log back into the handler, so
// log.Printf deadlocks.
//
// w is the destination (os.Stdout in the API, a buffer in tests). Nil uses stdout.
// Level stays Info, matching slog's default handler.
func NewProcessLogger(w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	base := slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(NewHandler(base))
}
