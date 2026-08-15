package evals

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWriteJSONArtifactPublishesOneCompleteDocument(t *testing.T) {
	t.Parallel()

	// The directory does not exist yet: artifacts are written to paths a learner
	// chooses on the command line, so the writer creates the tree it is given.
	path := filepath.Join(t.TempDir(), "nested", "artifact.json")
	if err := WriteJSONArtifact(path, map[string]any{"schema_version": 1}); err != nil {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(artifact) error = %v", err)
	}
	if got, want := string(raw), "{\n  \"schema_version\": 1\n}\n"; got != want {
		t.Fatalf("artifact = %q, want indented JSON %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(artifact) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("artifact mode = %v, want 0644 rather than the private temporary mode", got)
	}

	if err := WriteJSONArtifact(path, map[string]any{"schema_version": 2}); err != nil {
		t.Fatalf("WriteJSONArtifact(overwrite) error = %v", err)
	}
	assertOnlyArtifactRemains(t, path, "{\n  \"schema_version\": 2\n}\n")
}

func TestWriteJSONArtifactKeepsThePreviousArtifactWhenEncodingFails(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := WriteJSONArtifact(path, map[string]any{"schema_version": 1}); err != nil {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}
	// An artifact is evidence. A failed write must leave the last good document
	// intact and no half-written temporary beside it, so a reader never mistakes
	// a truncated file for a run that happened.
	if err := WriteJSONArtifact(path, math.Inf(1)); err == nil ||
		!strings.Contains(err.Error(), "encode artifact") {
		t.Fatalf("WriteJSONArtifact(unencodable) error = %v, want an encode failure", err)
	}
	assertOnlyArtifactRemains(t, path, "{\n  \"schema_version\": 1\n}\n")
}

func TestLoadRunArtifactReadsBackExactlyWhatWasPublished(t *testing.T) {
	t.Parallel()

	published, _ := comparisonFixtures()
	published.StartedAt = time.Unix(1_700_000_000, 0).UTC()
	published.CompletedAt = published.StartedAt.Add(5 * time.Second)
	published.Summary.MinimumPassRate = 0.5
	path := filepath.Join(t.TempDir(), "results.json")
	if err := WriteJSONArtifact(path, published); err != nil {
		t.Fatalf("WriteJSONArtifact() error = %v", err)
	}

	loaded, err := LoadRunArtifact(path)
	if err != nil {
		t.Fatalf("LoadRunArtifact() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, published) {
		t.Fatalf("loaded artifact = %+v, want the published artifact %+v", loaded, published)
	}
}

func TestLoadRunArtifactRefusesEvidenceItCannotTrust(t *testing.T) {
	t.Parallel()

	valid, _ := comparisonFixtures()
	valid.Summary.MinimumPassRate = 0.5
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for name, test := range map[string]struct {
		body func(*testing.T) string
		want string
	}{
		"unknown field": {
			body: func(*testing.T) string {
				return `{"reviewer":"nobody",` + strings.TrimPrefix(string(encoded), "{")
			},
			want: "decode evaluation artifact",
		},
		"trailing value": {
			body: func(*testing.T) string { return string(encoded) + " {}" },
			want: "decode evaluation artifact",
		},
		"unknown transport": {
			body: func(t *testing.T) string {
				t.Helper()
				return mutatedArtifactJSON(t, valid, func(a *RunArtifact) { a.Transport = "grpc" })
			},
			want: "incomplete identity",
		},
		"no cases": {
			body: func(t *testing.T) string {
				t.Helper()
				return mutatedArtifactJSON(t, valid, func(a *RunArtifact) { a.Cases = nil })
			},
			want: "invalid cases or threshold",
		},
		"impossible threshold": {
			body: func(t *testing.T) string {
				t.Helper()
				return mutatedArtifactJSON(t, valid, func(a *RunArtifact) { a.Summary.MinimumPassRate = 1.5 })
			},
			want: "invalid cases or threshold",
		},
		"negative usage": {
			body: func(t *testing.T) string {
				t.Helper()
				return mutatedArtifactJSON(t, valid, func(a *RunArtifact) {
					a.Cases = []CaseResult{{
						ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
						Usage: Usage{InputTokens: -1},
					}}
				})
			},
			want: "invalid usage",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(directory, strings.ReplaceAll(name, " ", "-")+".json")
			if writeErr := os.WriteFile(path, []byte(test.body(t)), 0o600); writeErr != nil {
				t.Fatalf("WriteFile(artifact) error = %v", writeErr)
			}
			if _, loadErr := LoadRunArtifact(path); loadErr == nil ||
				!strings.Contains(loadErr.Error(), test.want) {
				t.Fatalf("LoadRunArtifact() error = %v, want one mentioning %q", loadErr, test.want)
			}
		})
	}
	if _, err := LoadRunArtifact(filepath.Join(directory, "absent.json")); err == nil ||
		!strings.Contains(err.Error(), "open evaluation artifact") {
		t.Fatalf("LoadRunArtifact(absent) error = %v, want an open failure", err)
	}
}

func mutatedArtifactJSON(t *testing.T, artifact RunArtifact, mutate func(*RunArtifact)) string {
	t.Helper()
	mutate(&artifact)
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal(artifact) error = %v", err)
	}
	return string(encoded)
}

