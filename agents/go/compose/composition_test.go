package compose

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"google.golang.org/adk/v2/tool"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/tools"
)

// rootInstructionDigest is the digest the Python suite pins in
// tests/test_smoke.py. Matching it here is the proof that the instruction — 3139
// bytes of course content, including its backticks, its em dash and its
// trailing newline — crossed the port byte for byte.
const rootInstructionDigest = "7f2f16e2c2a31e92891ea89dcaefc2427a32f729af4ecc68f7c8a179c29f6c75"

// TestRootInstructionPromptContract is the Go port of
// test_root_instruction_prompt_contract.
//
// The digest alone would catch any change; the ordering and phrase assertions
// exist so the requirements the strict evalsets depend on stay legible instead
// of hiding behind a hash.
func TestRootInstructionPromptContract(t *testing.T) {
	t.Parallel()

	instruction := Instruction()
	if got := digest(instruction); got != rootInstructionDigest {
		t.Errorf(
			"instruction digest = %s, want %s — the prompt changed; "+
				"re-run the evaluation and update the digest after reviewing",
			got, rootInstructionDigest,
		)
	}

	recall := strings.Index(instruction, "call `"+RecallIncidentContextToolName+"`")
	incident := strings.Index(instruction, "call `"+tools.GetIncidentToolName+"`")
	if recall < 0 || incident < 0 {
		t.Fatalf("instruction does not prescribe both reads (recall=%d, incident=%d)", recall, incident)
	}
	if recall > incident {
		t.Error("the instruction must ask for the decision context before the incident read")
	}

	for _, phrase := range []string{
		"call `" + RecallIncidentContextToolName + "` and wait before",
		"then `" + LoadSkillToolName + "`",
		"your next model output must be the matching guarded tool call",
		"re-read the incident and affected service",
	} {
		if !strings.Contains(instruction, phrase) {
			t.Errorf("instruction lost the phrase %q the evalsets depend on", phrase)
		}
	}
}

// backtickedTerm matches the `snake_case` terms the instruction quotes.
var backtickedTerm = regexp.MustCompile("`([a-z][a-z0-9_]*)`")

// TestRootInstructionNamesOnlyToolsTheAgentHolds is the contract the Python
// track never had to state, because its instruction and its tool list lived in
// one module.
//
// Every tool the instruction tells the model to call must actually be bound. An
// instruction that names a tool the agent does not hold does not merely waste
// tokens: it teaches the model that a named capability can be absent, which is
// exactly the habit that produces a hallucinated tool call.
func TestRootInstructionNamesOnlyToolsTheAgentHolds(t *testing.T) {
	t.Parallel()

	// The instruction also quotes two field names of the incident record. They
	// are data, not capabilities, and are listed here so the sweep below stays
	// exhaustive rather than approximate.
	notTools := []string{"service", "runbook"}

	cfg := newCompose(t).conversationalConfig()
	bound := toolNames(cfg.Tools)
	for _, toolset := range cfg.Toolsets {
		listed, err := toolset.Tools(nil)
		if err != nil {
			t.Fatalf("Tools() error = %v, want nil", err)
		}
		bound = append(bound, toolNames(listed)...)
	}

	for _, match := range backtickedTerm.FindAllStringSubmatch(Instruction(), -1) {
		term := match[1]
		if slices.Contains(notTools, term) || slices.Contains(bound, term) {
			continue
		}
		t.Errorf("the instruction tells the model to use %q, which the agent does not hold; bound: %v", term, bound)
	}
}

