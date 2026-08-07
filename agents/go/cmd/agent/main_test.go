package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/a2aserver"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/compose"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/data"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/mcpserver"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/memory"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/resilience"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/state"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/telemetry"
)

// These tests are offline: no model is called and no port is opened. What they
// prove is the wiring — that each repository subcommand reaches its own package
// without the launcher parsing it, that the conversational surface holds every
// tool its instruction claims, and that sessions are actually persistent.

// repositoryDataset is the committed dataset, relative to this package.
const repositoryDataset = "../../../data"

// isolatedEnvironment clears the AGENT_ and provider variables a developer's
// shell may carry, so a test judges the configuration it set rather than the
// one that happened to be exported.
func isolatedEnvironment(t *testing.T) {
	t.Helper()

	for _, setting := range (config.Config{}).Describe() {
		// t.Setenv records the original value and restores it on cleanup;
		// unsetting straight afterwards is what makes the variable genuinely
		// absent rather than explicitly empty, which the loader deliberately
		// treats as two different states.
		t.Setenv(setting.Variable, "")
		if err := os.Unsetenv(setting.Variable); err != nil {
			t.Fatalf("clearing %s: %v", setting.Variable, err)
		}
	}
	t.Setenv(config.EnvDataDir, repositoryDataset)
	t.Setenv(config.EnvStateDir, t.TempDir())
}

// offlineConfig is the configuration every wiring test assembles against: the
// committed dataset, a disposable state directory, and nothing that reaches a
// network.
func offlineConfig(t *testing.T, entrypoint config.Entrypoint) config.Config {
	t.Helper()

	return config.Config{
		Entrypoint:         entrypoint,
		ModelProvider:      config.ProviderOpenAICompatible,
		Model:              "qwen3:4b-instruct",
		OpenAIBaseURL:      "http://127.0.0.1:11434/v1",
		OpenAIAPIKey:       "local-ollama",
		DataDir:            repositoryDataset,
		StateDir:           t.TempDir(),
		EmbeddingsURL:      "http://127.0.0.1:11434",
		EmbeddingModel:     "nomic-embed-text",
		EmbeddingTimeout:   config.Seconds(120),
		DrainTimeout:       config.Seconds(10),
		ModelTimeout:       config.Seconds(60),
		ToolTimeout:        config.Seconds(30),
		MaxRetries:         2,
		RetryBackoff:       config.Seconds(0.5),
		A2ABindHost:        "127.0.0.1",
		A2AHost:            "localhost",
		A2AProtocol:        "http",
		A2APort:            8080,
		A2AMaxLLMCalls:     12,
		SanitizeToolOutput: true,
	}
}

// keepDefaultLogger restores the process logger a test's telemetry install
// replaced, so one test's observability plane is not another's.
func keepDefaultLogger(t *testing.T) {
	t.Helper()

	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
}

// TestConfigCheckIsDispatchedBeforeTheLauncher covers the subcommand
// `mise run config:check` invokes.
//
// It has to be intercepted first: the launcher routes an unrecognized first
// argument to its default sublauncher, the console, which would report
// "config:check" as an unparsed flag instead of running the check.
func TestConfigCheckIsDispatchedBeforeTheLauncher(t *testing.T) {
	isolatedEnvironment(t)

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), []string{"config:check"}, &out, &errOut); code != config.ExitValid {
		t.Fatalf("execute() = %d, want %d; stderr: %s", code, config.ExitValid, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want it empty on a valid configuration", errOut.String())
	}
	for _, want := range []string{config.EnvEntrypoint, config.EnvModel, config.EnvOpenAIAPIKey} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report never mentions %s", want)
		}
	}
	// The secret is masked, which is the property that makes this command safe
	// to paste into an issue.
	if strings.Contains(out.String(), "local-ollama") {
		t.Error("the report disclosed the API key")
	}
	if !strings.Contains(out.String(), config.SecretMask) {
		t.Error("the report never shows a masked secret")
	}
}

// TestConfigCheckReportsAnInvalidConfiguration pins the non-zero exit and the
// error channel: the task that runs this is a gate, and a gate that exits zero
// on a broken configuration is not a gate.
func TestConfigCheckReportsAnInvalidConfiguration(t *testing.T) {
	isolatedEnvironment(t)
	t.Setenv(config.EnvMaxRetries, "not-a-number")

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), []string{"config:check"}, &out, &errOut); code != config.ExitInvalid {
		t.Fatalf("execute() = %d, want %d", code, config.ExitInvalid)
	}
	if !strings.Contains(errOut.String(), config.EnvMaxRetries) {
		t.Errorf("stderr = %q, want it to name the bad variable", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want the report to stay off the success channel", out.String())
	}
}

