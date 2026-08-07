// Command agent is the AgentOps Open Course reference agent.
//
// It is an on-call assistant that helps engineers triage and resolve incidents
// for a fictional platform against a bundled offline dataset. Model requests go
// to a local Ollama over the OpenAI-compatible adapter by default, through
// agentgateway when its URL is selected, or to Gemini when that provider is
// chosen explicitly.
//
// # Two entrypoints into one process
//
//	agent config:check                 # resolved configuration, secrets masked
//	agent                              # interactive console
//	agent console -streaming_mode=sse
//	agent web webui api                # Dev UI at /ui/, REST at /api
//	agent web api a2a -port 8080       # the deployed contract
//
// Everything except config:check is ADK's own launcher, which parses its own
// flags with the standard library's flag package. It is deliberately not
// wrapped in a CLI framework: a wrapper would have to re-declare every flag the
// launcher family owns and would drift from it on the next ADK release.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/compose"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/data"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/model"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/policy"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/resilience"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/tools"
)

// configCheckCommand is the subcommand `mise run config:check` invokes.
//
// It is intercepted before the launcher sees the arguments, because the
// launcher routes an unrecognized first argument to its default sublauncher —
// the console — which then reports it as an unparsed flag rather than as an
// unknown command.
const configCheckCommand = "config:check"

// sessionDatabaseName is the runtime session store inside AGENT_STATE_DIR. It
// shares a name with the Python track's file so a learner comparing the two
// tracks finds the same artifact in the same place.
const sessionDatabaseName = "runtime.db"

