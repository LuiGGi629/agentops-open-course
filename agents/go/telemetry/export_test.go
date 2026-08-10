package telemetry_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/policy"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/telemetry"
)

// recordingExporter keeps every log record the SDK exports, so a test can read
// the record that actually left the process rather than a formatted rendering
// of it.
type recordingExporter struct {
	records []sdklog.Record
	mu      sync.Mutex
}

func (e *recordingExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, record := range records {
		// The SDK reuses its record storage, so a copy is mandatory here.
		e.records = append(e.records, record.Clone())
	}
	return nil
}

func (e *recordingExporter) Shutdown(context.Context) error   { return nil }
func (e *recordingExporter) ForceFlush(context.Context) error { return nil }

func (e *recordingExporter) exported() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.records
}

// only returns the single record the exporter received.
func (e *recordingExporter) only(t *testing.T) sdklog.Record {
	t.Helper()

	records := e.exported()
	if len(records) != 1 {
		t.Fatalf("exported %d records, want 1", len(records))
	}
	return records[0]
}

// exportSink wires an [telemetry.ExportHandler] to a readable log pipeline.
func exportSink(t *testing.T, redact telemetry.Redactor) (*telemetry.ExportHandler, *recordingExporter) {
	t.Helper()

	exporter := &recordingExporter{}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)),
	)
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutting the logger provider down: %v", err)
		}
	})
	handler, err := telemetry.NewExportHandler(telemetry.ExportConfig{
		Redact: redact,
		Logger: provider.Logger(telemetry.ScopeName),
	})
	if err != nil {
		t.Fatalf("NewExportHandler() error = %v, want nil", err)
	}
	return handler, exporter
}

// passthrough is the redactor for tests about bounding, where redaction itself
// is not the subject.
func passthrough(_ context.Context, value any) any { return value }

// exportedAttributes flattens an exported record's attributes.
func exportedAttributes(record sdklog.Record) map[string]log.Value {
	found := map[string]log.Value{}
	record.WalkAttributes(func(attr log.KeyValue) bool {
		found[attr.Key] = attr.Value
		return true
	})
	return found
}

// TestExportRequiresARedactor: an unredacted record reaching a collector cannot
// be recalled, so a missing redactor is a startup failure, never a default.
func TestExportRequiresARedactor(t *testing.T) {
	t.Parallel()

	if _, err := telemetry.NewExportHandler(telemetry.ExportConfig{}); err == nil {
		t.Error("NewExportHandler() with no redactor returned no error")
	}
}

// TestExportBoundingRecursesThroughStructuredValues is the port of
// test_export_bounding_recurses_through_structured_values: every string inside
// a structured attribute is capped, and non-strings pass through as themselves
// — a boolean stays a boolean and a number stays a number, because a backend
// that receives them as text can no longer aggregate them.
func TestExportBoundingRecursesThroughStructuredValues(t *testing.T) {
	t.Parallel()

	handler, exporter := exportSink(t, passthrough)
	long := strings.Repeat("x", 3000)

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "structured", 0)
	record.AddAttrs(slog.Any("evidence", map[string]any{
		"items":   []any{long, 7},
		"enabled": true,
		"short":   "short",
	}))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	evidence, found := exportedAttributes(exporter.only(t))["evidence"]
	if !found {
		t.Fatal("the structured attribute was not exported")
	}
	members := map[string]log.Value{}
	for _, member := range evidence.AsMap() {
		members[member.Key] = member.Value
	}
	items := members["items"].AsSlice()
	if len(items) != 2 {
		t.Fatalf("items has %d members, want 2", len(items))
	}
	capped := items[0].AsString()
	if !strings.HasSuffix(capped, telemetry.TruncatedSuffix) {
		t.Errorf("the long string was not marked truncated: %q", capped[max(0, len(capped)-40):])
	}
	if got := len([]rune(capped)); got != telemetry.MaxExportedChars {
		t.Errorf("the capped string is %d characters, want exactly %d", got, telemetry.MaxExportedChars)
	}
	if got := items[1].AsInt64(); got != 7 {
		t.Errorf("the number was exported as %v, want 7", items[1])
	}
	if !members["enabled"].AsBool() {
		t.Errorf("the boolean was exported as %v, want true", members["enabled"])
	}
	if got := members["short"].AsString(); got != "short" {
		t.Errorf("a short string was rewritten to %q", got)
	}
}

