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
		RunID:   "run", Source: testSourceEvidence(),
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
		RunID: "run", Source: testSourceEvidence(),
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
		RunID: "run", Source: testSourceEvidence(), Model: ModelEvidence{Provider: "p", Name: "m"},
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
			RunID: "run", Source: testSourceEvidence(), Model: ModelEvidence{Provider: "p", Name: "m"},
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
		RunID:         "baseline", Source: testSourceEvidence(), Model: ModelEvidence{Provider: "p", Name: "m"},
		EvalSet: EvalSetEvidence{ID: "set", Digest: "digest"}, Transport: "rest",
		Cases: []CaseResult{{
			ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
			Usage: Usage{TotalTokens: 10, ModelCalls: 1},
		}},
		Summary: RunSummary{Passed: 1, PassRate: 1, MinimumPassRate: 1, RequiredCasesPassed: true},
	}
	candidate := baseline
	candidate.RunID = "candidate"
	candidate.Source.Revision = strings.Repeat("c", 40)
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

func TestRunnerRefusesAnUnusableConfiguration(t *testing.T) {
	t.Parallel()

	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	valid := RunnerConfig{
		EvalSet: singleCaseEvalSet(), RunID: "run", Source: testSourceEvidence(),
		Model: ModelEvidence{Provider: "provider", Name: "model"}, Transport: "rest",
		Recorder: recorder, ClientFactory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
			return nil, nil, errors.New("must not start a client for an invalid configuration")
		},
	}
	for name, test := range map[string]struct {
		mutate func(*RunnerConfig)
		want   string
	}{
		"evalset":        {mutate: func(c *RunnerConfig) { c.EvalSet.Name = "" }, want: "name is required"},
		"digest":         {mutate: func(c *RunnerConfig) { c.EvalSet.Digest = "" }, want: "evalset digest is required"},
		"run id":         {mutate: func(c *RunnerConfig) { c.RunID = "" }, want: "run id, model provider, and model name"},
		"model":          {mutate: func(c *RunnerConfig) { c.Model.Name = "" }, want: "run id, model provider, and model name"},
		"source":         {mutate: func(c *RunnerConfig) { c.Source = SourceEvidence{Revision: "abc"} }, want: "one full lowercase revision"},
		"transport":      {mutate: func(c *RunnerConfig) { c.Transport = "grpc" }, want: `unsupported transport "grpc"`},
		"repeat":         {mutate: func(c *RunnerConfig) { c.Repeat = -1 }, want: "repeat must not be negative"},
		"client factory": {mutate: func(c *RunnerConfig) { c.ClientFactory = nil }, want: "client factory and evidence recorder"},
		"recorder":       {mutate: func(c *RunnerConfig) { c.Recorder = nil }, want: "client factory and evidence recorder"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.mutate(&config)
			if _, err := Run(t.Context(), config); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Run() error = %v, want one mentioning %q", err, test.want)
			}
		})
	}
}

func TestRunnerStopsAtTheFirstUnusableCase(t *testing.T) {
	t.Parallel()

	// Every one of these failures leaves the case ungraded. Continuing would
	// publish an artifact whose pass rate silently excludes the cases that never
	// ran, so the run fails and names the case instead.
	for name, test := range map[string]struct {
		factory ClientFactory
		judge   VerdictJudge
		want    string
	}{
		"client cannot start": {
			factory: func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
				return nil, nil, errors.New("port already bound")
			},
			want: "start isolated agent client",
		},
		"session refused": {
			factory: fixedClientFactory(&failingClient{sessionErr: errors.New("session refused")}),
			want:    "session refused",
		},
		"turn refused": {
			factory: fixedClientFactory(&failingClient{sendErr: errors.New("turn refused")}),
			want:    "turn refused",
		},
		"judge unavailable": {
			factory: fixedClientFactory(&fixedClient{turn: Turn{Text: "answer"}}),
			judge:   errorJudge{err: errors.New("gateway unavailable")},
			want:    "judge:",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorder, err := NewNoopRecorder()
			if err != nil {
				t.Fatal(err)
			}
			_, err = Run(t.Context(), RunnerConfig{
				EvalSet: singleCaseEvalSet(), RunID: "run", Source: testSourceEvidence(),
				Model: ModelEvidence{Provider: "provider", Name: "model"}, Transport: "rest",
				Recorder: recorder, ClientFactory: test.factory, Judge: test.judge,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Run() error = %v, want one mentioning %q", err, test.want)
			}
			if !strings.Contains(err.Error(), "case-one") {
				t.Fatalf("Run() error = %v, want it to name the case that failed", err)
			}
		})
	}
}

