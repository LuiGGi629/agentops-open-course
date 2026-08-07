package telemetry_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/telemetry"
)

// capture is a slog.Handler that keeps every record it is given, so a test can
// assert on what a wrapper passed through rather than on formatted text.
type capture struct {
	fail    error
	records []slog.Record
	attrs   []slog.Attr
	groups  []string
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }

func (c *capture) Handle(_ context.Context, record slog.Record) error {
	c.records = append(c.records, record)
	return c.fail
}

func (c *capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	c.attrs = append(c.attrs, attrs...)
	return c
}

func (c *capture) WithGroup(name string) slog.Handler {
	c.groups = append(c.groups, name)
	return c
}

// only returns the single record the handler captured.
func (c *capture) only(t *testing.T) slog.Record {
	t.Helper()

	if len(c.records) != 1 {
		t.Fatalf("captured %d records, want 1", len(c.records))
	}
	return c.records[0]
}

// attributes flattens a record's attributes into a map for assertions.
func attributes(record slog.Record) map[string]string {
	found := map[string]string{}
	record.Attrs(func(attr slog.Attr) bool {
		found[attr.Key] = attr.Value.String()
		return true
	})
	return found
}

// sampledContext returns a context carrying a valid but non-recording span
// context, which is exactly the shape a propagated traceparent header produces.
func sampledContext(t *testing.T) (context.Context, string, string) {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("parsing the trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("parsing the span id: %v", err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, Remote: true,
	})
	return trace.ContextWithSpanContext(context.Background(), spanContext),
		traceID.String(), spanID.String()
}

// TestOtelHandlerStampsAPropagatedTraceCorrelation is the load-bearing one: a
// span context arriving in a header is valid but not recording here, and it is
// the case where the log line is this process's only evidence. Gating on
// IsRecording would drop the correlation on every request that came through the
// gateway, which is every request in Chapter 5 onwards.
func TestOtelHandlerStampsAPropagatedTraceCorrelation(t *testing.T) {
	t.Parallel()

	ctx, traceID, spanID := sampledContext(t)
	sink := &capture{}
	handler := telemetry.NewOtelHandler(sink)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "investigating", 0)
	if err := handler.Handle(ctx, record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	stamped := attributes(sink.only(t))
	if got := stamped[telemetry.TraceIDKey]; got != traceID {
		t.Errorf("%s = %q, want %q", telemetry.TraceIDKey, got, traceID)
	}
	if got := stamped[telemetry.SpanIDKey]; got != spanID {
		t.Errorf("%s = %q, want %q", telemetry.SpanIDKey, got, spanID)
	}
}

// TestOtelHandlerStampsNothingWithoutASpan keeps a local run clean: an
// all-zero trace id in every line would be worse than no field at all, because
// a derived field would then link every line to the same nonexistent trace.
func TestOtelHandlerStampsNothingWithoutASpan(t *testing.T) {
	t.Parallel()

	sink := &capture{}
	handler := telemetry.NewOtelHandler(sink)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "starting", 0)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	stamped := attributes(sink.only(t))
	if _, found := stamped[telemetry.TraceIDKey]; found {
		t.Errorf("stamped %s with no active span: %v", telemetry.TraceIDKey, stamped)
	}
	if _, found := stamped[telemetry.SpanIDKey]; found {
		t.Errorf("stamped %s with no active span: %v", telemetry.SpanIDKey, stamped)
	}
}