func assertOnlyArtifactRemains(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(artifact) error = %v", err)
	}
	if string(raw) != want {
		t.Fatalf("artifact = %q, want %q", raw, want)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir(artifact directory) error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("artifact directory holds %v, want only the published artifact", names)
	}
}

func TestCompareRunsRejectsNonFiniteScores(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		baseline, candidate := comparisonFixtures()
		candidate.Cases[0].Scores["trajectory"] = value

		if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "finite") {
			t.Fatalf("CompareRuns(score=%v) error = %v", value, err)
		}
	}
}

func TestCompareRunsRejectsInvalidUsage(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	candidate.Cases[0].Usage = Usage{InputTokens: -1}
	if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("negative usage error = %v", err)
	}

	baseline, candidate = comparisonFixtures()
	baseline.Cases = append(baseline.Cases, CaseResult{
		ID: "case", Sample: 2, Passed: true, Scores: map[string]float64{"trajectory": 1},
	})
	candidate.Cases = append(candidate.Cases, CaseResult{
		ID: "case", Sample: 2, Passed: true, Scores: map[string]float64{"trajectory": 1},
		Usage: Usage{TotalTokens: 1},
	})
	candidate.Cases[0].Usage = Usage{TotalTokens: math.MaxInt64}
	if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("overflowing usage error = %v", err)
	}
}

func TestComparePromptRunsRequiresDistinctCleanExactRevisions(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	candidate.Source = baseline.Source

	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "distinct clean exact revisions") {
		t.Fatalf("same revision error = %v", err)
	}

	candidate.Source = SourceEvidence{Dirty: true}
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "clean exact revisions") {
		t.Fatalf("dirty source error = %v", err)
	}

	candidate.Source = cleanSourceEvidence("c")
	if _, err := ComparePromptRuns(baseline, candidate); err != nil {
		t.Fatalf("two distinct clean exact revisions: %v", err)
	}

	candidate.Model.Name = "other"
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "model identity") {
		t.Fatalf("different-model error = %v", err)
	}

	candidate.Model = baseline.Model
	candidate.Transport = "a2a"
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "same transport") {
		t.Fatalf("different-transport error = %v", err)
	}
}

func TestCompareRunsRejectsIncompleteRunIdentity(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*RunArtifact){
		"schema":    func(artifact *RunArtifact) { artifact.SchemaVersion = 0 },
		"run id":    func(artifact *RunArtifact) { artifact.RunID = "" },
		"model":     func(artifact *RunArtifact) { artifact.Model.Provider = "" },
		"transport": func(artifact *RunArtifact) { artifact.Transport = "grpc" },
		"evalset":   func(artifact *RunArtifact) { artifact.EvalSet.Digest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			baseline, candidate := comparisonFixtures()
			mutate(&candidate)
			if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("incomplete %s error = %v", name, err)
			}
		})
	}
}