func TestRunnerRequiresEverySampleOfARequiredCaseToPass(t *testing.T) {
	t.Parallel()

	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	evalset := singleCaseEvalSet()
	evalset.Cases[0].Conversation[0].IntermediateData.ToolUses = []ExpectedToolCall{{
		Name: "get_incident", Args: map[string]any{"incident_id": "INC-002"},
	}}
	artifact, err := Run(t.Context(), RunnerConfig{
		EvalSet: evalset, RunID: "run", Source: testSourceEvidence(),
		Model: ModelEvidence{Provider: "provider", Name: "model"}, Transport: "rest",
		Repeat: 2, MinimumPassRate: 0.5, RequiredCases: []string{"case-one"}, Recorder: recorder,
		ClientFactory: func(_ context.Context, _ EvalCase, sample int) (AgentClient, func() error, error) {
			// A flaky agent: the first sample skips the required tool call, the
			// second makes it. Averaged over samples this still clears the floor.
			turn := Turn{Text: "answer"}
			if sample == 2 {
				turn.ToolCalls = []ToolCall{{Name: "get_incident", Args: map[string]any{"incident_id": "INC-002"}}}
			}
			return &fixedClient{turn: turn}, func() error { return nil }, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if artifact.Summary.PassRate != 0.5 || artifact.Summary.Passed != 1 || artifact.Summary.Failed != 1 {
		t.Fatalf("summary = %+v, want one passing and one failing sample", artifact.Summary)
	}
	// A safety case that only works half the time is not a safety case, so one
	// failing sample disqualifies it however well the average reads.
	if artifact.Summary.RequiredCasesPassed {
		t.Fatal("a required case passed while one of its samples failed")
	}
	if artifact.Passed() {
		t.Fatalf("run passed on a pass rate of %v despite a failed required case", artifact.Summary.PassRate)
	}
}

func singleCaseEvalSet() EvalSet {
	return EvalSet{
		ID: "set", Name: "Set", Digest: strings.Repeat("a", 64),
		Cases: []EvalCase{{
			ID: "case-one", Conversation: []Invocation{{
				UserContent:   EvalContent{Parts: []EvalPart{{Text: "question"}}},
				FinalResponse: EvalContent{Parts: []EvalPart{{Text: "reference"}}},
			}},
		}},
	}
}

func fixedClientFactory(client AgentClient) ClientFactory {
	return func(context.Context, EvalCase, int) (AgentClient, func() error, error) {
		return client, func() error { return nil }, nil
	}
}

type failingClient struct {
	sessionErr error
	sendErr    error
}

func (client *failingClient) CreateSession(context.Context) (string, error) {
	return "session", client.sessionErr
}

func (client *failingClient) Send(context.Context, string, string) (Turn, error) {
	return Turn{}, client.sendErr
}

func (client *failingClient) Confirm(context.Context, string, Turn, bool, string) (Turn, error) {
	return Turn{}, errors.New("not used")
}

func (client *failingClient) Close() error { return nil }

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

func TestRunnerLetsDeterministicScoresDecideARequiredCase(t *testing.T) {
	t.Parallel()

	recorder, err := NewNoopRecorder()
	if err != nil {
		t.Fatal(err)
	}
	evalset := singleCaseEvalSet()
	evalset.Cases[0].Conversation[0].IntermediateData.ToolUses = []ExpectedToolCall{{
		Name: "get_incident", Args: map[string]any{"incident_id": "INC-002"},
	}}
	artifact, err := Run(t.Context(), RunnerConfig{
		EvalSet: evalset, RunID: "run", Source: testSourceEvidence(),
		Model: ModelEvidence{Provider: "provider", Name: "model"}, Transport: "rest",
		Repeat: 1, MinimumPassRate: 0.5, RequiredCases: []string{"case-one"}, Recorder: recorder,
		// The judge rejects everything; every deterministic score passes.
		Judge: fixedJudge{pass: false},
		ClientFactory: func(_ context.Context, _ EvalCase, _ int) (AgentClient, func() error, error) {
			turn := Turn{Text: "answer", ToolCalls: []ToolCall{
				{Name: "get_incident", Args: map[string]any{"incident_id": "INC-002"}},
			}}
			return &fixedClient{turn: turn}, func() error { return nil }, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// The judged failure still costs the run its rate — that is the honest half.
	if artifact.Summary.PassRate >= 1 {
		t.Fatalf("summary = %+v, want a judged failure to reduce the pass rate", artifact.Summary)
	}
	// It must not, on its own, declare a safety case failed.
	if !artifact.Summary.RequiredCasesPassed {
		t.Fatal("one stochastic verdict vetoed a required case whose deterministic scores all passed")
	}
	if artifact.Cases[0].Scores["judge"] != 0 {
		t.Fatalf("scores = %v, want the judged verdict recorded rather than dropped", artifact.Cases[0].Scores)
	}
}
