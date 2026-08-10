package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLoadRetrievalCasesDerivesQueriesFromImmutableSeed(t *testing.T) {
	t.Parallel()

	cases, err := LoadRetrievalCases(filepath.Join("..", "agents", "data"))
	if err != nil {
		t.Fatalf("LoadRetrievalCases() error = %v", err)
	}
	if len(cases) != 10 {
		t.Fatalf("len(cases) = %d, want 10", len(cases))
	}
	if got, want := cases[0], (RetrievalCase{
		ID:              "INC-001",
		Query:           "Checkout latency spike. p99 checkout latency rose from 200ms to 3.5s right after the 08:00 deploy.",
		ExpectedRunbook: "high-latency",
	}); got != want {
		t.Fatalf("cases[0] = %#v, want %#v", got, want)
	}
	if got, want := cases[len(cases)-1].ID, "INC-010"; got != want {
		t.Fatalf("last case id = %q, want %q", got, want)
	}
}

func TestRunRetrievalEvaluationUsesDistinctModesAndWritesOnlyAggregateEvidence(t *testing.T) {
	t.Parallel()

	dataDir, cases := writeRetrievalFixture(t)
	answers := map[RetrievalMode]map[string][]string{
		RetrievalKeyword: {
			cases[0].Query: {"latency", "other-one", "other-two"},
			cases[1].Query: {"other-one", "down", "other-two"},
			cases[2].Query: {"other-one", "other-two", "other-three"},
			cases[3].Query: {"other-one", "other-two", "other-three"},
		},
		RetrievalSemantic: {
			cases[0].Query: {"latency", "other-one", "other-two"},
			cases[1].Query: {"down", "other-one", "other-two"},
			cases[2].Query: {"other-one", "errors", "other-two"},
			cases[3].Query: {"other-one", "other-two", "other-three"},
		},
	}
	factory := &fakeRetrievalFactory{answers: answers}

	artifact, err := RunRetrievalEvaluation(t.Context(), RetrievalRunConfig{
		DataDir: dataDir, Source: testSourceEvidence(), Platform: "test-retrieval", EmbeddingModel: "embed-fixture",
		EmbeddingModelDigest: "sha256:model-fixture",
		Factory:              factory.New,
	})
	if err != nil {
		t.Fatalf("RunRetrievalEvaluation() error = %v", err)
	}
	if got, want := factory.modes, []RetrievalMode{RetrievalKeyword, RetrievalSemantic}; !slices.Equal(got, want) {
		t.Fatalf("factory modes = %v, want %v", got, want)
	}
	if got, want := factory.closed, []RetrievalMode{RetrievalKeyword, RetrievalSemantic}; !slices.Equal(got, want) {
		t.Fatalf("closed modes = %v, want %v", got, want)
	}
	for mode, client := range factory.clients {
		if got, want := client.calls, len(cases); got != want {
			t.Errorf("%s calls = %d, want %d", mode, got, want)
		}
	}
	if artifact.SchemaVersion != RetrievalArtifactSchemaVersion || artifact.CaseCount != 4 {
		t.Fatalf("artifact identity = %#v", artifact)
	}
	if len(artifact.CorpusDigest) != 64 {
		t.Errorf("corpus digest length = %d, want 64", len(artifact.CorpusDigest))
	}
	if artifact.EmbeddingModelDigest != "sha256:model-fixture" {
		t.Errorf("embedding model digest = %q", artifact.EmbeddingModelDigest)
	}
	if got, want := artifact.Keyword, (RetrievalRates{HitRateAt1: 0.25, HitRateAt3: 0.5}); got != want {
		t.Errorf("keyword rates = %#v, want %#v", got, want)
	}
	if got, want := artifact.Semantic, (RetrievalRates{HitRateAt1: 0.5, HitRateAt3: 0.75}); got != want {
		t.Errorf("semantic rates = %#v, want %#v", got, want)
	}

	rendered, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, secret := range []string{
		cases[0].Query,
		cases[0].ExpectedRunbook,
		"http://127.0.0.1",
		"tool payload",
	} {
		if strings.Contains(string(rendered), secret) {
			t.Errorf("artifact contains retrieval content %q: %s", secret, rendered)
		}
	}
}

func TestRetrievalCorpusDigestBindsRunbookContents(t *testing.T) {
	t.Parallel()

	dataDir, cases := writeRetrievalFixture(t)
	before, err := retrievalCorpusDigest(dataDir, cases)
	if err != nil {
		t.Fatalf("retrievalCorpusDigest(before) error = %v", err)
	}
	path := filepath.Join(dataDir, "runbooks", "latency.md")
	if writeErr := os.WriteFile(path, []byte("# latency\n\nChanged body only.\n"), 0o600); writeErr != nil {
		t.Fatalf("WriteFile(runbook) error = %v", writeErr)
	}
	after, err := retrievalCorpusDigest(dataDir, cases)
	if err != nil {
		t.Fatalf("retrievalCorpusDigest(after) error = %v", err)
	}
	if before == after {
		t.Fatalf("digest stayed %q after a runbook-content-only change", before)
	}
}

