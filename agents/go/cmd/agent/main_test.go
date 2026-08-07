package main

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/compose"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/data"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/domain"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/resilience"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/tools"
)

// These tests are offline: no model is called and no port is opened. What they
// prove is the wiring — that the subcommand reaches the configuration checker
// without the launcher parsing it, that an incomplete tool surface refuses to
// start rather than serving a degraded agent, and that sessions are actually
// persistent.

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
	t.Setenv(config.EnvDataDir, filepath.Join("..", "..", "..", "data"))
	t.Setenv(config.EnvStateDir, t.TempDir())
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

// TestAnIncompleteToolSurfaceRefusesToStart records, as an executable fact, the
// one gap this binary still has.
//
// The runbook-knowledge tools and the long-term memory tools are built by the
// memory package, which is not part of this module yet. The root instruction
// tells the model to call all four, so the binary refuses to assemble the
// composition rather than starting an agent that would answer four of its own
// documented capabilities with a hallucination. When memory lands, filling
// those fields in newLauncherConfig is the entire change.
func TestAnIncompleteToolSurfaceRefusesToStart(t *testing.T) {
	isolatedEnvironment(t)

	var out, errOut bytes.Buffer
	if code := execute(t.Context(), nil, &out, &errOut); code != config.ExitInvalid {
		t.Fatalf("execute() = %d, want %d", code, config.ExitInvalid)
	}
	for _, want := range []string{
		"Tools." + compose.GetRunbookToolName,
		"Tools." + compose.SearchRunbooksToolName,
		compose.RecallIncidentContextToolName,
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr = %q, want it to name the missing %s", errOut.String(), want)
		}
	}
	// The launcher's syntax is printed alongside, because an operator who sees
	// a startup failure needs to know how to reach config:check.
	if !strings.Contains(errOut.String(), "console") {
		t.Error("the failure report omits the launcher syntax")
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

	skills, err := compose.NewSkills(t.Context(), compose.SkillsDir(filepath.Join("..", "..", "..", "data")))
	if err != nil {
		t.Fatalf("NewSkills() error = %v, want nil", err)
	}
	governance, err := newPolicy(config.Config{SanitizeToolOutput: true}, skills)
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
	if _, err := newPolicy(config.Config{PIIAnalyzerURL: "not-a-url"}, skills); err == nil {
		t.Error("newPolicy() error = nil for an unusable analyzer URL")
	}
	if _, err := newPolicy(config.Config{PIIAnalyzerURL: "http://127.0.0.1:3000"}, skills); err != nil {
		t.Errorf("newPolicy() error = %v, want nil for a usable analyzer URL", err)
	}
}

// wholeComposition builds a composer with every tool the compositions require.
//
// The four tools the memory package will own are stood in for here, and only
// here: this is a test of the loader, not of the tool surface, and standing them
// in is what lets the loader be tested before that package lands.
func wholeComposition(t *testing.T, entrypoint config.Entrypoint) *compose.Compose {
	t.Helper()

	agentTools, err := tools.New(tools.Config{
		Store:  refusingStore{},
		Guard:  func(ctx context.Context, _ string, call func(context.Context) error) error { return call(ctx) },
		Redact: func(text string) string { return text },
	})
	if err != nil {
		t.Fatalf("tools.New() error = %v, want nil", err)
	}
	skills, err := compose.NewSkills(t.Context(), compose.SkillsDir(filepath.Join("..", "..", "..", "data")))
	if err != nil {
		t.Fatalf("NewSkills() error = %v, want nil", err)
	}
	composer, err := compose.New(compose.Config{
		Model:  silentLLM{},
		Skills: skills,
		Tools: compose.Tools{
			ListIncidents:     agentTools.ListIncidents(),
			GetIncident:       agentTools.GetIncident(),
			GetServiceStatus:  agentTools.GetServiceStatus(),
			SearchServiceLogs: agentTools.SearchServiceLogs(),
			GetRunbook:        stubTool(compose.GetRunbookToolName),
			SearchRunbooks:    stubTool(compose.SearchRunbooksToolName),
			RestartService:    agentTools.RestartService(),
			ResolveIncident:   agentTools.ResolveIncident(),
		},
		Memory: []tool.Tool{
			stubTool(compose.RecallIncidentContextToolName),
			stubTool(compose.SaveIncidentNoteToolName),
		},
		Entrypoint: entrypoint,
	})
	if err != nil {
		t.Fatalf("compose.New() error = %v, want nil", err)
	}
	return composer
}

// stubTool stands in for a tool the memory package will build.
type stubTool string

func (t stubTool) Name() string        { return string(t) }
func (t stubTool) Description() string { return string(t) + " (test double)" }
func (t stubTool) IsLongRunning() bool { return false }

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

// refusingStore satisfies the tools package's Store seam without a database.
type refusingStore struct{}

var errNoStore = errors.New("refusingStore: the wiring tests never run a tool")

func (refusingStore) ListIncidents(context.Context, data.IncidentFilter) ([]domain.Incident, error) {
	return nil, errNoStore
}

func (refusingStore) GetIncident(context.Context, domain.IncidentID) (*domain.Incident, error) {
	return nil, errNoStore
}

func (refusingStore) ListServices(context.Context) ([]domain.Service, error) { return nil, errNoStore }

func (refusingStore) GetService(context.Context, domain.Slug) (*domain.Service, error) {
	return nil, errNoStore
}

func (refusingStore) ReadServiceLogs(string) ([]string, bool, error) { return nil, false, errNoStore }

func (refusingStore) LogsDir() string { return "" }

func (refusingStore) RestartServiceWithAudit(
	context.Context, domain.Slug, data.AuditIdentity,
) (*domain.AuditEntry, error) {
	return nil, errNoStore
}

func (refusingStore) ResolveIncidentWithAudit(
	context.Context, domain.IncidentID, data.AuditIdentity,
) (*domain.AuditEntry, error) {
	return nil, errNoStore
}
