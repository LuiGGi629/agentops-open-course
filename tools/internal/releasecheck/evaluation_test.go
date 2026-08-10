package releasecheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const evaluationSHA = "0123456789abcdef0123456789abcdef01234567"

var fixtureEvaluationCaseIDs = []string{
	"investigation-recalls-context",
	"remediation-loads-skill",
	"restart-needs-approval",
	"resolve-needs-approval",
}

func evaluationFixtures(t *testing.T) EvaluationQualificationInput {
	t.Helper()
	directory := t.TempDir()
	evalsetPath := filepath.Join(directory, "ops.evalset.json")
	policyPath := filepath.Join(directory, "release-policy.json")
	calibrationSetPath := filepath.Join(directory, "judge-calibration.json")
	evalCases := make([]any, 0, len(fixtureEvaluationCaseIDs))
	artifactCases := make([]evaluationCase, 0, len(fixtureEvaluationCaseIDs)*2)
	costCases := make(map[string]evaluationCostCase, len(fixtureEvaluationCaseIDs))
	costSamples := make([]evaluationCostSample, 0, len(fixtureEvaluationCaseIDs)*2)
	for index, id := range fixtureEvaluationCaseIDs {
		evalCases = append(evalCases, map[string]any{
			"eval_id": id, "conversation": []any{map[string]any{"turn": 1}},
			"session_input": map[string]any{"app_name": "agent", "user_id": "learner", "state": map[string]any{}},
		})
		for sample := 1; sample <= 2; sample++ {
			scores := map[string]float64{"trajectory": 1, "judge": 1}
			if index == 0 {
				for _, name := range []string{"safety", "confirmation", "authority", "refusal"} {
					scores[name] = 1
				}
			}
			artifactCases = append(artifactCases, evaluationCase{
				ID: id, Sample: sample, Passed: true, Scores: scores,
				Usage: evaluationUsage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30, ModelCalls: 1},
			})
			costSamples = append(costSamples, evaluationCostSample{
				ID: id, Sample: sample, TotalTokens: 30, ModelCalls: 1,
			})
		}
		costCases[id] = evaluationCostCase{TotalTokens: 30, ModelCalls: 1}
	}
	writeJSONFixture(t, evalsetPath, map[string]any{
		"eval_set_id": "ops", "name": "Operations", "description": "Black-box operations",
		"eval_cases": evalCases,
	})
	minimumPassRate := 0.75
	minimumJudgeAgreement := 0.8
	minimumRepeats := 2
	maximumTotalTokens := int64(240)
	maximumModelCalls := int64(8)
	policyCases := make([]map[string]any, 0, len(fixtureEvaluationCaseIDs))
	for index, id := range fixtureEvaluationCaseIDs {
		controls := []string{}
		category := "quality-reliability"
		mandatory := false
		if index == 0 {
			controls = []string{"approval", "write", "injection", "authority", "pii", "refusal"}
			category = "safety-critical"
			mandatory = true
		}
		policyCases = append(policyCases, map[string]any{
			"id": id, "category": category, "controls": controls, "mandatory": mandatory,
		})
	}
	writeJSONFixture(t, policyPath, map[string]any{
		"schema_version": 1, "policy_version": "test-v1", "status": "approved",
		"learner_minimum_pass_rate": 0.33,
		"release": map[string]any{
			"minimum_pass_rate": minimumPassRate, "minimum_repeats": minimumRepeats,
			"minimum_judge_agreement":      minimumJudgeAgreement,
			"maximum_total_tokens_per_run": maximumTotalTokens,
			"maximum_model_calls_per_run":  maximumModelCalls,
		},
		"cases": policyCases,
	})
	calibrationCases := validCalibrationCases()
	writeJSONFixture(t, calibrationSetPath, sourceCalibrationSet{
		SchemaVersion: 1,
		Cases:         calibrationCases,
	})
	evalDigest, err := canonicalJSONDigest(evalsetPath)
	if err != nil {
		t.Fatal(err)
	}
	policyDigest, err := canonicalJSONDigest(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	calibrationDigest, err := canonicalJSONDigest(calibrationSetPath)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	source := evaluationSource{
		Mode: "release", Identity: evaluationSHA, Revision: evaluationSHA,
		TreeDigest: "sha256:" + strings.Repeat("b", 64),
	}
	evaluationPath := filepath.Join(directory, "eval-results.json")
	writeJSONFixture(t, evaluationPath, evaluationArtifact{
		SchemaVersion: 3, Source: source, Platform: "github-actions-ubuntu-24.04-process", RunID: "eval-run-1",
		Model:   evaluationModel{Provider: "openai-compatible", Name: "qwen3:4b-instruct", Digest: strings.Repeat("a", 64)},
		EvalSet: evaluationEvalSet{ID: "ops", Digest: evalDigest}, Transport: "rest",
		Policy:    &evaluationPolicyEvidence{Version: "test-v1", Digest: policyDigest},
		StartedAt: started, CompletedAt: started.Add(time.Minute),
		Cases: artifactCases,
		Summary: evaluationSummary{
			Passed: len(artifactCases), Failed: 0, PassRate: 1, MinimumPassRate: 0.33, RequiredCasesPassed: true,
		},
	})
	calibrationPath := filepath.Join(directory, "judge-calibration-results.json")
	calibrationResults := make([]calibrationCaseResult, 0, len(calibrationCases))
	for _, calibrationCase := range calibrationCases {
		calibrationResults = append(calibrationResults, calibrationCaseResult{
			ID: calibrationCase.ID, Category: calibrationCase.Category,
			ExpectedPass:  calibrationCase.ExpectedPass,
			PredictedPass: calibrationCase.ExpectedPass, Matched: true,
		})
	}
	writeJSONFixture(t, calibrationPath, calibrationArtifact{
		SchemaVersion: 3, Source: source, Platform: "github-actions-ubuntu-24.04-judge",
		Policy:            &evaluationPolicyEvidence{Version: "test-v1", Digest: policyDigest},
		JudgeModel:        evaluationModel{Provider: "openai-compatible", Name: "qwen3:4b-instruct", Digest: strings.Repeat("a", 64)},
		CalibrationDigest: calibrationDigest, Floor: minimumJudgeAgreement,
		Matches: len(calibrationResults), Total: len(calibrationResults), Agreement: 1,
		Cases: calibrationResults,
	})
	contextLength := int64(8192)
	ollamaVersion := "ollama version is 0.32.6"
	temperature := 0.0
	modelDigest := strings.Repeat("a", 64)
	sourceRevision := evaluationSHA
	costBaselinePath := filepath.Join(directory, "cost_baseline.json")
	writeJSONFixture(t, costBaselinePath, costBaseline{
		SchemaVersion: 2, ModelProvider: "openai-compatible", Model: "qwen3:4b-instruct",
		ModelDigest: &modelDigest, SourceRevision: &sourceRevision, ContextLength: &contextLength,
		OllamaVersion: &ollamaVersion, Temperature: &temperature, PromptSelection: "git",
		EvaluationContractDigest: evalDigest,
		Cases:                    costCases,
	})
	costObservationPath := filepath.Join(directory, "cost-observed.json")
	writeJSONFixture(t, costObservationPath, costObservation{
		SchemaVersion: 3, Source: source, RunID: "eval-run-1",
		Model:   evaluationModel{Provider: "openai-compatible", Name: "qwen3:4b-instruct", Digest: modelDigest},
		EvalSet: evaluationEvalSet{ID: "ops", Digest: evalDigest}, Transport: "rest",
		ContextLength: &contextLength, OllamaVersion: &ollamaVersion, Temperature: &temperature,
		PromptSelection: "git", EvaluationContractDigest: evalDigest,
		Cases: costSamples,
	})
	return EvaluationQualificationInput{
		EvaluationPath: evaluationPath, CalibrationPath: calibrationPath,
		CostObservationPath: costObservationPath, CostBaselinePath: costBaselinePath,
		EvalSetPath: evalsetPath, PolicyPath: policyPath, CalibrationSet: calibrationSetPath,
		Repository: "MLOps-Courses/agentops-open-course-go", SHA: evaluationSHA,
		TreeDigest:    source.TreeDigest,
		WorkflowRunID: 42, WorkflowAttempt: 2,
	}
}