// TestConversationalAgentBindsTheLocalToolsInOrder pins the default, account-free
// wiring: every read runs in process, and the registration order matches the
// Python track's exactly.
func TestConversationalAgentBindsTheLocalToolsInOrder(t *testing.T) {
	t.Parallel()

	cfg := newCompose(t).conversationalConfig()
	want := []string{
		tools.ListIncidentsToolName,
		tools.GetIncidentToolName,
		tools.GetServiceStatusToolName,
		tools.SearchServiceLogsToolName,
		GetRunbookToolName,
		SearchRunbooksToolName,
		tools.RestartServiceToolName,
		tools.ResolveIncidentToolName,
		RecallIncidentContextToolName,
		SaveIncidentNoteToolName,
	}
	if got := toolNames(cfg.Tools); !reflect.DeepEqual(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
	if len(cfg.Toolsets) != 1 {
		t.Fatalf("toolsets = %d, want 1 (the skills)", len(cfg.Toolsets))
	}
	if cfg.Toolsets[0].Name() != "skills" {
		t.Errorf("toolset = %q, want the skill toolset", cfg.Toolsets[0].Name())
	}
}

// TestMCPRouteReplacesOnlyTheReadTools is the Go port of the _read_tools switch
// and the AGENTS.md invariant behind it.
//
// It is either-or by design: with a governed route configured, the six reads are
// served over MCP and their local counterparts are gone. What must not move is
// everything else — the two guarded writes, the memory tools and the skills stay
// in process, because a remote server must never be able to originate a
// mutation, reach the per-user store, or supply the reviewed instruction the
// policy plane trusts.
func TestMCPRouteReplacesOnlyTheReadTools(t *testing.T) {
	t.Parallel()

	route := fakeToolset{name: "mcp"}
	cfg := newCompose(t, func(cfg *Config) { cfg.MCP = route }).conversationalConfig()

	want := []string{
		tools.RestartServiceToolName,
		tools.ResolveIncidentToolName,
		RecallIncidentContextToolName,
		SaveIncidentNoteToolName,
	}
	if got := toolNames(cfg.Tools); !reflect.DeepEqual(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
	if len(cfg.Toolsets) != 2 {
		t.Fatalf("toolsets = %d, want 2 (the route then the skills)", len(cfg.Toolsets))
	}
	if cfg.Toolsets[0].Name() != "mcp" || cfg.Toolsets[1].Name() != "skills" {
		t.Errorf("toolsets = %q, want the route before the skills",
			[]string{cfg.Toolsets[0].Name(), cfg.Toolsets[1].Name()})
	}
}

// TestOnlyTheConversationalEntrypointTakesTheMCPRoute pins the other half of
// the same invariant: a specialist's tool set is the boundary the
// least-privilege claim rests on, so letting a remote server decide which tools
// a specialist holds would hand that boundary to the server.
func TestOnlyTheConversationalEntrypointTakesTheMCPRoute(t *testing.T) {
	t.Parallel()

	composer := newCompose(t, func(cfg *Config) { cfg.MCP = fakeToolset{name: "mcp"} })
	for name, cfg := range allConfigs(t, composer) {
		if name == AgentName {
			continue
		}
		for _, toolset := range cfg.Toolsets {
			if toolset.Name() == "mcp" {
				t.Errorf("%s binds the MCP route; only the conversational entrypoint may", name)
			}
		}
		if slices.Contains(toolNames(cfg.Tools), "") {
			t.Errorf("%s holds an unnamed tool", name)
		}
	}
}

// TestConversationalToolsAreTheRealToolValues pins identity rather than names.
//
// The two guarded writes are the same values the tools package built and proved
// pause for human confirmation. Comparing by name would accept a look-alike; a
// name is exactly what a remote MCP server chooses for itself.
func TestConversationalToolsAreTheRealToolValues(t *testing.T) {
	t.Parallel()

	composer := newCompose(t)
	surface := composer.tools
	cfg := composer.conversationalConfig()
	for _, want := range []tool.Tool{surface.RestartService, surface.ResolveIncident} {
		if !slices.ContainsFunc(cfg.Tools, func(bound tool.Tool) bool { return bound == want }) {
			t.Errorf("the agent does not hold the %s value the tools package built", want.Name())
		}
	}
}
