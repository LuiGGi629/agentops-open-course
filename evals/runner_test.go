package evals

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestRunnerProducesSanitizedReleaseEvidence(t *testing.T) {
	t.Parallel()
	evalset := EvalSet{
		ID: "set", Name: "Set", Description: "synthetic", Digest: strings.Repeat("a", 64),
		Cases: []EvalCase{{
			ID: "case-one",
			Conversation: []Invocation{{
				UserContent:   EvalContent{Parts: []EvalPart{{Text: "secret prompt"}}},
				FinalResponse: EvalContent{Parts: []EvalPart{{Text: "secret reference"}}},
				IntermediateData: IntermediateData{ToolUses: []ExpectedToolCall{{
					Name: "get_incident", Args: map[string]any{"incident_id": "INC-002"},
				}}},
			}},
		}},
	}
	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Run(context.Background(), RunnerConfig{
		EvalSet: evalset,
		Domain:  Domain{Incidents: map[string]Incident{}, Services: map[string]struct{}{}, Runbooks: map[string]struct{}{}},
		RunID:   "run", Source: testSourceEvidence(), Platform: "test-process",
		Model:     ModelEvidence{Provider: "provider", Name: "model"},
		Transport: "rest", MinimumPassRate: 0.33, RequiredCases: []string{"case-one"},
		Recorder: recorder,
		ClientFactory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
			return &fixedClient{turn: Turn{
				Text:          "secret answer",
				ToolCalls:     []ToolCall{{Name: "get_incident", Args: map[string]any{"incident_id": "INC-002", "extra": "secret"}}},
				ToolResponses: []ToolResponse{{Response: map[string]any{"secret": "tool evidence"}}},
				Usage:         Usage{InputTokens: 8, OutputTokens: 2, TotalTokens: 10, ModelCalls: 1},
			}}, func() error { return nil }, nil
		},
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.Passed() || artifact.Summary.MinimumPassRate != 0.33 {
		t.Fatalf("artifact = %+v", artifact)
	}
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret prompt", "secret answer", "secret reference", "tool evidence", "rationale", "error_message"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("artifact leaked %q: %s", forbidden, encoded)
		}
	}
	var decoded RunArtifact
	if decodeErr := json.Unmarshal(encoded, &decoded); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !decoded.Passed() {
		t.Fatalf("serialized artifact cannot independently prove threshold: %+v", decoded.Summary)
	}
	contextLength := int64(8192)
	temperature := 0.0
	ollamaVersion := "ollama version is 0.32.6"
	identity := CostIdentity{
		ModelProvider:            artifact.Model.Provider,
		Model:                    artifact.Model.Name,
		Source:                   artifact.Source,
		ContextLength:            &contextLength,
		OllamaVersion:            &ollamaVersion,
		Temperature:              &temperature,
		PromptSelection:          PromptAuthorityGit,
		EvaluationContractDigest: artifact.EvalSet.Digest,
	}
	observation, err := NewCostObservation(artifact, identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Cases) != 1 || observation.Cases[0] != (CostSample{
		ID: "case-one", Sample: 1, TotalTokens: 10, ModelCalls: 1,
	}) {
		t.Fatalf("cost observation = %+v", observation)
	}
	if observation.PromptSelection != PromptAuthorityGit || observation.ContextLength == nil ||
		*observation.ContextLength != contextLength || observation.OllamaVersion == nil ||
		*observation.OllamaVersion != ollamaVersion || observation.Temperature == nil ||
		*observation.Temperature != temperature {
		t.Fatalf("cost observation lost comparable runtime identity: %+v", observation)
	}
}

func TestRunnerOmitsProviderErrorCodes(t *testing.T) {
	t.Parallel()

	const marker = "SYNTHETIC_PROVIDER_ERROR_CODE_DO_NOT_LOG"
	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(t.Context(), RunnerConfig{
		EvalSet: EvalSet{
			ID: "set", Name: "Set", Digest: strings.Repeat("a", 64),
			Cases: []EvalCase{{
				ID: "provider-failure", Conversation: []Invocation{{
					UserContent:   EvalContent{Parts: []EvalPart{{Text: "question"}}},
					FinalResponse: EvalContent{Parts: []EvalPart{{Text: "reference"}}},
				}},
			}},
		},
		RunID: "run", Source: testSourceEvidence(), Platform: "test-process",
		Model: ModelEvidence{Provider: "provider", Name: "model"}, Transport: "rest",
		Recorder: recorder,
		ClientFactory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
			return &fixedClient{turn: Turn{ErrorCode: marker}}, func() error { return nil }, nil
		},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want provider failure")
	}
	if !strings.Contains(err.Error(), "provider turn failed") {
		t.Fatalf("Run() error = %q, want local provider failure class", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatal("Run() error retained provider-controlled error code")
	}
}

func TestCostObservationPreservesRepeatedSamples(t *testing.T) {
	t.Parallel()
	artifact := RunArtifact{
		Source: testSourceEvidence(), Model: ModelEvidence{Provider: "provider", Name: "model"},
		EvalSet: EvalSetEvidence{ID: "set", Digest: strings.Repeat("a", 64)},
		Cases: []CaseResult{
			{ID: "case", Sample: 1, Usage: Usage{TotalTokens: 10, ModelCalls: 1}},
			{ID: "case", Sample: 2, Usage: Usage{TotalTokens: 11, ModelCalls: 1}},
		},
	}
	observation, err := NewCostObservation(artifact, CostIdentity{
		ModelProvider: artifact.Model.Provider, Model: artifact.Model.Name,
		Source: artifact.Source, PromptSelection: PromptAuthorityGit,
		EvaluationContractDigest: artifact.EvalSet.Digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Cases) != 2 || observation.Cases[1].Sample != 2 {
		t.Fatalf("repeated observation = %+v", observation.Cases)
	}
}

func TestRunnerRequiresKnownRequiredCases(t *testing.T) {
	t.Parallel()
	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), RunnerConfig{
		EvalSet: EvalSet{
			ID: "set", Name: "Set", Digest: strings.Repeat("a", 64),
			Cases: []EvalCase{{
				ID: "known", Conversation: []Invocation{{
					UserContent:   EvalContent{Parts: []EvalPart{{Text: "q"}}},
					FinalResponse: EvalContent{Parts: []EvalPart{{Text: "a"}}},
				}},
			}},
		},
		RunID: "run", Source: testSourceEvidence(), Platform: "test-process", Model: ModelEvidence{Provider: "p", Name: "m"},
		Transport: "rest", RequiredCases: []string{"missing"}, Recorder: recorder,
		ClientFactory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
			return nil, nil, errors.New("must not run")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "required case") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerRejectsNonFiniteMinimumPassRate(t *testing.T) {
	t.Parallel()
	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	for _, minimum := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := Run(context.Background(), RunnerConfig{
			EvalSet: EvalSet{
				ID: "set", Name: "Set", Digest: strings.Repeat("a", 64),
				Cases: []EvalCase{{
					ID: "known", Conversation: []Invocation{{
						UserContent:   EvalContent{Parts: []EvalPart{{Text: "q"}}},
						FinalResponse: EvalContent{Parts: []EvalPart{{Text: "a"}}},
					}},
				}},
			},
			RunID: "run", Source: testSourceEvidence(), Platform: "test-process", Model: ModelEvidence{Provider: "p", Name: "m"},
			Transport: "rest", MinimumPassRate: minimum, Recorder: recorder,
			ClientFactory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
				return nil, nil, errors.New("must not run")
			},
		})
		if err == nil || !strings.Contains(err.Error(), "minimum pass rate") {
			t.Fatalf("minimum %v error = %v", minimum, err)
		}
	}
}