// TestContentCaptureIsPinnedBeforeAnythingElse covers the course invariant that
// telemetry content stays private by default.
//
// It runs on the config:check path deliberately: the defaults are pinned before
// the dispatch, so every mode of the binary gets them — including the ones that
// never build a model. ADK reads its switch through a sync.Once, so a call made
// any later would silently not apply.
func TestContentCaptureIsPinnedBeforeAnythingElse(t *testing.T) {
	isolatedEnvironment(t)
	for _, variable := range []string{
		telemetry.EnvADKCaptureMessageContent, telemetry.EnvGenAICaptureMessageContent,
	} {
		t.Setenv(variable, "")
		if err := os.Unsetenv(variable); err != nil {
			t.Fatalf("clearing %s: %v", variable, err)
		}
	}

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), []string{"config:check"}, &out, &errOut); code != config.ExitValid {
		t.Fatalf("execute() = %d, want %d; stderr: %s", code, config.ExitValid, errOut.String())
	}
	for _, variable := range []string{
		telemetry.EnvADKCaptureMessageContent, telemetry.EnvGenAICaptureMessageContent,
	} {
		if got := os.Getenv(variable); got != telemetry.ContentCaptureDisabled {
			t.Errorf("%s = %q, want %q", variable, got, telemetry.ContentCaptureDisabled)
		}
	}
}

// TestAnOperatorChoiceOfContentCaptureSurvives pins the other half of the rule:
// the pin is a default, not an override, which is what lets a learner turn
// capture on for one debugging session without editing code.
func TestAnOperatorChoiceOfContentCaptureSurvives(t *testing.T) {
	isolatedEnvironment(t)
	t.Setenv(telemetry.EnvADKCaptureMessageContent, "true")

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), []string{"config:check"}, &out, &errOut); code != config.ExitValid {
		t.Fatalf("execute() = %d, want %d; stderr: %s", code, config.ExitValid, errOut.String())
	}
	if got := os.Getenv(telemetry.EnvADKCaptureMessageContent); got != "true" {
		t.Errorf("%s = %q, want the operator's own value", telemetry.EnvADKCaptureMessageContent, got)
	}
}

// TestTheRuntimeAssemblesEveryPlane is the wiring guarantee this binary exists
// for: one value carries the model, the policy plane, the tools, the knowledge
// and recall surface, and the compositions, and the launcher and A2A paths both
// serve it.
//
// It replaces the placeholder assertion this test file used to carry, which
// recorded that the four memory-owned tools were unset and that the binary
// refused to start because of it. They are set now, so the fact worth pinning
// is the opposite one.
func TestTheRuntimeAssemblesEveryPlane(t *testing.T) {
	keepDefaultLogger(t)

	cfg := offlineConfig(t, config.EntrypointAgent)
	assembled, err := newAgentRuntime(t.Context(), cfg, io.Discard, launcherInstallsProviders)
	if err != nil {
		t.Fatalf("newAgentRuntime() error = %v, want nil", err)
	}
	t.Cleanup(func() { assembled.close(t.Context()) })

	root, err := assembled.compositions.RootAgent()
	if err != nil {
		t.Fatalf("RootAgent() error = %v, want nil", err)
	}
	if root.Name() != compose.AgentName {
		t.Errorf("RootAgent() = %q, want %q", root.Name(), compose.AgentName)
	}
	// The persistent memory service is what ADK's own load_memory and
	// preload_memory resolve against; a nil one would leave them answering from
	// an in-memory store that dies with the process.
	if assembled.memories.Service() == nil {
		t.Error("the runtime carries no memory service")
	}
	if assembled.store == nil {
		t.Error("the runtime carries no dataset store")
	}
	if assembled.version == "" {
		t.Error("the runtime resolved no version to attribute its telemetry to")
	}
}

