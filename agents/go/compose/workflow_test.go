package compose

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"google.golang.org/adk/v2/workflow"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/tools"
)

// The digests the Python suite pins in tests/test_workflow.py.
const (
	planInstructionDigest           = "290cfa7d7b04aa3321ec27f0089fc1771fc5ac739c98b32f3dc25f08cddf9129"
	investigateInstructionDigest    = "05b2f9cfa2ba5882b819eb3eda715e28215e40cd36a5b821442d07e52c0bc27f"
	evidenceReviewInstructionDigest = "506d4bf2bc6a5ca7061e87f81132ad3b0186a8edb67519235c3683472ff4125c"
	recommendInstructionDigest      = "60f917ec16911ac8a5cb48c57ffdf3ed6f8ca7ee85c9afbd30790ad2d7b9e2a1"
)

// TestWorkflowChainsFourStagesInOrder is the Go port of
// test_workflow_chains_four_steps_in_order.
//
// The Python track expressed the graph as a single chain tuple beginning with
// the literal "START". ADK Go's runtime takes pairwise edges instead, so the
// assertion is made on the edges the chain produced: four of them, from the
// Start sentinel through the four stages, and no branch anywhere.
func TestWorkflowChainsFourStagesInOrder(t *testing.T) {
	t.Parallel()

	composer := newCompose(t)
	stages := composer.workflowStageConfigs()
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		names = append(names, stage.Name)
	}
	want := []string{PlanStageName, InvestigateStageName, EvidenceReviewStageName, RecommendStageName}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("stages = %v, want %v", names, want)
	}

	graph, err := composer.TriageWorkflow()
	if err != nil {
		t.Fatalf("TriageWorkflow() error = %v, want nil", err)
	}
	if graph.Name() != WorkflowName {
		t.Errorf("Name() = %q, want %q", graph.Name(), WorkflowName)
	}
	if graph.Description() != WorkflowDescription {
		t.Errorf("Description() = %q, want %q", graph.Description(), WorkflowDescription)
	}
}

// TestWorkflowEdgesAreALinearReadOnlyChain asserts the topology directly rather
// than through the agent wrapper, which hides it.
//
// The bound is the whole lesson of this chapter: four edges, each unconditional,
// each from the previous stage to the next, starting at the sentinel. A
// conditional route or a back edge would turn a bounded run into a loop whose
// cost is not known before it starts.
func TestWorkflowEdgesAreALinearReadOnlyChain(t *testing.T) {
	t.Parallel()

	composer := newCompose(t)
	stages := composer.workflowStageConfigs()
	nodes := make([]workflow.Node, 0, len(stages)+1)
	nodes = append(nodes, workflow.Start)
	for _, stage := range stages {
		built, err := newAgent(stage)
		if err != nil {
			t.Fatalf("newAgent(%q) error = %v, want nil", stage.Name, err)
		}
		node, err := workflow.NewAgentNode(built, workflow.NodeConfig{})
		if err != nil {
			t.Fatalf("NewAgentNode(%q) error = %v, want nil", stage.Name, err)
		}
		nodes = append(nodes, node)
	}

	edges := workflow.Chain(nodes...)
	if len(edges) != len(nodes)-1 {
		t.Fatalf("Chain() produced %d edges, want %d", len(edges), len(nodes)-1)
	}
	for i, edge := range edges {
		if edge.From != nodes[i] || edge.To != nodes[i+1] {
			t.Errorf("edge %d = %s -> %s, want %s -> %s",
				i, edge.From.Name(), edge.To.Name(), nodes[i].Name(), nodes[i+1].Name())
		}
		if edge.Route != nil {
			t.Errorf("edge %d carries a route; the chain must be unconditional", i)
		}
	}
	if edges[0].From.Name() != "START" {
		t.Errorf("the chain starts at %q, want the START sentinel", edges[0].From.Name())
	}
	// The node names come from the agents, so the graph and the stage names
	// cannot drift apart.
	if got := edges[len(edges)-1].To.Name(); got != RecommendStageName {
		t.Errorf("the chain ends at %q, want %q", got, RecommendStageName)
	}
}