func TestCompareRunsPinsEvalsetAndDetectsDeterministicRegression(t *testing.T) {
	t.Parallel()
	baseline := RunArtifact{
		SchemaVersion: RunArtifactSchemaVersion,
		RunID:         "baseline", Source: testSourceEvidence(), Platform: "test-process", Model: ModelEvidence{Provider: "p", Name: "m"},
		EvalSet: EvalSetEvidence{ID: "set", Digest: "digest"}, Transport: "rest",
		Cases: []CaseResult{{
			ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
			Usage: Usage{TotalTokens: 10, ModelCalls: 1},
		}},
		Summary: RunSummary{Passed: 1, PassRate: 1, MinimumPassRate: 1, RequiredCasesPassed: true},
	}
	candidate := baseline
	candidate.RunID = "candidate"
	candidate.Source.TreeDigest = "sha256:" + strings.Repeat("c", 64)
	candidate.Cases = []CaseResult{{
		ID: "case", Sample: 1, Passed: false, Scores: map[string]float64{"trajectory": 0},
		Usage: Usage{TotalTokens: 12, ModelCalls: 2},
	}}
	candidate.Summary = RunSummary{Failed: 1, PassRate: 0, MinimumPassRate: 1, RequiredCasesPassed: false}
	comparison, err := CompareRuns(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.DeterministicPass || comparison.ScoreDeltas["trajectory"] != -1 || comparison.TotalTokensDelta != 2 {
		t.Fatalf("comparison = %+v", comparison)
	}
	candidate.EvalSet.Digest = "other"
	if _, err := CompareRuns(baseline, candidate); err == nil {
		t.Fatal("different evalset digests compared")
	}
}

func TestComparePromptRunsRequiresDifferentSourceRevisions(t *testing.T) {
	t.Parallel()
	baseline := RunArtifact{
		SchemaVersion: RunArtifactSchemaVersion,
		RunID:         "baseline", Source: testSourceEvidence(), Platform: "test-process", Model: ModelEvidence{Provider: "p", Name: "m"},
		EvalSet: EvalSetEvidence{ID: "set", Digest: "digest"}, Transport: "rest",
		Cases: []CaseResult{{
			ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
			Usage: Usage{TotalTokens: 10, ModelCalls: 1},
		}},
		Summary: RunSummary{Passed: 1, PassRate: 1, MinimumPassRate: 1, RequiredCasesPassed: true},
	}
	candidate := baseline
	candidate.RunID = "candidate"

	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "distinct clean exact revisions") {
		t.Fatalf("same-revision comparison error = %v", err)
	}
	candidate.Source.Identity = strings.Repeat("c", 40)
	candidate.Source.Revision = strings.Repeat("c", 40)
	candidate.Source.TreeDigest = "sha256:" + strings.Repeat("c", 64)
	if _, err := ComparePromptRuns(baseline, candidate); err != nil {
		t.Fatalf("different-revision comparison: %v", err)
	}
	candidate.Platform = "other-platform"
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "platform identity") {
		t.Fatalf("different-platform comparison error = %v", err)
	}
	candidate.Platform = baseline.Platform
	candidate.Model.Name = "other"
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "model identity") {
		t.Fatalf("different-model comparison error = %v", err)
	}
}

type fixedClient struct {
	turn Turn
}

func (client *fixedClient) CreateSession(context.Context) (string, error) { return "session", nil }

func (client *fixedClient) Send(context.Context, string, string) (Turn, error) {
	return client.turn, nil
}

func (client *fixedClient) Confirm(context.Context, string, Turn, bool, string) (Turn, error) {
	return Turn{}, errors.New("not used")
}

func (client *fixedClient) Close() error { return nil }