func TestLoadRetrievalCasesRejectsAnUnparsedIncidentRow(t *testing.T) {
	t.Parallel()

	dataDir, _ := writeRetrievalFixture(t)
	path := filepath.Join(dataDir, "sql", "seed.sql")
	seed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(seed) error = %v", err)
	}
	malformed := strings.Replace(string(seed), "'Disk summary.'", "NULL", 1)
	if writeErr := os.WriteFile(path, []byte(malformed), 0o600); writeErr != nil {
		t.Fatalf("WriteFile(seed) error = %v", writeErr)
	}
	if _, err := LoadRetrievalCases(dataDir); err == nil || !strings.Contains(err.Error(), "parsed 3 of 4") {
		t.Fatalf("LoadRetrievalCases() error = %v, want incomplete parse", err)
	}
}

func TestRunRetrievalEvaluationRejectsSemanticFallback(t *testing.T) {
	t.Parallel()

	dataDir, cases := writeRetrievalFixture(t)
	answers := answersForEveryCase(cases)
	factory := &fakeRetrievalFactory{
		answers:      answers,
		observedMode: map[RetrievalMode]RetrievalMode{RetrievalSemantic: RetrievalKeyword},
	}

	_, err := RunRetrievalEvaluation(t.Context(), RetrievalRunConfig{
		DataDir: dataDir, Source: testSourceEvidence(), Platform: "test-retrieval", EmbeddingModel: "embed", Factory: factory.New,
	})
	if err == nil || !strings.Contains(err.Error(), "semantic runtime reported keyword retrieval") {
		t.Fatalf("RunRetrievalEvaluation() error = %v, want semantic fallback rejection", err)
	}
	if got, want := factory.closed, []RetrievalMode{RetrievalKeyword, RetrievalSemantic}; !slices.Equal(got, want) {
		t.Fatalf("closed modes = %v, want %v", got, want)
	}
}

func TestRunRetrievalEvaluationRejectsReusedRuntime(t *testing.T) {
	t.Parallel()

	dataDir, cases := writeRetrievalFixture(t)
	factory := &fakeRetrievalFactory{answers: answersForEveryCase(cases), sharedID: "same-runtime"}

	_, err := RunRetrievalEvaluation(t.Context(), RetrievalRunConfig{
		DataDir: dataDir, Source: testSourceEvidence(), Platform: "test-retrieval", EmbeddingModel: "embed", Factory: factory.New,
	})
	if err == nil || !strings.Contains(err.Error(), "must use a distinct isolated runtime") {
		t.Fatalf("RunRetrievalEvaluation() error = %v, want runtime isolation rejection", err)
	}
	if got, want := factory.closed, []RetrievalMode{RetrievalKeyword, RetrievalSemantic}; !slices.Equal(got, want) {
		t.Fatalf("closed modes = %v, want %v", got, want)
	}
}

func TestRunRetrievalEvaluationJoinsCleanupFailure(t *testing.T) {
	t.Parallel()

	dataDir, cases := writeRetrievalFixture(t)
	factory := &fakeRetrievalFactory{
		answers: answersForEveryCase(cases),
		closeErrors: map[RetrievalMode]error{
			RetrievalKeyword: errors.New("cleanup failed"),
		},
	}

	_, err := RunRetrievalEvaluation(t.Context(), RetrievalRunConfig{
		DataDir: dataDir, Source: testSourceEvidence(), Platform: "test-retrieval", EmbeddingModel: "embed", Factory: factory.New,
	})
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("RunRetrievalEvaluation() error = %v, want cleanup failure", err)
	}
	if len(factory.modes) != 1 {
		t.Fatalf("factory modes = %v, want evaluation to stop after invalid cleanup", factory.modes)
	}
}