// TestExportFailsClosedWhenRedactionFails is the port of
// test_export_filter_fails_closed_when_local_redaction_fails. If the one piece
// of code that inspects untrusted text breaks, the record loses its body and
// every attribute. Falling back to the original would give the single path that
// matters no protection at all.
func TestExportFailsClosedWhenRedactionFails(t *testing.T) {
	t.Parallel()

	broken := func(context.Context, any) any { panic("recognizer unavailable") }
	handler, exporter := exportSink(t, broken)

	record := slog.NewRecord(time.Now(), slog.LevelError, "raw secret", 0)
	record.AddAttrs(slog.String("credential", "never-export-this"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	exported := exporter.only(t)
	if got := exported.Body().AsString(); got != telemetry.OmittedBody {
		t.Errorf("body = %q, want %q", got, telemetry.OmittedBody)
	}
	if got := exported.AttributesLen(); got != 0 {
		t.Errorf("exported %d attributes after a redaction failure, want 0", got)
	}
	// The record the caller wrote is untouched, so the console beside this
	// handler still shows an engineer what actually happened.
	if got := attributes(record)["credential"]; got != "never-export-this" {
		t.Errorf("the original record was mutated: credential = %q", got)
	}
}

// TestExportRedactsTheMessageAttributesAndWithAttrs is the Go-specific half of
// the policy, and the reason this handler owns its attributes instead of
// wrapping another one: slog keeps With() attributes inside the handler, so a
// filter that only inspected the incoming record would export everything a
// caller attached with logger.With(…) completely untouched.
func TestExportRedactsTheMessageAttributesAndWithAttrs(t *testing.T) {
	t.Parallel()

	governance, err := policy.New(policy.Config{})
	if err != nil {
		t.Fatalf("policy.New() error = %v, want nil", err)
	}
	handler, exporter := exportSink(t, governance.RedactPersistedValue)

	derived := handler.
		WithAttrs([]slog.Attr{slog.String("operator", "reach me at jane.doe@acme.example")}).
		WithGroup("call")
	record := slog.NewRecord(time.Now(), slog.LevelWarn,
		"calling upstream with api_key=super-secret-value-123456", 0)
	record.AddAttrs(slog.String("header", "Authorization: Bearer abcdefghijklmnop"))
	if err := derived.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	exported := exporter.only(t)
	body := exported.Body().AsString()
	if strings.Contains(body, "super-secret-value-123456") {
		t.Errorf("the credential survived into the exported body: %q", body)
	}
	if !strings.Contains(body, "api_key="+policy.SecretMask) {
		t.Errorf("body = %q, want it to name the masked credential", body)
	}

	attributes := exportedAttributes(exported)
	// The With() attribute is qualified by no group — it was attached before
	// WithGroup — and it is redacted like everything else.
	operator := attributes["operator"].AsString()
	if strings.Contains(operator, "jane.doe@acme.example") {
		t.Errorf("a With() attribute escaped redaction: %q", operator)
	}
	// The record attribute is qualified by the open group.
	header, found := attributes["call.header"]
	if !found {
		t.Fatalf("the record attribute was not exported under its group: %v", attributes)
	}
	if strings.Contains(header.AsString(), "abcdefghijklmnop") {
		t.Errorf("a bearer token survived into an exported attribute: %q", header.AsString())
	}
}

// TestExportCarriesTheTraceCorrelationAndOnlyTheFailureType is the port of the
// export half of
// test_agent_logs_export_one_safe_trace_correlated_copy_without_duplicate_handlers.
//
// Two properties. The exported record carries the trace and span identifiers,
// which the log SDK takes from the same context the handler was given — that is
// what makes Grafana's trace-to-logs resolve. And a record that logged a
// failure carries the failure's type as its own low-cardinality attribute,
// beside the redacted message, which is what lets a dashboard group failures
// without reading any of their text.
func TestExportCarriesTheTraceCorrelationAndOnlyTheFailureType(t *testing.T) {
	t.Parallel()

	ctx, traceID, spanID := sampledContext(t)
	governance, err := policy.New(policy.Config{})
	if err != nil {
		t.Fatalf("policy.New() error = %v, want nil", err)
	}
	handler, exporter := exportSink(t, governance.RedactPersistedValue)

	failure := fmt.Errorf("upstream refused: %w", errors.New("token=never-export-this"))
	record := slog.NewRecord(time.Now(), slog.LevelError, "tool call failed", 0)
	record.AddAttrs(slog.Any("error", failure))
	if err := handler.Handle(ctx, record); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	exported := exporter.only(t)
	if got := exported.TraceID().String(); got != traceID {
		t.Errorf("exported trace id = %q, want %q", got, traceID)
	}
	if got := exported.SpanID().String(); got != spanID {
		t.Errorf("exported span id = %q, want %q", got, spanID)
	}
	if got := exported.Severity(); got != log.SeverityError {
		t.Errorf("severity = %v, want %v", got, log.SeverityError)
	}

	attributes := exportedAttributes(exported)
	kind, found := attributes[telemetry.ExceptionTypeKey]
	if !found {
		t.Fatalf("no %s attribute on a record that logged an error: %v", telemetry.ExceptionTypeKey, attributes)
	}
	if got := kind.AsString(); got != fmt.Sprintf("%T", failure) {
		t.Errorf("%s = %q, want %q", telemetry.ExceptionTypeKey, got, fmt.Sprintf("%T", failure))
	}
	if text := attributes["error"].AsString(); strings.Contains(text, "never-export-this") {
		t.Errorf("the credential inside the error survived export: %q", text)
	}
}

// TestConsoleAndExportBothReceiveRedactedRecords pins the durable-sink rule:
// terminals and CI capture persist output too, so neither local nor OTLP logs
// may retain the credential while both keep trace correlation.
func TestConsoleAndExportBothReceiveRedactedRecords(t *testing.T) {
	t.Parallel()

	ctx, traceID, _ := sampledContext(t)
	governance, err := policy.New(policy.Config{})
	if err != nil {
		t.Fatalf("policy.New() error = %v, want nil", err)
	}

	exporter := &recordingExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() {
		if shutdownErr := provider.Shutdown(context.Background()); shutdownErr != nil {
			t.Errorf("shutting the logger provider down: %v", shutdownErr)
		}
	})
	export, err := telemetry.NewExportHandler(telemetry.ExportConfig{
		Redact: governance.RedactPersistedValue,
		Logger: provider.Logger(telemetry.ScopeName),
	})
	if err != nil {
		t.Fatalf("NewExportHandler() error = %v, want nil", err)
	}
	console := &capture{}
	sanitizedConsole, err := telemetry.NewSanitizingHandler(console, governance.RedactPersistedValue)
	if err != nil {
		t.Fatalf("NewSanitizingHandler() error = %v, want nil", err)
	}
	fanout, err := telemetry.NewMultiHandler(sanitizedConsole, export)
	if err != nil {
		t.Fatalf("NewMultiHandler() error = %v, want nil", err)
	}
	logger := slog.New(telemetry.NewOtelHandler(fanout))

	secret := "password=super-secret-value-123456"
	logger.WarnContext(ctx, secret)

	// The console record is redacted and trace-correlated.
	local := console.only(t)
	if strings.Contains(local.Message, "super-secret-value-123456") {
		t.Errorf("the credential reached the console: %q", local.Message)
	}
	if got := attributes(local)[telemetry.TraceIDKey]; got != traceID {
		t.Errorf("console %s = %q, want %q", telemetry.TraceIDKey, got, traceID)
	}

	// The exported record is redacted, and carries the correlation twice over:
	// as the stamped attributes and as the record's own trace fields.
	exported := exporter.only(t)
	if body := exported.Body().AsString(); strings.Contains(body, "super-secret-value-123456") {
		t.Errorf("the credential reached the collector: %q", body)
	}
	if got := exported.TraceID().String(); got != traceID {
		t.Errorf("exported trace id = %q, want %q", got, traceID)
	}
	if got := exportedAttributes(exported)[telemetry.TraceIDKey].AsString(); got != traceID {
		t.Errorf("exported %s attribute = %q, want %q", telemetry.TraceIDKey, got, traceID)
	}
}

// TestExportSkipsRecordsBelowTheLevel keeps the redaction pass — the most
// expensive thing on this path — from running for records nothing will read.
func TestExportSkipsRecordsBelowTheLevel(t *testing.T) {
	t.Parallel()

	exporter := &recordingExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutting the logger provider down: %v", err)
		}
	})
	handler, err := telemetry.NewExportHandler(telemetry.ExportConfig{
		Redact: passthrough,
		Level:  slog.LevelWarn,
		Logger: provider.Logger(telemetry.ScopeName),
	})
	if err != nil {
		t.Fatalf("NewExportHandler() error = %v, want nil", err)
	}

	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) = true on a warn-level handler")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false on a warn-level handler")
	}
}