// TestTheToolSurfaceCarriesEveryToolTheInstructionNames pins the four tools the
// memory package owns onto the surface the compositions draw from.
//
// The root instruction tells the model to call all four. A surface missing one
// would produce an agent that answers a documented capability with a
// hallucination, which is why compose.New refuses the wiring by name rather
// than starting.
func TestTheToolSurfaceCarriesEveryToolTheInstructionNames(t *testing.T) {
	t.Parallel()

	surface, memories := offlineToolSurface(t, config.EntrypointAgent)

	for name, built := range map[string]tool.Tool{
		compose.GetRunbookToolName:     surface.GetRunbook,
		compose.SearchRunbooksToolName: surface.SearchRunbooks,
	} {
		if built == nil {
			t.Fatalf("the surface has no %s tool", name)
		}
		if built.Name() != name {
			t.Errorf("tool name = %q, want %q", built.Name(), name)
		}
	}

	// Registration order is part of the contract: it is the order the agent
	// binds them in and the order the Python track's MEMORY_TOOLS listed.
	recall := memories.MemoryTools()
	want := []string{compose.RecallIncidentContextToolName, compose.SaveIncidentNoteToolName}
	got := make([]string, 0, len(recall))
	for _, built := range recall {
		got = append(got, built.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MemoryTools() = %v, want %v", got, want)
	}
}

// TestTheMCPServerServesExactlyTheReadAllowlist covers the second process this
// binary runs.
//
// The six names are a least-privilege allowlist shared with the client, and the
// server refuses to start on a seventh — so this asserts both that the wiring
// picks the right six and that it picks them in the order the allowlist names.
func TestTheMCPServerServesExactlyTheReadAllowlist(t *testing.T) {
	t.Parallel()

	surface, _ := offlineToolSurface(t, config.EntrypointAgent)
	server, err := mcpserver.New(mcpserver.Config{
		Probe:   func(context.Context) error { return nil },
		Tools:   slices.Concat(surface.ReadTools(), surface.KnowledgeTools()),
		Version: buildVersion(),
	})
	if err != nil {
		t.Fatalf("mcpserver.New() error = %v, want nil", err)
	}
	if got := server.ToolNames(); !reflect.DeepEqual(got, compose.MCPReadToolNames()) {
		t.Errorf("ToolNames() = %v, want %v", got, compose.MCPReadToolNames())
	}
	// The two guarded writes stay in the agent process, where a human can
	// actually approve them.
	for _, forbidden := range []string{surface.RestartService.Name(), surface.ResolveIncident.Name()} {
		if slices.Contains(server.ToolNames(), forbidden) {
			t.Errorf("the MCP surface serves the guarded write %q", forbidden)
		}
	}
}

// TestTheMCPProcessCarriesNoResiliencePolicy pins the guard that process uses.
//
// Retries, deadlines and the circuit breaker belong to the caller: the ADK
// client supplies its own timeout, and a server that retried on the client's
// behalf would double the load on a dependency that is already struggling.
func TestTheMCPProcessCarriesNoResiliencePolicy(t *testing.T) {
	t.Parallel()

	attempts := 0
	refuse := errors.New("the dependency is down")
	err := passThroughGuard(t.Context(), "list_incidents", func(context.Context) error {
		attempts++
		return refuse
	})
	if !errors.Is(err, refuse) {
		t.Errorf("passThroughGuard() error = %v, want the call's own failure", err)
	}
	if attempts != 1 {
		t.Errorf("the call ran %d times, want exactly one attempt", attempts)
	}
}

// TestTheMCPProcessIsBuiltFromTheEnvironmentAlone covers the `agent mcp`
// dispatch up to the point it would start serving.
//
// The transport is the one thing that changes between `mise run mcp` and
// `mise run mcp:http`, and it changes through the environment the deployment
// sets — never through a flag this binary would have to re-declare.
func TestTheMCPProcessIsBuiltFromTheEnvironmentAlone(t *testing.T) {
	keepDefaultLogger(t)
	t.Setenv(mcpserver.EnvTransport, string(mcpserver.TransportStreamableHTTP))
	t.Setenv(mcpserver.EnvPort, "8000")

	cfg := offlineConfig(t, config.EntrypointAgent)
	server, flush, err := newMCPServer(t.Context(), cfg, io.Discard)
	if err != nil {
		t.Fatalf("newMCPServer() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if flushErr := flush(t.Context()); flushErr != nil {
			t.Errorf("flush() error = %v, want nil", flushErr)
		}
	})
	if got := server.Transport(); got != mcpserver.TransportStreamableHTTP {
		t.Errorf("Transport() = %q, want %q", got, mcpserver.TransportStreamableHTTP)
	}
	if got := server.Address(); got != "127.0.0.1:8000" {
		t.Errorf("Address() = %q, want the address the deployment pinned", got)
	}
	if got := server.ToolNames(); !reflect.DeepEqual(got, compose.MCPReadToolNames()) {
		t.Errorf("ToolNames() = %v, want %v", got, compose.MCPReadToolNames())
	}

	// An unsupported transport is one named failure at the one place a learner
	// can supply one, rather than a server that quietly serves something else.
	t.Setenv(mcpserver.EnvTransport, "websocket")
	if _, _, err := newMCPServer(t.Context(), cfg, io.Discard); !errors.Is(err, mcpserver.ErrInvalidOptions) {
		t.Errorf("newMCPServer() error = %v, want ErrInvalidOptions", err)
	}
}