func TestCompareRunsRecomputesPassAndScoreSummariesFromCases(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	baseline.Summary = RunSummary{PassRate: 0, RequiredCasesPassed: false}
	candidate.Cases[0].Passed = false
	candidate.Cases[0].Scores["trajectory"] = 0.5
	candidate.Summary = RunSummary{Passed: 1, PassRate: 1, RequiredCasesPassed: true}

	comparison, err := CompareRuns(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.BaselinePassRate != 1 || comparison.CandidatePassRate != 0 || comparison.PassRateDelta != -1 {
		t.Fatalf("pass-rate summary = %v/%v/%v, want 1/0/-1",
			comparison.BaselinePassRate, comparison.CandidatePassRate, comparison.PassRateDelta)
	}
	if comparison.ScoreDeltas["trajectory"] != -0.5 {
		t.Fatalf("trajectory delta = %v, want -0.5", comparison.ScoreDeltas["trajectory"])
	}
	if comparison.DeterministicPass {
		t.Fatal("forged serialized summaries hid case-level regressions")
	}
}

func TestCompareRunsRejectsCasePassFlagWithoutConsistentScores(t *testing.T) {
	t.Parallel()
	t.Run("no cases", func(t *testing.T) {
		baseline, candidate := comparisonFixtures()
		baseline.Cases = nil
		candidate.Cases = nil
		if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "cases") {
			t.Fatalf("CompareRuns() error = %v, want empty-case refusal", err)
		}
	})

	for name, mutate := range map[string]func(*CaseResult, *CaseResult){
		"no deterministic scores": func(baseline, candidate *CaseResult) {
			baseline.Scores = map[string]float64{}
			candidate.Scores = map[string]float64{}
		},
		"passing flag with failed score": func(_, candidate *CaseResult) {
			candidate.Passed = true
			candidate.Scores = map[string]float64{"trajectory": 0}
		},
		"failing flag with passing scores": func(_, candidate *CaseResult) {
			candidate.Passed = false
			candidate.Scores = map[string]float64{"trajectory": 1}
		},
	} {
		t.Run(name, func(t *testing.T) {
			baseline, candidate := comparisonFixtures()
			mutate(&baseline.Cases[0], &candidate.Cases[0])
			if _, err := CompareRuns(baseline, candidate); err == nil ||
				(!strings.Contains(err.Error(), "scores") && !strings.Contains(err.Error(), "pass flag")) {
				t.Fatalf("CompareRuns() error = %v, want deterministic score/pass refusal", err)
			}
		})
	}
}

func TestCompareRunsRequiresIdenticalScoreCoveragePerCaseSample(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	candidate.Cases[0].Scores = map[string]float64{"grounding": 1}

	if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "identical score names") {
		t.Fatalf("score-coverage drift error = %v", err)
	}
}

func TestCompareRunsRejectsDuplicateCaseSamples(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	baseline.Cases = append(baseline.Cases, baseline.Cases[0])
	candidate.Cases = append(candidate.Cases, candidate.Cases[0])

	if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "duplicate case sample") {
		t.Fatalf("duplicate-sample error = %v", err)
	}
}

func TestCompareRunsRejectsInvalidCaseIdentity(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*CaseResult){
		"empty id":    func(result *CaseResult) { result.ID = "" },
		"zero sample": func(result *CaseResult) { result.Sample = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			baseline, candidate := comparisonFixtures()
			mutate(&candidate.Cases[0])

			if _, err := CompareRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "valid case id and positive sample") {
				t.Fatalf("invalid case identity error = %v", err)
			}
		})
	}
}

func TestCompareRunsIgnoresSerializedRequiredCaseOutcome(t *testing.T) {
	t.Parallel()

	baseline, candidate := comparisonFixtures()
	baseline.Summary.RequiredCasesPassed = false
	candidate.Summary.RequiredCasesPassed = false

	comparison, err := CompareRuns(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.DeterministicPass {
		t.Fatal("matching passing case results were overridden by an unverified serialized summary")
	}
}

func comparisonFixtures() (RunArtifact, RunArtifact) {
	baseline := RunArtifact{
		SchemaVersion: RunArtifactSchemaVersion,
		RunID:         "baseline",
		Source:        cleanSourceEvidence("a"),
		Model:         ModelEvidence{Provider: "provider", Name: "model"},
		EvalSet:       EvalSetEvidence{ID: "set", Digest: "digest"},
		Transport:     "rest",
		Cases: []CaseResult{{
			ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
		}},
		Summary: RunSummary{Passed: 1, PassRate: 1, RequiredCasesPassed: true},
	}
	candidate := baseline
	candidate.RunID = "candidate"
	candidate.Source = cleanSourceEvidence("c")
	candidate.Cases = []CaseResult{{
		ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
	}}
	return baseline, candidate
}

func cleanSourceEvidence(revisionByte string) SourceEvidence {
	return SourceEvidence{Revision: strings.Repeat(revisionByte, 40)}
}