func validCalibrationCases() []sourceCalibrationCase {
	cases := make([]sourceCalibrationCase, 0, 12)
	for _, category := range []string{"good", "bad", "hallucinated"} {
		for index := 1; index <= 4; index++ {
			cases = append(cases, sourceCalibrationCase{
				ID: fmt.Sprintf("%s-%d", category, index), Category: category,
				Question: "What happened?", ReferenceAnswer: "The reviewed answer.",
				Answer: "The candidate answer.", ExpectedPass: category == "good",
			})
		}
	}
	return cases
}

func rewriteCalibrationFixture(
	t *testing.T,
	input EvaluationQualificationInput,
	set sourceCalibrationSet,
) {
	t.Helper()
	writeJSONFixture(t, input.CalibrationSet, set)
	digest, err := canonicalJSONDigest(input.CalibrationSet)
	if err != nil {
		t.Fatal(err)
	}
	var artifact calibrationArtifact
	if err := decodeStrictFile(input.CalibrationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.CalibrationDigest = digest
	artifact.Cases = make([]calibrationCaseResult, 0, len(set.Cases))
	for _, calibrationCase := range set.Cases {
		artifact.Cases = append(artifact.Cases, calibrationCaseResult{
			ID: calibrationCase.ID, Category: calibrationCase.Category,
			ExpectedPass:  calibrationCase.ExpectedPass,
			PredictedPass: calibrationCase.ExpectedPass, Matched: true,
		})
	}
	artifact.Matches = len(artifact.Cases)
	artifact.Total = len(artifact.Cases)
	artifact.Agreement = 1
	writeJSONFixture(t, input.CalibrationPath, artifact)
}

func TestQualifyEvaluationRejectsInvalidCalibrationSet(t *testing.T) {
	for name, mutate := range map[string]func(*sourceCalibrationSet){
		"wrong schema": func(set *sourceCalibrationSet) {
			set.SchemaVersion = 2
		},
		"fewer than twelve cases": func(set *sourceCalibrationSet) {
			set.Cases = append([]sourceCalibrationCase(nil), set.Cases[:3]...)
		},
		"invalid category": func(set *sourceCalibrationSet) {
			set.Cases[0].Category = "almost-good"
		},
		"unbalanced categories": func(set *sourceCalibrationSet) {
			set.Cases[0].Category = "bad"
		},
		"empty source text": func(set *sourceCalibrationSet) {
			set.Cases[0].Question = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := evaluationFixtures(t)
			var set sourceCalibrationSet
			if err := decodeStrictFile(input.CalibrationSet, &set); err != nil {
				t.Fatal(err)
			}
			mutate(&set)
			rewriteCalibrationFixture(t, input, set)
			if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "calibration") {
				t.Fatalf("QualifyEvaluation() error = %v, want invalid calibration-set refusal", err)
			}
		})
	}
}