// TestTheA2AProcessOwnsFirstBoot runs the startup sequence the deployed
// contract depends on, over a real state directory and no port.
//
// The order is the guarantee: an interrupted state transaction is recovered
// before anything reads or publishes, then the incident database is published,
// then both schemas are created — and only a server that finished all of it
// reports ready.
func TestTheA2AProcessOwnsFirstBoot(t *testing.T) {
	keepDefaultLogger(t)

	cfg := offlineConfig(t, config.EntrypointAgent)
	server, assembled, err := newA2AServer(t.Context(), cfg, io.Discard)
	if err != nil {
		t.Fatalf("newA2AServer() error = %v, want nil", err)
	}
	t.Cleanup(func() { assembled.close(t.Context()) })

	// Construction is a pure wiring decision: the task store is opened during
	// startup, after the state preflight, and there is nothing before then.
	if server.TaskStore() != nil {
		t.Error("the task store was opened before the startup sequence ran")
	}
	if got := server.Options().StateDir; got != cfg.StateDir {
		t.Errorf("Options().StateDir = %q, want %q", got, cfg.StateDir)
	}
	if got := server.AgentCard().Name; got != a2aserver.CardName {
		t.Errorf("AgentCard().Name = %q, want %q", got, a2aserver.CardName)
	}

	if err := server.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("Close() error = %v, want nil", closeErr)
		}
	})
	// The three files first boot is responsible for: the published incident
	// database, ADK's session schema, and this package's task schema.
	for _, name := range []string{"incidents.db", a2aserver.SessionDatabaseName, a2aserver.TaskDatabaseName} {
		if _, err := os.Stat(filepath.Join(cfg.StateDir, name)); err != nil {
			t.Errorf("Stat(%s) = %v, want the file first boot publishes", name, err)
		}
	}

	// Readiness is an observation over that state, served without a port.
	request := httptest.NewRequest(http.MethodGet, a2aserver.ReadinessPath, nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request.WithContext(t.Context()))
	if recorder.Code != http.StatusOK {
		t.Errorf("GET %s = %d (%s), want %d",
			a2aserver.ReadinessPath, recorder.Code, recorder.Body.String(), http.StatusOK)
	}
}

// TestTheA2AServerRefusesAnEntrypointItCannotServe pins the one composition the
// public card describes.
//
// The card advertises the conversational agent's two skills, so serving the
// workflow or the coordinator behind it would publish a contract the process
// does not honor.
func TestTheA2AServerRefusesAnEntrypointItCannotServe(t *testing.T) {
	keepDefaultLogger(t)

	cfg := offlineConfig(t, config.EntrypointWorkflow)
	if _, _, err := newA2AServer(t.Context(), cfg, io.Discard); !errors.Is(err, a2aserver.ErrIncompleteConfig) {
		t.Errorf("newA2AServer() error = %v, want ErrIncompleteConfig", err)
	}
}

// TestTheLauncherConfigPublishesEveryPlane covers what ADK's launcher family is
// handed: both agents, the persistent session and memory stores, exactly one
// policy plugin, and the resource its own signals are attributed to.
func TestTheLauncherConfigPublishesEveryPlane(t *testing.T) {
	keepDefaultLogger(t)

	cfg := offlineConfig(t, config.EntrypointAgent)
	assembled, err := newAgentRuntime(t.Context(), cfg, io.Discard, launcherInstallsProviders)
	if err != nil {
		t.Fatalf("newAgentRuntime() error = %v, want nil", err)
	}
	t.Cleanup(func() { assembled.close(t.Context()) })

	launcherConfig, err := assembled.launcherConfig()
	if err != nil {
		t.Fatalf("launcherConfig() error = %v, want nil", err)
	}
	published := slices.Sorted(slices.Values(launcherConfig.AgentLoader.ListAgents()))
	want := slices.Sorted(slices.Values([]string{compose.AgentName, compose.ReportAgentName}))
	if !reflect.DeepEqual(published, want) {
		t.Errorf("ListAgents() = %v, want %v", published, want)
	}
	if launcherConfig.SessionService == nil {
		t.Error("the launcher is handed no session service")
	}
	if launcherConfig.MemoryService == nil {
		t.Error("the launcher is handed no memory service")
	}
	// One plugin, attached once, at the application boundary: a second copy
	// would double-count tokens and double-redact every turn.
	if len(launcherConfig.PluginConfig.Plugins) != 1 {
		t.Errorf("PluginConfig.Plugins = %d, want exactly the governance plugin",
			len(launcherConfig.PluginConfig.Plugins))
	}
	if len(launcherConfig.TelemetryOptions) != 1 {
		t.Errorf("TelemetryOptions = %d, want exactly the resource", len(launcherConfig.TelemetryOptions))
	}

	// The session store the launcher gets is migrated: ADK creates its tables
	// lazily and the console would otherwise fail on the first turn.
	if _, err := launcherConfig.SessionService.Create(t.Context(), &session.CreateRequest{
		AppName: compose.AgentName, UserID: "engineer",
	}); err != nil {
		t.Errorf("Create() error = %v, want a migrated session store", err)
	}
}