// TestWorkflowUsesStageSpecificReadOnlyTools is the Go port of
// test_workflow_uses_stage_specific_read_only_tools.
//
// The order inside each stage is asserted, not just the set: list_incidents is
// last in the investigate stage so the model reaches for it only when the plan
// named no incident, and it appears in no other stage at all so nothing
// downstream can reopen discovery and drift to a different target.
func TestWorkflowUsesStageSpecificReadOnlyTools(t *testing.T) {
	t.Parallel()

	writes := []string{
		tools.RestartServiceToolName,
		tools.ResolveIncidentToolName,
		SaveIncidentNoteToolName,
	}
	want := map[string][]string{
		PlanStageName: nil,
		InvestigateStageName: {
			tools.GetIncidentToolName,
			tools.GetServiceStatusToolName,
			tools.SearchServiceLogsToolName,
			GetRunbookToolName,
			tools.ListIncidentsToolName,
		},
		EvidenceReviewStageName: {
			tools.GetIncidentToolName,
			tools.GetServiceStatusToolName,
			tools.SearchServiceLogsToolName,
			GetRunbookToolName,
		},
		RecommendStageName: {GetRunbookToolName},
	}

	for _, stage := range newCompose(t).workflowStageConfigs() {
		t.Run(stage.Name, func(t *testing.T) {
			t.Parallel()

			got := toolNames(stage.Tools)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, want[stage.Name]) {
				t.Errorf("tools = %v, want %v", got, want[stage.Name])
			}
			for _, write := range writes {
				if slices.Contains(got, write) {
					t.Errorf("holds %q; every stage of this workflow is read-only", write)
				}
			}
			if len(stage.Toolsets) != 0 {
				t.Errorf("binds %d toolsets; a stage's surface is exactly its tool list", len(stage.Toolsets))
			}
		})
	}
}

// TestOnlyInvestigateCanReopenDiscovery states the bound the tool table implies,
// as its own assertion, because it is the property a future edit is most likely
// to lose by adding one convenient tool to a later stage.
func TestOnlyInvestigateCanReopenDiscovery(t *testing.T) {
	t.Parallel()

	for _, stage := range newCompose(t).workflowStageConfigs() {
		holds := slices.Contains(toolNames(stage.Tools), tools.ListIncidentsToolName)
		if want := stage.Name == InvestigateStageName; holds != want {
			t.Errorf("%s holds %s = %v, want %v", stage.Name, tools.ListIncidentsToolName, holds, want)
		}
	}
}

// TestWorkflowPromptContracts is the Go port of test_workflow_prompt_contracts
// and test_workflow_eval_dependencies_remain_explicit.
func TestWorkflowPromptContracts(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name        string
		instruction string
		description string
		want        string
		phrases     []string
	}{
		{
			name:        PlanStageName,
			instruction: planInstruction,
			description: planDescription,
			want:        planInstructionDigest,
		},
		{
			name:        InvestigateStageName,
			instruction: investigateInstruction,
			description: investigateDescription,
			want:        investigateInstructionDigest,
			phrases:     []string{"plan controls which evidence to collect; it is not evidence"},
		},
		{
			name:        EvidenceReviewStageName,
			instruction: evidenceReviewInstruction,
			description: evidenceReviewDescription,
			want:        evidenceReviewInstructionDigest,
			phrases:     []string{"unfiltered service logs with " + tools.SearchServiceLogsToolName},
		},
		{
			name:        RecommendStageName,
			instruction: recommendInstruction,
			description: recommendDescription,
			want:        recommendInstructionDigest,
			phrases:     []string{"never call either action"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := digest(testCase.instruction); got != testCase.want {
				t.Errorf(
					"instruction digest = %s, want %s — the prompt changed; "+
						"re-run the evaluation and update the digest after reviewing",
					got, testCase.want,
				)
			}
			for _, phrase := range testCase.phrases {
				if !strings.Contains(testCase.instruction, phrase) {
					t.Errorf("instruction lost the phrase %q the evalsets depend on", phrase)
				}
			}
			if testCase.description == "" {
				t.Error("the stage has no description; the graph node takes its own from the agent")
			}
		})
	}
}