func main() {
	os.Exit(execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// execute is main without the exit, so the subcommand dispatch and the failure
// report are provable rather than only reviewable.
func execute(ctx context.Context, arguments []string, out, errOut io.Writer) int {
	if len(arguments) > 0 && arguments[0] == configCheckCommand {
		return config.Check(out, errOut)
	}
	agentLauncher := full.NewLauncher()
	err := run(ctx, agentLauncher, arguments)
	if err == nil {
		return config.ExitValid
	}
	// The launcher returns an error for an unrecognized mode; printing the
	// syntax is the caller's job, and this is the caller. The exit codes are the
	// ones config:check already established, so an operator reads one table.
	if _, writeErr := fmt.Fprintf(errOut, "agent: %v\n\n%s\n", err, agentLauncher.CommandLineSyntax()); writeErr != nil {
		return config.ExitUnreportable
	}
	return config.ExitInvalid
}

// run loads the configuration, assembles the runtime, and hands it to ADK.
func run(ctx context.Context, agentLauncher launcher.Launcher, arguments []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading the configuration (run `%s` for the resolved settings): %w", configCheckCommand, err)
	}
	launcherConfig, err := newLauncherConfig(ctx, cfg)
	if err != nil {
		return err
	}
	return agentLauncher.Execute(ctx, launcherConfig, arguments)
}

// newLauncherConfig assembles every plane of the runtime, in dependency order.
//
// The order is not incidental. The skill toolset has to exist before the policy
// plane, because the trust carve-out is keyed on the identity of the load_skill
// tool that toolset built. The policy plane has to exist before the tools,
// because a rationale is redacted by the policy before it reaches the
// append-only audit trail. And the tools have to exist before the compositions,
// because least-privilege delegation is a statement about which tool values
// each agent holds.
func newLauncherConfig(ctx context.Context, cfg config.Config) (*launcher.Config, error) {
	llm, err := model.Build(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("building the model: %w", err)
	}

	guard, err := newGuard(cfg)
	if err != nil {
		return nil, err
	}

	skills, err := compose.NewSkills(ctx, compose.SkillsDir(cfg.DataDir))
	if err != nil {
		return nil, err
	}

	governance, err := newPolicy(cfg, skills)
	if err != nil {
		return nil, err
	}

	store := data.New(data.Config{DataDir: cfg.DataDir, StateDir: cfg.StateDir})
	agentTools, err := tools.New(tools.Config{
		Store:  store,
		Guard:  guard.Run,
		Redact: governance.PersistedRedactor(ctx),
		// Read once at startup, exactly as the setting documents itself: roll
		// the value out and restart to freeze mutations while reads keep
		// working.
		WritesDisabled: cfg.WritesDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("building the tools: %w", err)
	}

	mcpToolset, err := newMCPToolset(cfg)
	if err != nil {
		return nil, err
	}

	compositions, err := compose.New(compose.Config{
		Model:                 llm,
		GenerateContentConfig: model.GenerationConfig(cfg),
		Skills:                skills,
		MCP:                   mcpToolset,
		Tools: compose.Tools{
			ListIncidents:     agentTools.ListIncidents(),
			GetIncident:       agentTools.GetIncident(),
			GetServiceStatus:  agentTools.GetServiceStatus(),
			SearchServiceLogs: agentTools.SearchServiceLogs(),
			RestartService:    agentTools.RestartService(),
			ResolveIncident:   agentTools.ResolveIncident(),
			// GetRunbook, SearchRunbooks and Memory below are built by the
			// memory package (MIGRATE.md §7 — memory.py, longterm.py, and the
			// retrieval behind them), which is not part of this module yet.
			// They are left unset rather than substituted, so compose.New
			// refuses the wiring by name instead of returning an agent whose
			// own instruction tells the model to call four tools it does not
			// hold.
			GetRunbook:     nil,
			SearchRunbooks: nil,
		},
		Memory:     nil,
		PromptURI:  cfg.PromptURI,
		Entrypoint: cfg.Entrypoint,
	})
	if err != nil {
		return nil, fmt.Errorf("wiring the compositions: %w", err)
	}

	agentLoader, err := newAgentLoader(compositions)
	if err != nil {
		return nil, err
	}

	sessions, err := newSessionService(cfg)
	if err != nil {
		return nil, err
	}

	governancePlugin, err := governance.Plugin()
	if err != nil {
		return nil, err
	}
	return &launcher.Config{
		AgentLoader:    agentLoader,
		SessionService: sessions,
		// One plugin, attached once, at the application boundary. Its hooks fire
		// for every agent, sub-agent and workflow node the launcher runs, which
		// is what makes adding an agent unable to lose the policy.
		PluginConfig: runner.PluginConfig{Plugins: []*plugin.Plugin{governancePlugin}},
	}, nil
}

// newGuard builds the read-tool resilience policy: a deadline, bounded retries,
// and — only when the setting is on — a per-tool circuit breaker.
func newGuard(cfg config.Config) (*resilience.Guard, error) {
	var breakers *resilience.Breakers
	if cfg.CircuitBreakerEnabled {
		var err error
		breakers, err = resilience.NewBreakers(resilience.BreakersConfig{
			FailureThreshold: cfg.CircuitFailureThreshold,
			ResetTimeout:     cfg.CircuitResetTimeout.Duration(),
		})
		if err != nil {
			return nil, fmt.Errorf("building the circuit breakers: %w", err)
		}
	}
	guard, err := resilience.NewGuard(resilience.Config{
		Breakers:     breakers,
		ToolTimeout:  cfg.ToolTimeout.Duration(),
		RetryBackoff: cfg.RetryBackoff.Duration(),
		MaxRetries:   cfg.MaxRetries,
	})
	if err != nil {
		return nil, fmt.Errorf("building the resilience guard: %w", err)
	}
	return guard, nil
}

// newPolicy builds the governance plane over the configuration.
func newPolicy(cfg config.Config, skills *compose.Skills) (*policy.Policy, error) {
	var analyzer *policy.Analyzer
	if cfg.PIIAnalyzerURL != "" {
		var err error
		analyzer, err = policy.NewAnalyzer(policy.AnalyzerConfig{Endpoint: cfg.PIIAnalyzerURL})
		if err != nil {
			return nil, fmt.Errorf("building the PII analyzer client: %w", err)
		}
	}
	governance, err := policy.New(policy.Config{
		Analyzer:            analyzer,
		MaxHistoryMessages:  cfg.MaxHistoryMessages,
		MaxTokensPerSession: cfg.MaxTokensPerSession,
		ActionableError:     actionableError,
		// The carve-out is the exact tool value the locally built toolset
		// produced, never a name: a name is something a remote MCP server picks
		// for itself, and a trust boundary keyed on an attacker's choice is not
		// a trust boundary.
		TrustedInstructionTools: []tool.Tool{skills.LoadSkillTool()},
		InputPricePer1K:         cfg.InputPricePer1K,
		OutputPricePer1K:        cfg.OutputPricePer1K,
		WritesDisabled:          cfg.WritesDisabled,
		SanitizeToolOutput:      cfg.SanitizeToolOutput,
	})
	if err != nil {
		return nil, fmt.Errorf("building the policy plane: %w", err)
	}
	return governance, nil
}

// actionableError classifies the failures whose own message is safe to hand
// back to the model verbatim.
//
// These four are first-party, carry no untrusted content, and name the setting
// to change. Everything else stays opaque, because an arbitrary message may
// embed a query, a path or a driver detail that should not reach a model.
func actionableError(err error) bool {
	var circuitOpen *resilience.CircuitOpenError
	var deadline *resilience.DeadlineError
	var exhausted *resilience.RetriesExhaustedError
	return errors.As(err, &circuitOpen) ||
		errors.As(err, &deadline) ||
		errors.As(err, &exhausted) ||
		errors.Is(err, data.ErrDataAccess)
}

// newMCPToolset builds the governed MCP route, or nil when no URL is set.
//
// Nil is the account-free default: every read runs in process, under the local
// deadline, retries and circuit breaker. Only the conversational entrypoint
// consumes this — see the compose package doc.
func newMCPToolset(cfg config.Config) (tool.Toolset, error) {
	if cfg.MCPURL == "" {
		return nil, nil
	}
	toolset, err := compose.NewMCPToolset(compose.MCPConfig{
		Endpoint: cfg.MCPURL,
		Token:    cfg.MCPToken,
		Timeout:  cfg.ToolTimeout.Duration(),
	})
	if err != nil {
		return nil, err
	}
	return toolset, nil
}

// newAgentLoader publishes the selected entrypoint as the root agent and the
// structured report beside it.
//
// The report is a second, independently addressable agent rather than a mode of
// the root one, which is what lets an evaluation drive the schema path over the
// REST surface without changing AGENT_ENTRYPOINT — the same property the Python
// track got from its separate discovery package. The console and the A2A card
// still serve the root agent alone.
func newAgentLoader(compositions *compose.Compose) (adkagent.Loader, error) {
	root, err := compositions.RootAgent()
	if err != nil {
		return nil, err
	}
	report, err := compositions.TriageReportAgent()
	if err != nil {
		return nil, err
	}
	loader, err := adkagent.NewMultiLoader(root, report)
	if err != nil {
		return nil, fmt.Errorf("publishing the agents: %w", err)
	}
	return loader, nil
}

// newSessionService opens the persistent session store.
//
// Sessions are durable state, not dataset: they live in AGENT_STATE_DIR beside
// the runtime incident database, never in the committed dataset directory. The
// driver is the pure-Go SQLite dialector, so the binary keeps one SQLite
// implementation and no cgo.
func newSessionService(cfg config.Config) (session.Service, error) {
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating the state directory %s: %w", cfg.StateDir, err)
	}
	path := filepath.Join(cfg.StateDir, sessionDatabaseName)

	// busy_timeout makes a concurrent writer wait for the lock instead of
	// failing the request: SQLite admits one writer, and the A2A server and the
	// console can both be appending events.
	sessions, err := database.NewSessionService(sqlite.Open("file:" + path + "?_pragma=busy_timeout(5000)"))
	if err != nil {
		return nil, fmt.Errorf("opening the session store %s: %w", path, err)
	}
	// ADK never migrates for you; the launcher will not either.
	if err := database.AutoMigrate(sessions); err != nil {
		return nil, fmt.Errorf("migrating the session store %s: %w", path, err)
	}
	return sessions, nil
}
