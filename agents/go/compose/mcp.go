package compose

import (
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/internal/httpguard"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/tools"
)

// Consume the AgentOps Agent MCP server as a client toolset (Chapter 3.3).
//
// An MCP toolset connects to an MCP server and adapts its tools into ADK tools
// an agent can call — no change to the agent beyond adding the toolset. This is
// the seam the gateway later slots into (Chapter 5.2): point the toolset at the
// gateway instead of at the raw server and nothing else moves.

// ErrMCP marks a failure to build the MCP toolset.
var ErrMCP = errors.New("building the MCP toolset")

// MCPReadToolNames returns the exact read tools this agent will accept from an
// MCP server, in the order the server registers them.
//
// This is a least-privilege allowlist, not documentation. A tool's name,
// description and schema reach the model as instruction text at connection
// time, so a compromised or swapped server can inject prose merely by
// registering a new tool. Pinning the set means a renamed or added tool is
// never offered to the model at all. The skill toolset applies the same rule,
// and the infrastructure gate asserts the gateway's allowlist matches this one.
//
// It is a function rather than a variable for the reason [github.com/MLOps-Courses/agentops-open-course-go/agents/go/domain.Reference]
// is: a package-level slice could be reordered or truncated in place by any
// importer, and an allowlist an importer can edit is not an allowlist.
func MCPReadToolNames() []string {
	return []string{
		tools.ListIncidentsToolName,
		tools.GetIncidentToolName,
		tools.GetServiceStatusToolName,
		tools.SearchServiceLogsToolName,
		GetRunbookToolName,
		SearchRunbooksToolName,
	}
}

// MCPConfig selects and bounds the transport to the MCP server.
type MCPConfig struct {
	// Command runs the server as a child process over stdio. It is used only
	// when Endpoint is empty, and it is supplied by the caller because the
	// server binary is the caller's to name.
	Command *exec.Cmd

	// Endpoint is the streamable-HTTP URL of the server or the gateway route in
	// front of it (AGENT_MCP_URL). It takes precedence over Command.
	Endpoint string

	// Token authenticates the caller to a secured gateway route (Chapter 5.5).
	// Empty sends no Authorization header, which is what the default local
	// route expects.
	Token config.Secret

	// Timeout bounds one HTTP exchange with the server (AGENT_TOOL_TIMEOUT_S).
	// It is required on the HTTP transport: a hung gateway must fail a tool
	// call fast instead of hanging the whole turn.
	Timeout time.Duration
}

// NewMCPToolset returns the governed read toolset over HTTP or local stdio.
//
// The tool filter pins which tools may be offered, so a server cannot widen the
// agent's surface — or reach the model with new description text — by adding
// one. Filtering happens inside the toolset, before a tool joins the slice, so
// a rejected tool's declaration never reaches a request.
// --8<-- [start:ops-mcp-toolset]
func NewMCPToolset(cfg MCPConfig) (tool.Toolset, error) {
	transport, credentials, err := cfg.transport()
	if err != nil {
		return nil, err
	}
	built, err := mcptoolset.New(mcptoolset.Config{
		Transport: transport,
		Auth:      credentials,
		// The in-Config filter rather than tool.FilterToolset. Both work here —
		// unlike the skill toolset, this one has no catalog injection for an
		// external wrapper to drop — and the in-Config one runs before the tool
		// is wrapped, which is the earlier of the two boundaries.
		ToolFilter: tool.AllowedToolsPredicate(MCPReadToolNames()),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMCP, err)
	}
	return built, nil
}

// --8<-- [end:ops-mcp-toolset]

// transport chooses the transport and its credentials.
func (cfg MCPConfig) transport() (mcp.Transport, auth.CredentialProvider, error) {
	if cfg.Endpoint != "" {
		if cfg.Timeout <= 0 {
			return nil, nil, fmt.Errorf(
				"%w: Timeout is %s, want a positive duration; a read served over MCP "+
					"with no deadline can hang a turn forever",
				ErrMCP, cfg.Timeout,
			)
		}
		// The deadline lives on the HTTP client because the MCP toolset has no
		// timeout of its own and the SDK's transport has no header or deadline
		// field either. One client, so every exchange with this server shares
		// the deadline and the connection pool.
		//
		// It also caps the transport's long-lived standalone SSE stream, which
		// is therefore torn down and reconnected every AGENT_TOOL_TIMEOUT_S.
		// That is the accepted cost of the guarantee: this agent only calls
		// read tools and never waits on a server-initiated message, whereas a
		// tool call with no deadline can hang a whole turn.
		transport := &mcp.StreamableClientTransport{
			Endpoint:   cfg.Endpoint,
			HTTPClient: httpguard.NoRedirects(&http.Client{Timeout: cfg.Timeout}),
		}
		if cfg.Token == "" {
			return transport, nil, nil
		}
		// The bearer token goes through the credential provider rather than a
		// header, because the SDK's transport has no headers field and ADK's
		// provider wraps the client's round-tripper so the header is applied to
		// reconnects too.
		return transport, auth.StaticToken(cfg.Token.Reveal()), nil
	}
	if cfg.Command != nil {
		// No credential provider: combining one with a child process is a
		// configuration error in ADK, and a process this agent starts itself
		// needs no bearer token to prove who it is.
		return &mcp.CommandTransport{Command: cfg.Command}, nil, nil
	}
	return nil, nil, fmt.Errorf(
		"%w: neither Endpoint nor Command is set; there is nothing to connect to", ErrMCP,
	)
}

// LocalReadToolNames returns the names of the six local read and runbook tools
// the MCP route replaces, in the order the agent registers them.
//
// It exists so [MCPReadToolNames] can be checked against the surface it stands
// in for: the allowlist is only least-privilege if it is also complete, and a
// local tool missing from it would silently disappear the moment AGENT_MCP_URL
// is set.
func (t Tools) LocalReadToolNames() []string {
	named := slices.Concat(t.ReadTools(), t.KnowledgeTools())
	names := make([]string, 0, len(named))
	for _, candidate := range named {
		names = append(names, candidate.Name())
	}
	return names
}
