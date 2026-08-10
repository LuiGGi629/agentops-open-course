package evals

import (
	"math"
	"strings"
	"testing"
)

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
	candidate.Source.TreeDigest = "sha256:" + strings.Repeat("c", 64)

	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "distinct clean exact revisions") {
		t.Fatalf("same revision with a different tree digest error = %v", err)
	}

	candidate.Source = SourceEvidence{
		Mode: SourceDevelopment, Identity: "unknown+dirty." + strings.Repeat("d", 12), Dirty: true,
		TreeDigest: "sha256:" + strings.Repeat("d", 64),
	}
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "clean exact revisions") {
		t.Fatalf("dirty source error = %v", err)
	}

	candidate.Source = SourceEvidence{
		Mode: SourceDevelopment, Identity: "unknown", TreeDigest: "sha256:" + strings.Repeat("d", 64),
	}
	if _, err := ComparePromptRuns(baseline, candidate); err == nil || !strings.Contains(err.Error(), "clean exact revisions") {
		t.Fatalf("unknown source error = %v", err)
	}

	candidate.Source = cleanSourceEvidence("c", "d")
	if _, err := ComparePromptRuns(baseline, candidate); err != nil {
		t.Fatalf("two distinct clean exact revisions: %v", err)
	}
}

func TestCompareRunsRejectsIncompleteRunIdentity(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*RunArtifact){
		"schema":    func(artifact *RunArtifact) { artifact.SchemaVersion = 0 },
		"run id":    func(artifact *RunArtifact) { artifact.RunID = "" },
		"model":     func(artifact *RunArtifact) { artifact.Model.Provider = "" },
		"platform":  func(artifact *RunArtifact) { artifact.Platform = "" },
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
		Source:        cleanSourceEvidence("a", "b"),
		Platform:      "test-process",
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
	candidate.Source = cleanSourceEvidence("c", "d")
	candidate.Cases = []CaseResult{{
		ID: "case", Sample: 1, Passed: true, Scores: map[string]float64{"trajectory": 1},
	}}
	return baseline, candidate
}

func cleanSourceEvidence(revisionByte, treeByte string) SourceEvidence {
	revision := strings.Repeat(revisionByte, 40)
	return SourceEvidence{
		Mode: SourceRelease, Identity: revision, Revision: revision,
		TreeDigest: "sha256:" + strings.Repeat(treeByte, 64),
	}
}
