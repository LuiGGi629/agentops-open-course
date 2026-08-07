package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// The keys the trace correlation is published under.
//
// Snake case, not the dotted OpenTelemetry attribute names, because these are
// read by Loki's derived fields and by a human scanning a console line — and
// Grafana's shipped Loki-to-Tempo link is configured against exactly these two.
// The OTLP log path does not need them: the log SDK stamps the trace and span
// identifiers onto the exported record itself, from the same context.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// OtelHandler stamps the active span's trace and span identifiers onto every
// record before passing it to the handler it wraps.
//
// This is the join key for the whole observability chapter. Tempo's
// trace-to-logs jumps from a span to the log lines of that trace, and Loki's
// derived fields jump back from a log line to the trace, and neither direction
// resolves unless the identifiers are on the line. Without this type the OTLP
// spans and the log stream are two unrelated piles of data.
type OtelHandler struct {
	slog.Handler
}

// NewOtelHandler wraps inner so every record it receives carries the active
// trace correlation.
func NewOtelHandler(inner slog.Handler) *OtelHandler {
	return &OtelHandler{Handler: inner}
}

// Handle adds the trace correlation, when there is one, and delegates.
func (h *OtelHandler) Handle(ctx context.Context, record slog.Record) error {
	// IsValid, not IsRecording. A span context extracted from an inbound
	// traceparent header is valid but not recording in this process, and those
	// are precisely the requests where correlation earns its keep: the trace
	// exists, it was sampled by the caller, and this process's log line is the
	// only evidence of what happened here. Gating on IsRecording — as the
	// generic reference handler does — would drop the correlation on every
	// propagated request.
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String(TraceIDKey, spanContext.TraceID().String()),
			slog.String(SpanIDKey, spanContext.SpanID().String()),
		)
	}
	if err := h.Handler.Handle(ctx, record); err != nil {
		return fmt.Errorf("handling a trace-correlated log record: %w", err)
	}
	return nil
}

// WithAttrs returns a handler that keeps stamping the correlation.
//
// Overriding this and WithGroup is not optional: the embedded handler's own
// implementations return the *inner* handler, so a single logger.With() call
// would silently unwrap the correlation for the rest of that logger's life.
func (h *OtelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &OtelHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a handler that keeps stamping the correlation.
func (h *OtelHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		// slog's contract: an empty group name is a no-op, and forwarding it
		// would nest every later attribute under a nameless group.
		return h
	}
	return &OtelHandler{Handler: h.Handler.WithGroup(name)}
}

// MultiHandler fans one record out to several handlers.
//
// It is what lets the console keep the record exactly as it was written while
// the OTLP path sees a redacted, bounded copy. The two sinks must not share
// mutable state, so every handler gets its own clone of the record: slog.Record
// keeps its attributes in a shared backing array, and a handler that appends to
// its copy would otherwise corrupt what the next one reads.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler fans records out to every non-nil handler given.
//
// It refuses an empty set rather than returning a handler that silently
// discards everything: "the process logs nowhere" is a startup bug, and it is
// far cheaper to find here than in an incident.
func NewMultiHandler(handlers ...slog.Handler) (*MultiHandler, error) {
	kept := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			kept = append(kept, handler)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("a multi handler needs at least one destination")
	}
	return &MultiHandler{handlers: kept}, nil
}

// Enabled reports whether any destination wants this level.
func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle delivers the record to every destination that wants it.
//
// A failing destination does not stop the others and does not hide its failure:
// the errors are joined, so one broken exporter cannot make the console lose a
// line, and a broken console cannot make the export silently stop.
func (h *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var problems []error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}

// WithAttrs returns a handler whose destinations all carry attrs.
func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	derived := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		derived = append(derived, handler.WithAttrs(attrs))
	}
	return &MultiHandler{handlers: derived}
}

// WithGroup returns a handler whose destinations all carry the group.
func (h *MultiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	derived := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		derived = append(derived, handler.WithGroup(name))
	}
	return &MultiHandler{handlers: derived}
}

// Config is everything the logging plane needs from the rest of the runtime.
type Config struct {
	// Console receives every record exactly as it was written. Required: a
	// process with no local log is undiagnosable when the collector is the
	// thing that is broken.
	Console slog.Handler

	// Redact removes personal data and credentials from a value bound for
	// export. Required when ExportLogs is true. See [Redactor].
	Redact Redactor

	// Level bounds what reaches the OTLP path. Nil means slog.LevelInfo. The
	// console keeps whatever level its own handler was built with.
	Level slog.Leveler

	// ExportLogs turns the OTLP log path on. Wire it from [LogsConfigured]
	// rather than reading the environment here, so a caller decides once and a
	// test can decide differently without touching the process environment.
	ExportLogs bool
}

// NewHandler assembles the process's slog handler.
//
// The shape is: correlation on the outside, fan-out in the middle, the console
// and the redacting exporter as the two leaves.
//
//	OtelHandler → MultiHandler ─┬→ console (verbatim)
//	                            └→ export  (redacted, bounded, fail-closed)
//
// Correlation is outermost so both leaves see the identifiers. The redaction is
// on the export leaf only, so the console keeps the raw text an engineer needs
// while nothing sensitive leaves the process. That asymmetry is the whole point
// and is pinned by TestConsoleKeepsTheRawRecordWhileExportIsRedacted.
//
// Install the result with slog.SetDefault.
func NewHandler(cfg Config) (slog.Handler, error) {
	if cfg.Console == nil {
		return nil, errors.New("telemetry: Config.Console is required")
	}
	if !cfg.ExportLogs {
		// No OTLP log endpoint: the console alone, still trace-correlated,
		// because `adk web`'s own trace view and a local Tempo both benefit and
		// the stamping costs one context lookup.
		return NewOtelHandler(cfg.Console), nil
	}
	export, err := NewExportHandler(ExportConfig{Redact: cfg.Redact, Level: cfg.Level})
	if err != nil {
		return nil, err
	}
	fanout, err := NewMultiHandler(cfg.Console, export)
	if err != nil {
		return nil, err
	}
	return NewOtelHandler(fanout), nil
}
