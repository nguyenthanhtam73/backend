package logmask

import (
	"context"
	"fmt"
	"log/slog"
)

// Handler wraps a slog.Handler and runs every message and attribute through Redact.
// Install it once as the process default so every slog call site is covered,
// including error values that embed an address.
type Handler struct {
	next slog.Handler
}

// NewHandler returns a redacting handler. next must be non-nil.
func NewHandler(next slog.Handler) *Handler {
	return &Handler{next: next}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = redactAttr(a)
	}
	return &Handler{next: h.next.WithAttrs(red)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	a.Value = redactValue(a.Value)
	return a
}

func redactValue(v slog.Value) slog.Value {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.StringValue(Redact(v.String()))
	case slog.KindGroup:
		attrs := v.Group()
		out := make([]slog.Attr, len(attrs))
		for i, a := range attrs {
			out[i] = redactAttr(a)
		}
		return slog.GroupValue(out...)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok && err != nil {
			return slog.StringValue(Redact(err.Error()))
		}
		text := fmt.Sprint(v.Any())
		red := Redact(text)
		if red != text {
			return slog.StringValue(red)
		}
		return v
	default:
		return v
	}
}
