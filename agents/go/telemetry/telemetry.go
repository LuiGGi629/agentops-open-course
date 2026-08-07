// Package telemetry is the agent's observability plane: precisely the parts of
// OpenTelemetry that ADK's launcher does not set up for this process, and
// nothing else.
//
// # What ADK already does, and this package therefore does not
//
// launcher.Config.TelemetryOptions feeds google.golang.org/adk/v2/telemetry,
// which builds a TracerProvider and a LoggerProvider from the standard
// OTEL_EXPORTER_OTLP_* variables, installs both globally, and emits a span for
// every model call, tool call, agent invocation and workflow node. There is no
// TracerProvider here, and there must never be one: a second provider installed
// after ADK's packages load would silently orphan every ADK span, because ADK
// resolves its tracer from the global provider at package-init time.
//
// # What ADK does not do, and this package therefore does
//
//  1. Metrics. ADK v2.1.0 constructs no MeterProvider at all — its own TODO
//     says so — so [InstallMeterProvider] builds the OTLP metric pipeline the
//     four custom agentops.* counters need to reach Prometheus. See metrics.go.
//  2. Trace-correlated logs. Go's log/slog knows nothing about OpenTelemetry,
//     so [OtelHandler] stamps the active trace and span identifiers onto every
//     record, and [NewHandler] fans each record out to the console verbatim and
//     to OTLP through a redacting, bounded, fail-closed filter. See slog.go and
//     export.go. Without the stamping, Grafana's trace-to-logs and Loki's
//     derived fields have no identifier to join on and the correlation the
//     whole observability chapter is built on does not resolve.
//  3. Content-capture defaults. See [SetContentCaptureDefaults].
//
// # Configuration
//
// Everything here is gated by the standard OTLP environment variables, exactly
// as ADK gates itself: with no endpoint set, no exporter is built, nothing is
// sent, and no error is raised. That is what keeps the account-free local path
// silent. The package reads those variables through [ExportConfigured],
// [LogsConfigured] and [MetricsConfigured] rather than holding a package
// singleton, so a caller decides once at startup and passes the answer in.
package telemetry

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.36.0"
)

// ScopeName is the OpenTelemetry instrumentation scope every signal this agent
// emits for itself shares.
//
// It is the meter name in Chapter 4.3's documented instrument definitions and
// the logger name on the OTLP log path, so a backend groups the agent's own
// telemetry apart from ADK's (which uses its own gcp.vertex.agent scope). The
// resilience package spells the same string for its circuit-breaker counter
// rather than importing this one, because that package deliberately depends on
// nothing else in the module; the two are pinned together by
// TestScopeNameMatchesTheResilienceMeter.
const ScopeName = "agentops.agent"

// ServiceName is the service.name resource attribute every signal carries.
//
// Tempo, Loki and Prometheus all key their service selectors on it, and the
// Grafana dashboards ship those selectors, so it is a contract rather than a
// label. It matches the ADK application name the policy plugin governs.
const ServiceName = "agentops-agent"

// The content-capture switches, and the value that keeps them off.
//
// Course invariant: telemetry content stays private by default. Spans and log
// records keep timing, model, tool, token and status metadata, and never carry
// the user's prompt or the model's answer, unless an operator opts in
// explicitly. ADK Go reads only the GenAI variable (its Python counterpart read
// both); the ADK one is pinned anyway so the two tracks resolve to the same
// answer and a future ADK release cannot quietly flip the default.
const (
	EnvADKCaptureMessageContent   = "ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS"
	EnvGenAICaptureMessageContent = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"
	ContentCaptureDisabled        = "false"
)

// The standard OTLP environment variables this package gates on. They are named
// here rather than spelled at each call site because the gating rule — "an
// endpoint for this signal, and the SDK not disabled" — is repeated three times
// and has to stay identical in all three.
const (
	EnvSDKDisabled         = "OTEL_SDK_DISABLED"
	EnvOTLPEndpoint        = "OTEL_EXPORTER_OTLP_ENDPOINT"
	EnvOTLPTracesEndpoint  = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	EnvOTLPMetricsEndpoint = "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"
	EnvOTLPLogsEndpoint    = "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"
)

// SetContentCaptureDefaults pins both content-capture switches to "false"
// unless the operator has already chosen a value.
//
// It is a default, not an override: an explicitly exported variable wins, which
// is what lets a learner turn capture on for one debugging session in Chapter
// 7.1 without editing code. Call it once, before the launcher builds the
// telemetry providers — ADK reads its variable through a sync.Once, so a later
// call has no effect on a process that already logged one model call.
func SetContentCaptureDefaults() error {
	var problems []error
	for _, name := range []string{EnvADKCaptureMessageContent, EnvGenAICaptureMessageContent} {
		if _, chosen := os.LookupEnv(name); chosen {
			continue
		}
		if err := os.Setenv(name, ContentCaptureDisabled); err != nil {
			problems = append(problems, fmt.Errorf(
				"pinning %s to %q: %w", name, ContentCaptureDisabled, err,
			))
		}
	}
	return errors.Join(problems...)
}

// sdkDisabled reports the OTEL_SDK_DISABLED kill switch.
//
// The spec defines only "true", but a hand-edited .env reaches this code with
// "1" and "yes" too, and a learner who typed one of those meant to turn export
// off. Accepting the three is deliberate; accepting anything else would make
// "disabled=maybe" a silent on.
func sdkDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvSDKDisabled))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// endpointConfigured reports whether any of the named variables holds a value.
func endpointConfigured(names ...string) bool {
	if sdkDisabled() {
		return false
	}
	for _, name := range names {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// ExportConfigured reports whether this process exports any signal at all.
//
// It is the coarse gate: true means at least one of the four endpoints is set,
// so building providers is worth the cost. It does not imply that logs or
// metrics specifically are exported — a traces-only endpoint answers true here
// and false to [LogsConfigured].
func ExportConfigured() bool {
	return endpointConfigured(
		EnvOTLPEndpoint, EnvOTLPTracesEndpoint, EnvOTLPMetricsEndpoint, EnvOTLPLogsEndpoint,
	)
}

// LogsConfigured reports whether log records should be exported over OTLP.
//
// A traces-only endpoint deliberately answers false: the OTLP log path costs a
// redaction pass on every record, and paying it to feed an exporter that has
// nowhere to send is waste. The console handler is unaffected either way.
func LogsConfigured() bool {
	return endpointConfigured(EnvOTLPEndpoint, EnvOTLPLogsEndpoint)
}

// MetricsConfigured reports whether metrics should be exported over OTLP.
func MetricsConfigured() bool {
	return endpointConfigured(EnvOTLPEndpoint, EnvOTLPMetricsEndpoint)
}

// Resource returns the resource every signal this process emits is attributed
// to.
//
// One builder for both providers: it is passed to ADK through
// telemetry.WithResource (for spans and log records) and to
// [InstallMeterProvider] (for metrics), so a metric and the span it was
// recorded on can never disagree about which service produced them.
//
// The resource is schemaless on purpose. ADK merges this value into its own
// resource with resource.Merge, which fails outright when two non-empty schema
// URLs disagree — and ADK's default resource carries the SDK's, which moves
// with every OpenTelemetry release. An empty schema URL merges with anything,
// and the only attributes here are two whose keys have been stable for years.
func Resource(version string) *resource.Resource {
	return resource.NewSchemaless(
		semconv.ServiceName(ServiceName),
		semconv.ServiceVersion(version),
	)
}