// TestServerSubcommandsRefuseFlagsTheyDoNotOwn covers the dispatch of the two
// long-running surfaces.
//
// Both are configured entirely by environment variables, exactly as the Python
// track configures them, so an argument is a mistake worth naming rather than
// something to ignore. The failure also proves the dispatch reached the
// subcommand at all: the launcher would have answered with its own syntax.
func TestServerSubcommandsRefuseFlagsTheyDoNotOwn(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		wantHint  string
		arguments []string
	}{
		"mcp": {arguments: []string{mcpCommand, "--transport=stdio"}, wantHint: mcpserver.EnvTransport},
		"a2a": {arguments: []string{a2aCommand, "--port=8080"}, wantHint: config.EnvEntrypoint},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer
			if code := execute(t.Context(), testCase.arguments, &out, &errOut); code != config.ExitInvalid {
				t.Fatalf("execute(%v) = %d, want %d", testCase.arguments, code, config.ExitInvalid)
			}
			if !strings.Contains(errOut.String(), testCase.wantHint) {
				t.Errorf("stderr = %q, want it to name %s", errOut.String(), testCase.wantHint)
			}
			// The launcher's syntax belongs to the launcher's failures alone;
			// printing it here would send an operator to the wrong manual.
			if strings.Contains(errOut.String(), "console") {
				t.Errorf("stderr = %q, want no launcher syntax on a subcommand failure", errOut.String())
			}
		})
	}
}

// TestStateSubcommandIsDispatchedBeforeTheLauncher covers the backup and
// restore command the host scripts and the Kubernetes CronJob drive.
func TestStateSubcommandIsDispatchedBeforeTheLauncher(t *testing.T) {
	isolatedEnvironment(t)

	stateDir := t.TempDir()
	backupRoot := filepath.Join(t.TempDir(), "snapshots")
	t.Setenv(config.EnvStateDir, stateDir)
	// A snapshot is taken of SQLite databases, so there has to be one: opening
	// the session store the way the launcher path does is what creates it.
	if _, err := newSessionService(config.Config{StateDir: stateDir}); err != nil {
		t.Fatalf("newSessionService() error = %v, want nil", err)
	}

	var out, errOut bytes.Buffer
	arguments := []string{state.Command, state.BackupSubcommand, "--backup-root", backupRoot}
	if code := execute(t.Context(), arguments, &out, &errOut); code != config.ExitValid {
		t.Fatalf("execute(%v) = %d, want %d; stderr: %s", arguments, code, config.ExitValid, errOut.String())
	}
	entries, err := os.ReadDir(backupRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadDir(%s) = %d entries, %v; want exactly one published snapshot", backupRoot, len(entries), err)
	}

	// An unknown subcommand is the state command's own error, not the
	// launcher's report of an unparsed flag.
	out.Reset()
	errOut.Reset()
	if code := execute(t.Context(), []string{state.Command, "vacuum"}, &out, &errOut); code != config.ExitInvalid {
		t.Fatalf("execute(state vacuum) = %d, want %d", code, config.ExitInvalid)
	}
	for _, want := range []string{state.BackupSubcommand, state.RestoreSubcommand} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to name the %s subcommand", errOut.String(), want)
		}
	}
}

// TestStateEnvironmentSeparatesUnsetFromUnparseable covers the retention
// setting a CronJob passes through the process environment.
//
// A pointer rather than a string, because a typo must not silently fall back to
// the default retention and start deleting snapshots on a different schedule.
func TestStateEnvironmentSeparatesUnsetFromUnparseable(t *testing.T) {
	for _, variable := range []string{state.EnvBackupKeep, state.EnvBackupTimestamp, state.EnvSourceCommit} {
		t.Setenv(variable, "")
		if err := os.Unsetenv(variable); err != nil {
			t.Fatalf("clearing %s: %v", variable, err)
		}
	}
	if environment := stateEnvironment(); environment.Keep != nil {
		t.Errorf("Keep = %q, want nil for an unset %s", *environment.Keep, state.EnvBackupKeep)
	}

	t.Setenv(state.EnvBackupKeep, "not-a-number")
	t.Setenv(state.EnvBackupTimestamp, "20260101T000000Z")
	t.Setenv(state.EnvSourceCommit, "deadbeef")
	environment := stateEnvironment()
	if environment.Keep == nil || *environment.Keep != "not-a-number" {
		t.Errorf("Keep = %v, want the operator's own value verbatim", environment.Keep)
	}
	if environment.Timestamp != "20260101T000000Z" {
		t.Errorf("Timestamp = %q, want the pinned stamp", environment.Timestamp)
	}
	if environment.SourceCommit != "deadbeef" {
		t.Errorf("SourceCommit = %q, want the recorded build", environment.SourceCommit)
	}
}