func TestQualifyEvaluationRequiresExactSourceTreeAcrossEvidence(t *testing.T) {
	input := evaluationFixtures(t)
	input.TreeDigest = "sha256:" + strings.Repeat("c", 64)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "tree") {
		t.Fatalf("tree mismatch error = %v", err)
	}

	input = evaluationFixtures(t)
	var calibration calibrationArtifact
	if err := decodeStrictFile(input.CalibrationPath, &calibration); err != nil {
		t.Fatal(err)
	}
	calibration.Source.TreeDigest = "sha256:" + strings.Repeat("c", 64)
	writeJSONFixture(t, input.CalibrationPath, calibration)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("cross-artifact source mismatch error = %v", err)
	}
}

func TestQualifyEvaluationRequiresPolicyJudgeFloorAndTypedJudgeIdentity(t *testing.T) {
	input := evaluationFixtures(t)
	var calibration calibrationArtifact
	if err := decodeStrictFile(input.CalibrationPath, &calibration); err != nil {
		t.Fatal(err)
	}
	calibration.Floor = 0.5
	writeJSONFixture(t, input.CalibrationPath, calibration)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "floor") {
		t.Fatalf("caller-selected judge floor error = %v", err)
	}

	input = evaluationFixtures(t)
	if err := decodeStrictFile(input.CalibrationPath, &calibration); err != nil {
		t.Fatal(err)
	}
	calibration.JudgeModel.Digest = strings.Repeat("c", 64)
	writeJSONFixture(t, input.CalibrationPath, calibration)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "judge") {
		t.Fatalf("judge identity drift error = %v", err)
	}
}

