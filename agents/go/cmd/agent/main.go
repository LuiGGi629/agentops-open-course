// Command agent is the AgentOps Open Course reference agent.
//
// It is an on-call assistant that helps engineers triage and resolve incidents
// for a fictional platform against a bundled offline dataset. Model requests go
// to a local Ollama over the OpenAI-compatible adapter by default, through
// agentgateway when its URL is selected, or to Gemini when that provider is
// chosen explicitly.
//
// # One binary, one set of surfaces
//
//	agent config:check                 # resolved configuration, secrets masked
//	agent                              # interactive console
//	agent console -streaming_mode=sse
//	agent web -port 8002 webui api     # Dev UI at /ui/, REST at /api
//	agent a2a                          # the deployed A2A contract, on :8080
//	agent mcp                          # the read-only MCP server (MCP_TRANSPORT)
//	agent state backup                 # a versioned snapshot of runtime state
//	agent state restore <snapshot>
//
// The repository's own subcommands are intercepted before ADK's launcher sees
// the arguments, because the launcher routes an unrecognized first argument to
// its default sublauncher — the console — which then reports it as an unparsed
// flag rather than as an unknown command. Everything else is ADK's own
// launcher, which parses its own flags with the standard library's flag
// package. It is deliberately not wrapped in a CLI framework: a wrapper would
// have to re-declare every flag the launcher family owns and would drift from
// it on the next ADK release.
//
// # `agent a2a` and `agent web … a2a` are not the same server
//
// The deployed contract is `agent a2a`: the a2aserver package's own surface,
// with the persistent SQLite task store, the trusted-identity binding, the
// readiness route, and the startup sequence that recovers an interrupted state
// restore before it publishes anything. ADK's `a2a` sublauncher is a different
// binding of the same agent — the framework's own, on the framework's in-memory
// task store — so a task it accepted does not survive a restart. Use it to see
// what the launcher family offers; deploy the other one.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/a2aserver"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/state"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/telemetry"
)

// The subcommands this repository owns. Every other argument list belongs to
// ADK's launcher.
//
// state.Command is not repeated here: the state package names its own
// subcommand, and spelling it twice is how the host scripts and the binary
// would eventually disagree about it.
const (
	configCheckCommand = "config:check"
	mcpCommand         = "mcp"
	a2aCommand         = "a2a"
)

func main() {
	// SIGINT and SIGTERM arrive as a canceled context, which is exactly what
	// both servers treat as a normal shutdown: Ctrl-C in a foreground terminal
	// and Kubernetes draining a pod are the same event to this process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	// Released explicitly rather than with defer: os.Exit runs no deferred call.
	stop()
	os.Exit(code)
}

// execute is main without the exit, so the subcommand dispatch and the failure
// report are provable rather than only reviewable.
func execute(ctx context.Context, arguments []string, out, errOut io.Writer) int {
	// First, before any other work in any mode. ADK reads its content-capture
	// switch through a sync.Once, so a call made after the launcher has built
	// its providers would silently have no effect on this process.
	if err := telemetry.SetContentCaptureDefaults(); err != nil {
		return report(errOut, err, "")
	}

	command := ""
	if len(arguments) > 0 {
		command = arguments[0]
	}
	switch command {
	case configCheckCommand:
		return config.Check(out, errOut)
	case state.Command:
		return state.Main(ctx, arguments[1:], stateEnvironment(), out, errOut)
	case mcpCommand:
		return report(errOut, serveMCP(ctx, arguments[1:], errOut), "")
	case a2aCommand:
		return report(errOut, serveA2A(ctx, arguments[1:], errOut), "")
	default:
		agentLauncher := full.NewLauncher()
		// The launcher returns an error for an unrecognized mode; printing the
		// syntax is the caller's job, and this is the caller.
		return report(errOut, run(ctx, agentLauncher, arguments, errOut), agentLauncher.CommandLineSyntax())
	}
}

// report turns a failure into the process exit code, printing syntax help when
// the caller has some to offer.
//
// The exit codes are the ones config:check already established, so an operator
// reads one table for every subcommand the binary offers.
func report(errOut io.Writer, err error, syntax string) int {
	if err == nil {
		return config.ExitValid
	}
	message := fmt.Sprintf("agent: %v\n", err)
	if syntax != "" {
		message += "\n" + syntax + "\n"
	}
	if _, writeErr := io.WriteString(errOut, message); writeErr != nil {
		return config.ExitUnreportable
	}
	return config.ExitInvalid
}

// run loads the configuration, assembles the runtime, and hands it to ADK.
func run(
	ctx context.Context, agentLauncher launcher.Launcher, arguments []string, console io.Writer,
) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading the configuration (run `%s` for the resolved settings): %w", configCheckCommand, err)
	}
	// ADK's launcher installs the tracer and logger providers itself, from the
	// options this runtime hands it.
	assembled, err := newAgentRuntime(ctx, cfg, console, launcherInstallsProviders)
	if err != nil {
		return err
	}
	defer assembled.close(ctx)

	launcherConfig, err := assembled.launcherConfig()
	if err != nil {
		return err
	}
	if err := agentLauncher.Execute(ctx, launcherConfig, arguments); err != nil {
		return fmt.Errorf("running the agent: %w", err)
	}
	return nil
}

// launcherConfig publishes the runtime through ADK's launcher family.
func (r *agentRuntime) launcherConfig() (*launcher.Config, error) {
	agentLoader, err := newAgentLoader(r.compositions)
	if err != nil {
		return nil, err
	}
	governancePlugin, err := r.governance.Plugin()
	if err != nil {
		return nil, err
	}
	sessions, err := newSessionService(r.config)
	if err != nil {
		return nil, err
	}
	return &launcher.Config{
		AgentLoader:    agentLoader,
		SessionService: sessions,
		// The long-term note store, so ADK's own load_memory and preload_memory
		// resolve against the same per-user, per-application store the two
		// bespoke recall tools use.
		MemoryService: r.memories.Service(),
		// One plugin, attached once, at the application boundary. Its hooks fire
		// for every agent, sub-agent and workflow node the launcher runs, which
		// is what makes adding an agent unable to lose the policy.
		PluginConfig: runner.PluginConfig{Plugins: []*plugin.Plugin{governancePlugin}},
		// ADK builds the tracer and logger providers from the standard
		// OTEL_EXPORTER_OTLP_* variables; this pins what they are attributed to,
		// so a span, a log record and a metric all name the same service and
		// version. The meter provider is ours — ADK v2.1.0 builds none.
		TelemetryOptions: r.telemetryOptions,
	}, nil
}

// buildVersion reports the module version stamped into the binary.
//
// A `go build` from a source checkout stamps "(devel)" or nothing at all, so
// both resolve to the string the A2A card already publishes in that situation
// and the resource, the card and the log records agree on one answer.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return a2aserver.UnknownVersion
	}
	return info.Main.Version
}