// TestTelemetryInstallsTheTraceStampingHandler pins the join key the whole
// observability chapter is built on: without the stamping, Grafana's
// trace-to-logs and Loki's derived fields have no identifier to join on.
func TestTelemetryInstallsTheTraceStampingHandler(t *testing.T) {
	keepDefaultLogger(t)

	cfg := offlineConfig(t, config.EntrypointAgent)
	governance, err := newPolicy(cfg, nil)
	if err != nil {
		t.Fatalf("newPolicy() error = %v, want nil", err)
	}

	var console bytes.Buffer
	flush, options, err := installTelemetry(t.Context(), &console, governance, "test", launcherInstallsProviders)
	if err != nil {
		t.Fatalf("installTelemetry() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if flushErr := flush(t.Context()); flushErr != nil {
			t.Errorf("flush() error = %v, want nil", flushErr)
		}
	})
	if _, ok := slog.Default().Handler().(*telemetry.OtelHandler); !ok {
		t.Errorf("slog.Default() handler = %T, want the trace-stamping handler", slog.Default().Handler())
	}
	// ADK's launcher builds the tracer and logger providers itself; the runtime
	// only tells it what to attribute them to.
	if len(options) != 1 {
		t.Errorf("installTelemetry() returned %d launcher options, want exactly the resource", len(options))
	}

	slog.Default().Info("a console line")
	if !strings.Contains(console.String(), "a console line") {
		t.Errorf("console = %q, want the record written verbatim", console.String())
	}

	// A process that installs its own providers has nothing to hand a launcher
	// it never runs.
	standaloneFlush, standaloneOptions, err := installTelemetry(
		t.Context(), &console, governance, "test", processInstallsProviders,
	)
	if err != nil {
		t.Fatalf("installTelemetry() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if flushErr := standaloneFlush(t.Context()); flushErr != nil {
			t.Errorf("flush() error = %v, want nil", flushErr)
		}
	})
	if len(standaloneOptions) != 0 {
		t.Errorf("installTelemetry() returned %d launcher options, want none", len(standaloneOptions))
	}
}

// TestSessionsSurviveARestart is the persistence guarantee: sessions live in a
// SQLite file inside AGENT_STATE_DIR, opened through the pure-Go dialector, so
// the binary keeps one SQLite implementation and no cgo.
func TestSessionsSurviveARestart(t *testing.T) {
	t.Parallel()

	cfg := config.Config{StateDir: filepath.Join(t.TempDir(), "state")}

	first, err := newSessionService(cfg)
	if err != nil {
		t.Fatalf("newSessionService() error = %v, want nil", err)
	}
	created, err := first.Create(t.Context(), &session.CreateRequest{
		AppName: "agentops-agent",
		UserID:  "engineer",
		State:   map[string]any{"note": "before the restart"},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	// A second service over the same directory is what a restart looks like.
	second, err := newSessionService(cfg)
	if err != nil {
		t.Fatalf("newSessionService() after a restart error = %v, want nil", err)
	}
	got, err := second.Get(t.Context(), &session.GetRequest{
		AppName:   "agentops-agent",
		UserID:    "engineer",
		SessionID: created.Session.ID(),
	})
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if got.Session.ID() != created.Session.ID() {
		t.Errorf("Get() = %q, want the session created before the restart", got.Session.ID())
	}
	value, err := got.Session.State().Get("note")
	if err != nil || value != "before the restart" {
		t.Errorf("State()[note] = %v, %v; want the value written before the restart", value, err)
	}
	if _, err := newSessionService(config.Config{StateDir: string([]byte{0})}); err == nil {
		t.Error("newSessionService() error = nil for an unusable state directory")
	}
}

// TestTheSessionStoreIsOpenedUnmigratedForA2A covers the course invariant the
// A2A startup sequence rests on.
//
// Creating ADK's schema is a startup step the A2A server runs *after* an
// interrupted state restore has been recovered. Opening the store must
// therefore not migrate it: until recovery has run, a half-published generation
// is indistinguishable from a healthy one, and a migration would write into it.
func TestTheSessionStoreIsOpenedUnmigratedForA2A(t *testing.T) {
	t.Parallel()

	cfg := config.Config{StateDir: filepath.Join(t.TempDir(), "state")}
	sessions, err := openSessionStore(cfg)
	if err != nil {
		t.Fatalf("openSessionStore() error = %v, want nil", err)
	}
	if _, err := sessions.Create(t.Context(), &session.CreateRequest{
		AppName: "agentops-agent", UserID: "engineer",
	}); err == nil {
		t.Error("Create() error = nil on an unmigrated store; the schema was created too early")
	}
}

// TestGuardHonoursTheCircuitBreakerToggle pins the shipped default: the breaker
// registry exists only when AGENT_CIRCUIT_BREAKER_ENABLED is on, so the default
// behavior is retry-only and no breaker is created for any tool.
func TestGuardHonoursTheCircuitBreakerToggle(t *testing.T) {
	t.Parallel()

	base := config.Config{
		ToolTimeout:             config.Seconds(30),
		RetryBackoff:            config.Seconds(0.5),
		MaxRetries:              2,
		CircuitFailureThreshold: 5,
		CircuitResetTimeout:     config.Seconds(30),
	}

	off, err := newGuard(base)
	if err != nil {
		t.Fatalf("newGuard() error = %v, want nil", err)
	}
	if off.Breakers() != nil {
		t.Error("a breaker registry exists with the toggle off")
	}
	if off.Attempts() != base.MaxRetries+1 {
		t.Errorf("Attempts() = %d, want %d", off.Attempts(), base.MaxRetries+1)
	}

	enabled := base
	enabled.CircuitBreakerEnabled = true
	on, err := newGuard(enabled)
	if err != nil {
		t.Fatalf("newGuard() error = %v, want nil", err)
	}
	if on.Breakers() == nil {
		t.Error("no breaker registry with the toggle on")
	}

	// A deadline of zero would silently disable a Chapter 4.5 guarantee, so it
	// is refused rather than defaulted.
	if _, err := newGuard(config.Config{}); err == nil {
		t.Error("newGuard() error = nil for a zero tool timeout")
	}
}

// TestActionableErrorClassifiesOnlyFirstPartyFailures covers the error-hygiene
// classifier: a message is handed to the model verbatim only when this
// repository wrote it and it names the setting to change.
func TestActionableErrorClassifiesOnlyFirstPartyFailures(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		err  error
		want bool
	}{
		"circuit open":      {err: &resilience.CircuitOpenError{}, want: true},
		"deadline":          {err: &resilience.DeadlineError{}, want: true},
		"retries exhausted": {err: &resilience.RetriesExhaustedError{}, want: true},
		"data access":       {err: data.ErrDataAccess, want: true},
		"wrapped":           {err: errors.Join(errors.New("context"), data.ErrDataAccess), want: true},
		"anything else":     {err: errors.New("pq: relation \"secrets\" does not exist"), want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := actionableError(testCase.err); got != testCase.want {
				t.Errorf("actionableError(%v) = %v, want %v", testCase.err, got, testCase.want)
			}
		})
	}
}