// TestOtelHandlerKeepsStampingAfterWithAttrsAndWithGroup is the bug this
// wrapper invites: slog's embedded promotion makes WithAttrs return the *inner*
// handler, so one logger.With() call would silently unwrap the correlation for
// the rest of that logger's life and nothing would fail.
func TestOtelHandlerKeepsStampingAfterWithAttrsAndWithGroup(t *testing.T) {
	t.Parallel()

	ctx, traceID, _ := sampledContext(t)
	sink := &capture{}

	derived := telemetry.NewOtelHandler(sink).
		WithAttrs([]slog.Attr{slog.String("component", "tools")}).
		WithGroup("call")

	record := slog.NewRecord(time.Now(), slog.LevelWarn, "retrying", 0)
	if err := derived.Handle(ctx, record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := attributes(sink.only(t))[telemetry.TraceIDKey]; got != traceID {
		t.Errorf("%s = %q after With/WithGroup, want %q", telemetry.TraceIDKey, got, traceID)
	}
}

// TestOtelHandlerIgnoresAnEmptyGroup follows slog's own contract, which says an
// empty group name is a no-op rather than a nameless nesting level.
func TestOtelHandlerIgnoresAnEmptyGroup(t *testing.T) {
	t.Parallel()

	sink := &capture{}
	handler := telemetry.NewOtelHandler(sink)
	if handler.WithGroup("") != slog.Handler(handler) {
		t.Error("WithGroup(\"\") returned a different handler; it must be a no-op")
	}
	if len(sink.groups) != 0 {
		t.Errorf("forwarded an empty group name: %v", sink.groups)
	}
}

// TestMultiHandlerGivesEachDestinationItsOwnRecord is why the fan-out clones:
// slog.Record keeps its attributes in a shared backing array, so a destination
// that appends to its copy would corrupt what the next one reads. The console
// and the exporter must be unable to affect each other.
func TestMultiHandlerGivesEachDestinationItsOwnRecord(t *testing.T) {
	t.Parallel()

	first, second := &capture{}, &capture{}
	fanout, err := telemetry.NewMultiHandler(first, second)
	if err != nil {
		t.Fatalf("NewMultiHandler() error = %v, want nil", err)
	}

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "one message", 0)
	record.AddAttrs(slog.String("incident", "shared"))
	if err := fanout.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	// A destination that mutates its copy must not be visible to the other.
	delivered := first.only(t)
	delivered.AddAttrs(slog.String("added", "by the first destination"))
	if _, leaked := attributes(second.only(t))["added"]; leaked {
		t.Error("one destination's mutation reached the other; the record was not cloned")
	}
}

// TestMultiHandlerReportsEveryFailure keeps one broken destination from hiding
// another, and from stopping the healthy one.
func TestMultiHandlerReportsEveryFailure(t *testing.T) {
	t.Parallel()

	broken := &capture{fail: errors.New("collector unreachable")}
	healthy := &capture{}
	fanout, err := telemetry.NewMultiHandler(broken, healthy)
	if err != nil {
		t.Fatalf("NewMultiHandler() error = %v, want nil", err)
	}

	handleErr := fanout.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelError, "boom", 0))
	if handleErr == nil || !strings.Contains(handleErr.Error(), "collector unreachable") {
		t.Errorf("Handle() error = %v, want it to name the failing destination", handleErr)
	}
	if len(healthy.records) != 1 {
		t.Errorf("the healthy destination received %d records, want 1", len(healthy.records))
	}
}

// TestMultiHandlerRefusesNoDestination catches the wiring mistake that would
// otherwise present as "the process stopped logging".
func TestMultiHandlerRefusesNoDestination(t *testing.T) {
	t.Parallel()

	if _, err := telemetry.NewMultiHandler(); err == nil {
		t.Error("NewMultiHandler() with no destination returned no error")
	}
	if _, err := telemetry.NewMultiHandler(nil, nil); err == nil {
		t.Error("NewMultiHandler(nil, nil) returned no error")
	}
}

// TestNewHandlerRefusesAnIncompleteConfiguration keeps both required seams
// explicit. A missing redactor in particular must not default to a
// pass-through: "we chose not to redact" cannot be the same line of code as "we
// forgot to wire it".
func TestNewHandlerRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := telemetry.NewHandler(telemetry.Config{}); err == nil {
		t.Error("NewHandler() with no console returned no error")
	}
	_, err := telemetry.NewHandler(telemetry.Config{Console: &capture{}, ExportLogs: true})
	if err == nil {
		t.Error("NewHandler() exporting without a redactor returned no error")
	}
}

// TestNewHandlerWithoutExportStillCorrelates: with no OTLP log endpoint the
// console is the only sink, and it still gets the identifiers — that is what
// makes a locally collected log file joinable with a locally collected trace.
func TestNewHandlerWithoutExportStillCorrelates(t *testing.T) {
	t.Parallel()

	ctx, traceID, _ := sampledContext(t)
	console := &capture{}
	handler, err := telemetry.NewHandler(telemetry.Config{Console: console})
	if err != nil {
		t.Fatalf("NewHandler() error = %v, want nil", err)
	}
	if err := handler.Handle(ctx, slog.NewRecord(time.Now(), slog.LevelInfo, "local", 0)); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if got := attributes(console.only(t))[telemetry.TraceIDKey]; got != traceID {
		t.Errorf("%s = %q, want %q", telemetry.TraceIDKey, got, traceID)
	}
}