func TestQualifyEvaluationRequiresJudgeAndControlSpecificScores(t *testing.T) {
	input := evaluationFixtures(t)
	var artifact evaluationArtifact
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	delete(artifact.Cases[0].Scores, "confirmation")
	writeJSONFixture(t, input.EvaluationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("missing control score error = %v", err)
	}

	input = evaluationFixtures(t)
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	delete(artifact.Cases[1].Scores, "judge")
	writeJSONFixture(t, input.EvaluationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "judge") {
		t.Fatalf("missing judge score error = %v", err)
	}
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestQualifyEvaluationReturnsOnlySanitizedLineageAndAggregates(t *testing.T) {
	input := evaluationFixtures(t)
	qualified, err := QualifyEvaluation(input)
	if err != nil {
		t.Fatal(err)
	}
	if qualified.Source.Revision != evaluationSHA || qualified.EvalRun.Summary.PassRate != 1 ||
		qualified.Calibration.Agreement != 1 || qualified.Workflow.RunID != 42 ||
		qualified.Cost.TotalTokens != 240 || qualified.Cost.ModelCalls != 8 ||
		qualified.Policy.Version != "test-v1" || qualified.EvalRun.Summary.RepeatCount != 2 ||
		qualified.Privacy != evaluationPrivacy {
		t.Fatalf("qualified = %#v", qualified)
	}
	content, err := json.Marshal(qualified)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"question":`, `"answer":`, `"tool_args":`, `"rationale":`, `"api_key":`, `"url":`} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("qualified evidence contains %q: %s", forbidden, content)
		}
	}
}

func TestQualifyEvaluationRejectsCostIdentityDrift(t *testing.T) {
	input := evaluationFixtures(t)
	var observation costObservation
	if err := decodeStrictFile(input.CostObservationPath, &observation); err != nil {
		t.Fatal(err)
	}
	observation.PromptSelection = "mutable-registry"
	writeJSONFixture(t, input.CostObservationPath, observation)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsCostRegression(t *testing.T) {
	input := evaluationFixtures(t)
	var observation costObservation
	if err := decodeStrictFile(input.CostObservationPath, &observation); err != nil {
		t.Fatal(err)
	}
	observation.Cases[0].TotalTokens = 38
	writeJSONFixture(t, input.CostObservationPath, observation)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsUnknownRawCostField(t *testing.T) {
	input := evaluationFixtures(t)
	var document map[string]any
	content, err := os.ReadFile(input.CostObservationPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	document["prompt"] = "must not cross the release boundary"
	writeJSONFixture(t, input.CostObservationPath, document)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsUnknownRawEvidenceField(t *testing.T) {
	input := evaluationFixtures(t)
	var document map[string]any
	content, err := os.ReadFile(input.EvaluationPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	document["prompt"] = "must not cross the release boundary"
	writeJSONFixture(t, input.EvaluationPath, document)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsDirtySourceIdentity(t *testing.T) {
	input := evaluationFixtures(t)
	var artifact evaluationArtifact
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Source = evaluationSource{
		Mode: "development", Identity: "unknown+dirty.0123456789ab",
		TreeDigest: "sha256:" + strings.Repeat("c", 64), Dirty: true,
	}
	writeJSONFixture(t, input.EvaluationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("dirty source qualified as a release")
	}
}

func TestQualifyEvaluationUsesPolicyThresholdInsteadOfArtifactThreshold(t *testing.T) {
	input := evaluationFixtures(t)
	var artifact evaluationArtifact
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 3, 4} {
		artifact.Cases[index].Passed = false
		artifact.Cases[index].Scores["trajectory"] = 0
	}
	artifact.Summary = evaluationSummary{
		Passed: 5, Failed: 3, PassRate: 0.625, MinimumPassRate: 0.01, RequiredCasesPassed: true,
	}
	writeJSONFixture(t, input.EvaluationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsFailedCanonicalRequiredCase(t *testing.T) {
	input := evaluationFixtures(t)
	var artifact evaluationArtifact
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Cases[0].Passed = false
	artifact.Cases[0].Scores["trajectory"] = 0
	artifact.Summary = evaluationSummary{
		Passed: len(artifact.Cases) - 1, Failed: 1, PassRate: 0.875,
		MinimumPassRate: 0.01, RequiredCasesPassed: true,
	}
	writeJSONFixture(t, input.EvaluationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}

func TestQualifyEvaluationRejectsCalibrationRequiredPolicy(t *testing.T) {
	input := evaluationFixtures(t)
	var policy evaluationPolicy
	if err := decodeStrictFile(input.PolicyPath, &policy); err != nil {
		t.Fatal(err)
	}
	policy.Status = evaluationPolicyCalibrationRequired
	policy.Release = evaluationReleaseThresholds{}
	writeJSONFixture(t, input.PolicyPath, policy)
	if _, err := QualifyEvaluation(input); err == nil || !strings.Contains(err.Error(), "calibration") {
		t.Fatalf("QualifyEvaluation error = %v", err)
	}
}

func TestQualifyEvaluationRejectsCalibrationLabelDrift(t *testing.T) {
	input := evaluationFixtures(t)
	var artifact calibrationArtifact
	if err := decodeStrictFile(input.CalibrationPath, &artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Cases[0].Category = "bad"
	writeJSONFixture(t, input.CalibrationPath, artifact)
	if _, err := QualifyEvaluation(input); err == nil {
		t.Fatal("QualifyEvaluation error = nil")
	}
}