// TestMCPRouteIsOffUntilItsURLIsSet pins the account-free default: with no URL
// the six reads run in process, under the local deadline, retries and circuit
// breaker.
func TestMCPRouteIsOffUntilItsURLIsSet(t *testing.T) {
	t.Parallel()

	toolset, err := newMCPToolset(config.Config{})
	if err != nil {
		t.Fatalf("newMCPToolset() error = %v, want nil", err)
	}
	if toolset != nil {
		t.Error("an MCP route exists with no URL configured")
	}

	routed, err := newMCPToolset(config.Config{
		MCPURL:      "http://127.0.0.1:3000/mcp",
		ToolTimeout: config.Seconds(30),
	})
	if err != nil {
		t.Fatalf("newMCPToolset() error = %v, want nil", err)
	}
	if routed == nil {
		t.Error("no MCP route with a URL configured")
	}

	// Without a deadline the route is refused rather than built: a hung gateway
	// must fail a tool call fast, not hang the turn.
	if _, err := newMCPToolset(config.Config{MCPURL: "http://127.0.0.1:3000/mcp"}); !errors.Is(err, compose.ErrMCP) {
		t.Errorf("newMCPToolset() error = %v, want ErrMCP", err)
	}
}

// TestBothAgentsArePublished covers the loader.
//
// The structured report is a second, independently addressable agent rather than
// a mode of the root one, which is what lets an evaluation drive the schema path
// over the REST surface without changing AGENT_ENTRYPOINT. The console and the
// A2A card still serve the root agent alone, so RootAgent must stay the
// entrypoint's choice.
func TestBothAgentsArePublished(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		entrypoint config.Entrypoint
		wantRoot   string
	}{
		{config.EntrypointAgent, compose.AgentName},
		{config.EntrypointWorkflow, compose.WorkflowName},
		{config.EntrypointCoordinator, compose.CoordinatorName},
	} {
		t.Run(string(testCase.entrypoint), func(t *testing.T) {
			t.Parallel()

			loader, err := newAgentLoader(wholeComposition(t, testCase.entrypoint))
			if err != nil {
				t.Fatalf("newAgentLoader() error = %v, want nil", err)
			}
			if got := loader.RootAgent().Name(); got != testCase.wantRoot {
				t.Errorf("RootAgent() = %q, want %q", got, testCase.wantRoot)
			}
			published := slices.Sorted(slices.Values(loader.ListAgents()))
			want := slices.Sorted(slices.Values([]string{testCase.wantRoot, compose.ReportAgentName}))
			if !reflect.DeepEqual(published, want) {
				t.Errorf("ListAgents() = %v, want %v", published, want)
			}
			report, err := loader.LoadAgent(compose.ReportAgentName)
			if err != nil {
				t.Fatalf("LoadAgent(%q) error = %v, want nil", compose.ReportAgentName, err)
			}
			if report.Name() != compose.ReportAgentName {
				t.Errorf("LoadAgent() = %q, want %q", report.Name(), compose.ReportAgentName)
			}
			if _, err := loader.LoadAgent("anything-else"); err == nil {
				t.Error("LoadAgent() error = nil for an agent this binary never published")
			}
		})
	}
}