func TestValidateRetrievalArtifactRejectsIncompleteIdentityAndInvalidRates(t *testing.T) {
	t.Parallel()

	valid := RetrievalArtifact{
		Source: testSourceEvidence(), Platform: "test-retrieval", CorpusDigest: strings.Repeat("a", 64), EmbeddingModel: "embed",
		EmbeddingModelDigest: "sha256:model",
		Keyword:              RetrievalRates{HitRateAt1: 0.5, HitRateAt3: 1},
		Semantic:             RetrievalRates{HitRateAt1: 0.75, HitRateAt3: 1},
		SchemaVersion:        RetrievalArtifactSchemaVersion, CaseCount: 4,
	}
	if err := ValidateRetrievalArtifact(valid); err != nil {
		t.Fatalf("ValidateRetrievalArtifact(valid) error = %v", err)
	}

	tests := map[string]RetrievalArtifact{
		"source newline": func() RetrievalArtifact { value := valid; value.Source.Identity = "commit\nother"; return value }(),
		"digest":         func() RetrievalArtifact { value := valid; value.CorpusDigest = "short"; return value }(),
		"embedding":      func() RetrievalArtifact { value := valid; value.EmbeddingModel = ""; return value }(),
		"model digest":   func() RetrievalArtifact { value := valid; value.EmbeddingModelDigest = "bad\ndigest"; return value }(),
		"case count":     func() RetrievalArtifact { value := valid; value.CaseCount = 0; return value }(),
		"rate range":     func() RetrievalArtifact { value := valid; value.Semantic.HitRateAt3 = 1.1; return value }(),
		"monotonic": func() RetrievalArtifact {
			value := valid
			value.Keyword.HitRateAt1 = 0.75
			value.Keyword.HitRateAt3 = 0.5
			return value
		}(),
	}
	for name, artifact := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateRetrievalArtifact(artifact); err == nil {
				t.Fatal("ValidateRetrievalArtifact() error = nil")
			}
		})
	}
}

type fakeRetrievalClient struct {
	answers      map[string][]string
	observedMode RetrievalMode
	calls        int
}

func (c *fakeRetrievalClient) SearchRunbooks(_ context.Context, query string, limit int) (RetrievalSearchResult, error) {
	c.calls++
	if limit != 3 {
		return RetrievalSearchResult{}, fmt.Errorf("limit = %d, want 3", limit)
	}
	return RetrievalSearchResult{Mode: c.observedMode, Slugs: slices.Clone(c.answers[query])}, nil
}

type fakeRetrievalFactory struct {
	answers      map[RetrievalMode]map[string][]string
	observedMode map[RetrievalMode]RetrievalMode
	closeErrors  map[RetrievalMode]error
	clients      map[RetrievalMode]*fakeRetrievalClient
	sharedID     string
	modes        []RetrievalMode
	closed       []RetrievalMode
}

func (f *fakeRetrievalFactory) New(_ context.Context, mode RetrievalMode) (RetrievalRuntime, error) {
	f.modes = append(f.modes, mode)
	observed := mode
	if override := f.observedMode[mode]; override != "" {
		observed = override
	}
	client := &fakeRetrievalClient{answers: f.answers[mode], observedMode: observed}
	if f.clients == nil {
		f.clients = make(map[RetrievalMode]*fakeRetrievalClient)
	}
	f.clients[mode] = client
	id := string(mode) + "-runtime"
	if f.sharedID != "" {
		id = f.sharedID
	}
	return RetrievalRuntime{
		ID: id, Client: client,
		Close: func() error {
			f.closed = append(f.closed, mode)
			return f.closeErrors[mode]
		},
	}, nil
}

func answersForEveryCase(cases []RetrievalCase) map[RetrievalMode]map[string][]string {
	answers := map[RetrievalMode]map[string][]string{
		RetrievalKeyword:  {},
		RetrievalSemantic: {},
	}
	for _, evalCase := range cases {
		answers[RetrievalKeyword][evalCase.Query] = []string{evalCase.ExpectedRunbook}
		answers[RetrievalSemantic][evalCase.Query] = []string{evalCase.ExpectedRunbook}
	}
	return answers
}

func writeRetrievalFixture(t *testing.T) (string, []RetrievalCase) {
	t.Helper()
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "sql"), 0o755); err != nil {
		t.Fatalf("MkdirAll(sql) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "runbooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll(runbooks) error = %v", err)
	}
	seed := `INSERT INTO incidents (id, service, title, severity, status, runbook, opened_at, resolved_at, summary) VALUES
    ('INC-001', 'checkout', 'Latency title', 'SEV2', 'open', 'latency', '2026-01-01T00:00:00Z', NULL, 'Latency summary.'),
    ('INC-002', 'inventory', 'Down title', 'SEV1', 'open', 'down', '2026-01-01T00:00:00Z', NULL, 'Down summary.'),
    ('INC-003', 'payments', 'Errors title', 'SEV3', 'open', 'errors', '2026-01-01T00:00:00Z', NULL, 'Errors summary.'),
    ('INC-004', 'checkout', 'Disk title', 'SEV2', 'open', 'disk', '2026-01-01T00:00:00Z', NULL, 'Disk summary.');
`
	if err := os.WriteFile(filepath.Join(directory, "sql", "seed.sql"), []byte(seed), 0o600); err != nil {
		t.Fatalf("WriteFile(seed) error = %v", err)
	}
	for _, slug := range []string{"latency", "down", "errors", "disk"} {
		if err := os.WriteFile(filepath.Join(directory, "runbooks", slug+".md"), []byte("# "+slug+"\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(runbook) error = %v", err)
		}
	}
	cases, err := LoadRetrievalCases(directory)
	if err != nil {
		t.Fatalf("LoadRetrievalCases() error = %v", err)
	}
	return directory, cases
}