// TestPolicyTrustsOnlyTheLocalLoadSkillTool covers the governance wiring.
//
// The carve-out is granted to the exact tool value the locally built toolset
// produced. Keying it on a name would hand the boundary to whichever MCP server
// happened to advertise that name.
func TestPolicyTrustsOnlyTheLocalLoadSkillTool(t *testing.T) {
	t.Parallel()

	skills, err := compose.NewSkills(t.Context(), compose.SkillsDir(repositoryDataset))
	if err != nil {
		t.Fatalf("NewSkills() error = %v, want nil", err)
	}
	trusted := []tool.Tool{skills.LoadSkillTool()}
	governance, err := newPolicy(config.Config{SanitizeToolOutput: true}, trusted)
	if err != nil {
		t.Fatalf("newPolicy() error = %v, want nil", err)
	}
	if governance == nil {
		t.Fatal("newPolicy() = nil")
	}
	if _, err := governance.Plugin(); err != nil {
		t.Errorf("Plugin() error = %v, want nil", err)
	}

	// The optional layer-2 analyzer is off unless its URL is set, and a
	// malformed URL is refused rather than silently disabling the layer.
	if _, err := newPolicy(config.Config{PIIAnalyzerURL: "not-a-url"}, trusted); err == nil {
		t.Error("newPolicy() error = nil for an unusable analyzer URL")
	}
	if _, err := newPolicy(config.Config{PIIAnalyzerURL: "http://127.0.0.1:3000"}, trusted); err != nil {
		t.Errorf("newPolicy() error = %v, want nil for a usable analyzer URL", err)
	}
}

// TestTheFailureReportNamesTheLauncherSyntaxOnlyForTheLauncher pins where an
// operator is sent after a startup failure.
//
// A prompt-registry pin nobody resolved is the cheapest offline way to make the
// composition refuse: there is no registry client in this runtime, so a pin is
// refused rather than silently replaced by the committed instruction.
func TestTheFailureReportNamesTheLauncherSyntaxOnlyForTheLauncher(t *testing.T) {
	isolatedEnvironment(t)
	const promptPin = "prompts:/agentops-agent-instruction/2"
	t.Setenv(config.EnvPromptURI, promptPin)

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), nil, &out, &errOut); code != config.ExitInvalid {
		t.Fatalf("execute() = %d, want %d; stderr: %s", code, config.ExitInvalid, errOut.String())
	}
	if !strings.Contains(errOut.String(), promptPin) {
		t.Errorf("stderr = %q, want it to quote the pin that refused", errOut.String())
	}
	// An operator who sees a startup failure needs to know how to reach the
	// other modes, config:check among them.
	if !strings.Contains(errOut.String(), "console") {
		t.Error("the failure report omits the launcher syntax")
	}
}

// offlineToolSurface builds the eight function tools over the committed dataset
// and a disposable state directory.
//
// Nothing here touches the filesystem: the tools are constructed, not called,
// and the memory plane opens its databases lazily on first use.
func offlineToolSurface(t *testing.T, entrypoint config.Entrypoint) (compose.Tools, *memory.Memory) {
	t.Helper()

	cfg := offlineConfig(t, entrypoint)
	store := data.New(data.Config{DataDir: cfg.DataDir, StateDir: cfg.StateDir})
	surface, memories, err := newToolSurface(cfg, store, passThroughGuard, func(text string) string { return text })
	if err != nil {
		t.Fatalf("newToolSurface() error = %v, want nil", err)
	}
	return surface, memories
}

// wholeComposition builds a composer with every tool the compositions require,
// over a model that answers nothing: the loader never calls it.
func wholeComposition(t *testing.T, entrypoint config.Entrypoint) *compose.Compose {
	t.Helper()

	surface, memories := offlineToolSurface(t, entrypoint)
	skills, err := compose.NewSkills(t.Context(), compose.SkillsDir(repositoryDataset))
	if err != nil {
		t.Fatalf("NewSkills() error = %v, want nil", err)
	}
	composer, err := compose.New(compose.Config{
		Model:      silentLLM{},
		Skills:     skills,
		Tools:      surface,
		Memory:     memories.MemoryTools(),
		Entrypoint: entrypoint,
	})
	if err != nil {
		t.Fatalf("compose.New() error = %v, want nil", err)
	}
	return composer
}

// silentLLM is a model that answers nothing; the loader never calls it.
type silentLLM struct{}

func (silentLLM) Name() string { return "silent" }

func (silentLLM) GenerateContent(
	context.Context, *adkmodel.LLMRequest, bool,
) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(nil, errors.New("silentLLM must not be called: the wiring tests are offline"))
	}
}
