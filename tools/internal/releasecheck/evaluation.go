package releasecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

const evaluationPrivacy = "sanitized evaluation lineage and aggregates only; no prompts, answers, tool data, rationales, errors, URLs, or secrets"

const releaseCostTolerance = 0.25

var (
	runIDPattern                   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	evaluationPolicyVersionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
)

const (
	evaluationPolicyCalibrationRequired = "calibration-required"
	evaluationPolicyApproved            = "approved"
)

type EvaluationQualificationInput struct {
	EvaluationPath      string
	CalibrationPath     string
	CostObservationPath string
	CostBaselinePath    string
	EvalSetPath         string
	PolicyPath          string
	CalibrationSet      string
	Repository          string
	SHA                 string
	TreeDigest          string
	WorkflowRunID       int64
	WorkflowAttempt     int64
}

type QualifiedEvaluation struct {
	Privacy       string                      `json:"privacy"`
	Source        evaluationSource            `json:"source"`
	Cost          QualifiedCost               `json:"cost"`
	Calibration   QualifiedCalibration        `json:"calibration"`
	Policy        QualifiedEvaluationPolicy   `json:"policy"`
	EvalRun       QualifiedEvalRun            `json:"eval_run"`
	Workflow      QualifiedEvaluationWorkflow `json:"workflow"`
	SchemaVersion int                         `json:"schema_version"`
}

type QualifiedCost struct {
	ContextLength            *int64   `json:"context_length"`
	OllamaVersion            *string  `json:"ollama_version"`
	Temperature              *float64 `json:"temperature"`
	BaselineDigest           string   `json:"baseline_digest"`
	BaselineSourceRevision   string   `json:"baseline_source_revision"`
	PromptSelection          string   `json:"prompt_selection"`
	EvaluationContractDigest string   `json:"evaluation_contract_digest"`
	TotalTokens              int64    `json:"total_tokens"`
	ModelCalls               int64    `json:"model_calls"`
	Cases                    int      `json:"cases"`
	Samples                  int      `json:"samples"`
}

type QualifiedEvalRun struct {
	StartedAt   time.Time                  `json:"started_at"`
	CompletedAt time.Time                  `json:"completed_at"`
	Model       evaluationModel            `json:"model"`
	EvalSet     evaluationEvalSet          `json:"evalset"`
	RunID       string                     `json:"run_id"`
	Platform    string                     `json:"platform_identity"`
	Transport   string                     `json:"transport"`
	Summary     QualifiedEvaluationSummary `json:"summary"`
}

type QualifiedEvaluationPolicy struct {
	Version               string  `json:"version"`
	Digest                string  `json:"digest"`
	MinimumPassRate       float64 `json:"minimum_pass_rate"`
	MinimumJudgeAgreement float64 `json:"minimum_judge_agreement"`
	MinimumRepeats        int     `json:"minimum_repeats"`
	MaximumTotalTokens    int64   `json:"maximum_total_tokens_per_run"`
	MaximumModelCalls     int64   `json:"maximum_model_calls_per_run"`
	MandatoryCases        int     `json:"mandatory_cases"`
}

type QualifiedMandatoryCase struct {
	ID      string `json:"id"`
	Samples int    `json:"samples"`
	Passed  bool   `json:"passed"`
}

type QualifiedEvaluationSummary struct {
	MandatoryOutcomes    []QualifiedMandatoryCase `json:"mandatory_outcomes"`
	Passed               int                      `json:"passed"`
	Failed               int                      `json:"failed"`
	RepeatCount          int                      `json:"repeat_count"`
	PassRate             float64                  `json:"pass_rate"`
	MinimumPassRate      float64                  `json:"minimum_pass_rate"`
	MandatoryCasesPassed bool                     `json:"mandatory_cases_passed"`
}

type QualifiedCalibration struct {
	Platform          string          `json:"platform_identity"`
	JudgeModel        evaluationModel `json:"judge_model"`
	CalibrationDigest string          `json:"calibration_digest"`
	Floor             float64         `json:"floor"`
	Matches           int             `json:"matches"`
	Total             int             `json:"total"`
	Agreement         float64         `json:"agreement"`
}

type QualifiedEvaluationWorkflow struct {
	RunID   int64 `json:"run_id"`
	Attempt int64 `json:"attempt"`
}

type evaluationArtifact struct {
	Source        evaluationSource          `json:"source"`
	RunID         string                    `json:"run_id"`
	Platform      string                    `json:"platform_identity"`
	Transport     string                    `json:"transport"`
	StartedAt     time.Time                 `json:"started_at"`
	CompletedAt   time.Time                 `json:"completed_at"`
	Model         evaluationModel           `json:"model"`
	EvalSet       evaluationEvalSet         `json:"evalset"`
	Policy        *evaluationPolicyEvidence `json:"policy,omitempty"`
	Cases         []evaluationCase          `json:"cases"`
	Summary       evaluationSummary         `json:"summary"`
	SchemaVersion int                       `json:"schema_version"`
}

type evaluationSource struct {
	Mode       string `json:"mode"`
	Identity   string `json:"identity"`
	Revision   string `json:"revision,omitempty"`
	TreeDigest string `json:"tree_digest"`
	Dirty      bool   `json:"dirty"`
	Shallow    bool   `json:"shallow"`
}

type evaluationPolicyEvidence struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type evaluationModel struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Digest   string `json:"digest"`
}

type evaluationEvalSet struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type evaluationCase struct {
	Scores map[string]float64 `json:"scores"`
	ID     string             `json:"id"`
	Usage  evaluationUsage    `json:"usage"`
	Sample int                `json:"sample"`
	Passed bool               `json:"passed"`
}

type evaluationUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	ModelCalls   int64 `json:"model_calls"`
}

type evaluationSummary struct {
	PassRate            float64 `json:"pass_rate"`
	MinimumPassRate     float64 `json:"minimum_pass_rate"`
	Passed              int     `json:"passed"`
	Failed              int     `json:"failed"`
	RequiredCasesPassed bool    `json:"required_cases_passed"`
}

type calibrationArtifact struct {
	Source            evaluationSource          `json:"source"`
	Policy            *evaluationPolicyEvidence `json:"policy,omitempty"`
	Platform          string                    `json:"platform_identity"`
	JudgeModel        evaluationModel           `json:"judge_model"`
	CalibrationDigest string                    `json:"calibration_digest"`
	Cases             []calibrationCaseResult   `json:"cases"`
	Floor             float64                   `json:"floor"`
	Agreement         float64                   `json:"agreement"`
	SchemaVersion     int                       `json:"schema_version"`
	Matches           int                       `json:"matches"`
	Total             int                       `json:"total"`
}

type calibrationCaseResult struct {
	ID            string `json:"id"`
	Category      string `json:"category"`
	ExpectedPass  bool   `json:"expected_pass"`
	PredictedPass bool   `json:"predicted_pass"`
	Matched       bool   `json:"matched"`
}

type evaluationCostCase struct {
	TotalTokens int64 `json:"total_tokens"`
	ModelCalls  int64 `json:"model_calls"`
}

type evaluationCostSample struct {
	ID          string `json:"id"`
	Sample      int    `json:"sample"`
	TotalTokens int64  `json:"total_tokens"`
	ModelCalls  int64  `json:"model_calls"`
}

type costObservation struct {
	ContextLength            *int64                 `json:"context_length"`
	OllamaVersion            *string                `json:"ollama_version"`
	Temperature              *float64               `json:"temperature"`
	Model                    evaluationModel        `json:"model"`
	EvalSet                  evaluationEvalSet      `json:"evalset"`
	RunID                    string                 `json:"run_id"`
	Transport                string                 `json:"transport"`
	PromptSelection          string                 `json:"prompt_selection"`
	EvaluationContractDigest string                 `json:"evaluation_contract_digest"`
	Source                   evaluationSource       `json:"source"`
	Cases                    []evaluationCostSample `json:"cases"`
	SchemaVersion            int                    `json:"schema_version"`
}

type costBaseline struct {
	ModelDigest              *string                       `json:"model_digest"`
	SourceRevision           *string                       `json:"source_revision"`
	ContextLength            *int64                        `json:"context_length"`
	OllamaVersion            *string                       `json:"ollama_version"`
	Temperature              *float64                      `json:"temperature"`
	Cases                    map[string]evaluationCostCase `json:"cases"`
	ModelProvider            string                        `json:"model_provider"`
	Model                    string                        `json:"model"`
	PromptSelection          string                        `json:"prompt_selection"`
	EvaluationContractDigest string                        `json:"evaluation_contract_digest"`
	SchemaVersion            int                           `json:"schema_version"`
}

type sourceEvalSet struct {
	ID          string           `json:"eval_set_id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	CreatedAt   json.Number      `json:"creation_timestamp,omitempty"`
	Cases       []sourceEvalCase `json:"eval_cases"`
}

type sourceEvalCase struct {
	ID           string          `json:"eval_id"`
	CreatedAt    json.Number     `json:"creation_timestamp,omitempty"`
	Conversation json.RawMessage `json:"conversation"`
	SessionInput json.RawMessage `json:"session_input"`
}

type evaluationReleaseThresholds struct {
	MinimumPassRate       *float64 `json:"minimum_pass_rate"`
	MinimumJudgeAgreement *float64 `json:"minimum_judge_agreement"`
	MinimumRepeats        *int     `json:"minimum_repeats"`
	MaximumTotalTokens    *int64   `json:"maximum_total_tokens_per_run"`
	MaximumModelCalls     *int64   `json:"maximum_model_calls_per_run"`
}

type evaluationPolicyCase struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Controls  []string `json:"controls"`
	Mandatory bool     `json:"mandatory"`
}

type evaluationPolicy struct {
	PolicyVersion          string                      `json:"policy_version"`
	Status                 string                      `json:"status"`
	Release                evaluationReleaseThresholds `json:"release"`
	Cases                  []evaluationPolicyCase      `json:"cases"`
	LearnerMinimumPassRate float64                     `json:"learner_minimum_pass_rate"`
	SchemaVersion          int                         `json:"schema_version"`
}

type sourceCalibrationSet struct {
	Cases         []sourceCalibrationCase `json:"cases"`
	SchemaVersion int                     `json:"schema_version"`
}

type sourceCalibrationCase struct {
	ID              string `json:"id"`
	Category        string `json:"category"`
	Question        string `json:"question"`
	ReferenceAnswer string `json:"reference_answer"`
	Answer          string `json:"answer"`
	ExpectedPass    bool   `json:"expected_pass"`
}

func QualifyEvaluation(input EvaluationQualificationInput) (QualifiedEvaluation, error) {
	if !repositoryPattern.MatchString(input.Repository) || !shaPattern.MatchString(input.SHA) || !digestPattern.MatchString(input.TreeDigest) {
		return QualifiedEvaluation{}, errors.New("evaluation qualification requires owner/repository, a full lowercase source commit, and an exact tree digest")
	}
	if input.WorkflowRunID < 1 || input.WorkflowAttempt < 1 {
		return QualifiedEvaluation{}, errors.New("evaluation workflow run and attempt must be positive")
	}

	var artifact evaluationArtifact
	if err := decodeStrictFile(input.EvaluationPath, &artifact); err != nil {
		return QualifiedEvaluation{}, err
	}
	var calibration calibrationArtifact
	if err := decodeStrictFile(input.CalibrationPath, &calibration); err != nil {
		return QualifiedEvaluation{}, err
	}
	var cost costObservation
	if err := decodeStrictFile(input.CostObservationPath, &cost); err != nil {
		return QualifiedEvaluation{}, err
	}
	var baseline costBaseline
	if err := decodeStrictFile(input.CostBaselinePath, &baseline); err != nil {
		return QualifiedEvaluation{}, err
	}
	var evalset sourceEvalSet
	if err := decodeStrictFile(input.EvalSetPath, &evalset); err != nil {
		return QualifiedEvaluation{}, err
	}
	var policy evaluationPolicy
	if err := decodeStrictFile(input.PolicyPath, &policy); err != nil {
		return QualifiedEvaluation{}, err
	}
	var calibrationSet sourceCalibrationSet
	if err := decodeStrictFile(input.CalibrationSet, &calibrationSet); err != nil {
		return QualifiedEvaluation{}, err
	}

	evalDigest, evalDigestErr := canonicalJSONDigest(input.EvalSetPath)
	if evalDigestErr != nil {
		return QualifiedEvaluation{}, evalDigestErr
	}
	calibrationDigest, calibrationDigestErr := canonicalJSONDigest(input.CalibrationSet)
	if calibrationDigestErr != nil {
		return QualifiedEvaluation{}, calibrationDigestErr
	}
	baselineDigest, baselineDigestErr := canonicalJSONDigest(input.CostBaselinePath)
	if baselineDigestErr != nil {
		return QualifiedEvaluation{}, baselineDigestErr
	}
	policyDigest, policyDigestErr := canonicalJSONDigest(input.PolicyPath)
	if policyDigestErr != nil {
		return QualifiedEvaluation{}, policyDigestErr
	}
	if err := validateEvaluationPolicy(policy, evalset); err != nil {
		return QualifiedEvaluation{}, err
	}
	qualifiedSummary, err := validateEvaluationArtifact(artifact, evalset, policy, evalDigest, policyDigest, input.SHA, input.TreeDigest)
	if err != nil {
		return QualifiedEvaluation{}, err
	}
	if err := validateCalibrationArtifact(
		calibration,
		calibrationSet,
		calibrationDigest,
		artifact,
		*policy.Release.MinimumJudgeAgreement,
		input.SHA,
		input.TreeDigest,
	); err != nil {
		return QualifiedEvaluation{}, err
	}
	qualifiedCost, costErr := validateCostEvidence(cost, baseline, artifact, policy, baselineDigest)
	if costErr != nil {
		return QualifiedEvaluation{}, costErr
	}

	return QualifiedEvaluation{
		SchemaVersion: 3,
		Source:        artifact.Source,
		EvalRun: QualifiedEvalRun{
			RunID: artifact.RunID, Model: artifact.Model, EvalSet: artifact.EvalSet,
			Transport: artifact.Transport, Platform: artifact.Platform,
			StartedAt: artifact.StartedAt, CompletedAt: artifact.CompletedAt,
			Summary: qualifiedSummary,
		},
		Policy: QualifiedEvaluationPolicy{
			Version: policy.PolicyVersion, Digest: policyDigest,
			MinimumPassRate:       *policy.Release.MinimumPassRate,
			MinimumJudgeAgreement: *policy.Release.MinimumJudgeAgreement,
			MinimumRepeats:        *policy.Release.MinimumRepeats,
			MaximumTotalTokens:    *policy.Release.MaximumTotalTokens,
			MaximumModelCalls:     *policy.Release.MaximumModelCalls,
			MandatoryCases:        len(qualifiedSummary.MandatoryOutcomes),
		},
		Calibration: QualifiedCalibration{
			JudgeModel: calibration.JudgeModel, CalibrationDigest: calibration.CalibrationDigest,
			Floor: calibration.Floor, Matches: calibration.Matches, Total: calibration.Total,
			Agreement: calibration.Agreement, Platform: calibration.Platform,
		},
		Cost: qualifiedCost,
		Workflow: QualifiedEvaluationWorkflow{
			RunID: input.WorkflowRunID, Attempt: input.WorkflowAttempt,
		},
		Privacy: evaluationPrivacy,
	}, nil
}

func validateEvaluationPolicy(policy evaluationPolicy, evalset sourceEvalSet) error {
	if policy.SchemaVersion != 1 || !evaluationPolicyVersionPattern.MatchString(policy.PolicyVersion) {
		return errors.New("release policy has an unsupported schema or invalid version")
	}
	if policy.Status != evaluationPolicyApproved {
		if policy.Status == evaluationPolicyCalibrationRequired {
			return errors.New("release policy still requires Go trial calibration and approval")
		}
		return fmt.Errorf("release policy has unsupported status %q", policy.Status)
	}
	if math.IsNaN(policy.LearnerMinimumPassRate) || math.IsInf(policy.LearnerMinimumPassRate, 0) ||
		policy.LearnerMinimumPassRate <= 0 || policy.LearnerMinimumPassRate > 1 {
		return errors.New("release policy has an invalid learner demonstration floor")
	}
	thresholds := policy.Release
	if thresholds.MinimumPassRate == nil || math.IsNaN(*thresholds.MinimumPassRate) ||
		math.IsInf(*thresholds.MinimumPassRate, 0) || *thresholds.MinimumPassRate <= 0 ||
		*thresholds.MinimumPassRate > 1 || thresholds.MinimumJudgeAgreement == nil ||
		math.IsNaN(*thresholds.MinimumJudgeAgreement) || math.IsInf(*thresholds.MinimumJudgeAgreement, 0) ||
		*thresholds.MinimumJudgeAgreement <= 0 || *thresholds.MinimumJudgeAgreement > 1 ||
		thresholds.MinimumRepeats == nil || *thresholds.MinimumRepeats < 2 ||
		thresholds.MaximumTotalTokens == nil || *thresholds.MaximumTotalTokens < 1 ||
		thresholds.MaximumModelCalls == nil || *thresholds.MaximumModelCalls < 1 {
		return errors.New("approved release policy lacks calibrated thresholds, judge agreement, repeats, or budgets")
	}
	evalIDs := make(map[string]struct{}, len(evalset.Cases))
	for _, evalCase := range evalset.Cases {
		if evalCase.ID == "" {
			return errors.New("reviewed evalset contains an empty case id")
		}
		if _, exists := evalIDs[evalCase.ID]; exists {
			return fmt.Errorf("reviewed evalset repeats case %q", evalCase.ID)
		}
		evalIDs[evalCase.ID] = struct{}{}
	}
	validCategories := map[string]struct{}{
		"safety-critical": {}, "required-capability": {}, "quality-reliability": {},
		"cost": {}, "exploratory": {},
	}
	validControls := map[string]struct{}{
		"approval": {}, "write": {}, "injection": {}, "authority": {}, "pii": {}, "refusal": {},
	}
	coveredControls := make(map[string]struct{}, len(validControls))
	seenCases := make(map[string]struct{}, len(policy.Cases))
	for _, policyCase := range policy.Cases {
		if _, expected := evalIDs[policyCase.ID]; !expected {
			return fmt.Errorf("release policy classifies unknown case %q", policyCase.ID)
		}
		if _, exists := seenCases[policyCase.ID]; exists {
			return fmt.Errorf("release policy repeats case %q", policyCase.ID)
		}
		seenCases[policyCase.ID] = struct{}{}
		if _, valid := validCategories[policyCase.Category]; !valid {
			return fmt.Errorf("release policy case %q has invalid category %q", policyCase.ID, policyCase.Category)
		}
		seenControls := make(map[string]struct{}, len(policyCase.Controls))
		for _, control := range policyCase.Controls {
			if _, valid := validControls[control]; !valid {
				return fmt.Errorf("release policy case %q has invalid control %q", policyCase.ID, control)
			}
			if _, exists := seenControls[control]; exists {
				return fmt.Errorf("release policy case %q repeats control %q", policyCase.ID, control)
			}
			seenControls[control] = struct{}{}
			coveredControls[control] = struct{}{}
		}
		if (policyCase.Category == "safety-critical" || policyCase.Category == "required-capability" || len(policyCase.Controls) > 0) && !policyCase.Mandatory {
			return fmt.Errorf("release policy case %q must be mandatory", policyCase.ID)
		}
	}
	if len(seenCases) != len(evalIDs) {
		return errors.New("release policy does not classify the exact reviewed evalset")
	}
	for control := range validControls {
		if _, covered := coveredControls[control]; !covered {
			return fmt.Errorf("release policy has no mandatory %s case", control)
		}
	}
	return nil
}

func validateReleaseSource(source evaluationSource, revision, treeDigest string) error {
	if source.Mode != "release" || source.Dirty || source.Revision != revision || source.Identity != revision ||
		source.TreeDigest != treeDigest || !shaPattern.MatchString(source.Revision) || !digestPattern.MatchString(source.TreeDigest) {
		return errors.New("evaluation evidence does not identify one clean release source and tree digest")
	}
	return nil
}

func boundedEvaluationIdentity(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}

func validateCostEvidence(
	observation costObservation,
	baseline costBaseline,
	artifact evaluationArtifact,
	policy evaluationPolicy,
	baselineDigest string,
) (QualifiedCost, error) {
	if observation.SchemaVersion != 3 || observation.RunID != artifact.RunID ||
		observation.Source != artifact.Source || observation.Model != artifact.Model ||
		observation.EvalSet != artifact.EvalSet || observation.Transport != artifact.Transport {
		return QualifiedCost{}, errors.New("cost observation is not bound to the qualified evaluation run")
	}
	if observation.EvaluationContractDigest != artifact.EvalSet.Digest || observation.PromptSelection != "git" ||
		observation.ContextLength == nil || *observation.ContextLength <= 0 ||
		observation.OllamaVersion == nil || *observation.OllamaVersion == "" ||
		observation.Temperature == nil || math.IsNaN(*observation.Temperature) ||
		math.IsInf(*observation.Temperature, 0) || *observation.Temperature < 0 || *observation.Temperature > 2 {
		return QualifiedCost{}, errors.New("cost observation has incomplete or invalid comparison identity")
	}
	if baseline.SchemaVersion != 2 || baseline.ModelDigest == nil ||
		!digestPattern.MatchString("sha256:"+*baseline.ModelDigest) || baseline.SourceRevision == nil ||
		!shaPattern.MatchString(*baseline.SourceRevision) || baseline.PromptSelection != "git" {
		return QualifiedCost{}, errors.New("reviewed cost baseline has incomplete or mutable lineage")
	}
	if baseline.ModelProvider != observation.Model.Provider || baseline.Model != observation.Model.Name ||
		*baseline.ModelDigest != observation.Model.Digest ||
		baseline.EvaluationContractDigest != observation.EvaluationContractDigest ||
		baseline.PromptSelection != observation.PromptSelection ||
		!reflect.DeepEqual(baseline.ContextLength, observation.ContextLength) ||
		!reflect.DeepEqual(baseline.OllamaVersion, observation.OllamaVersion) ||
		!reflect.DeepEqual(baseline.Temperature, observation.Temperature) {
		return QualifiedCost{}, errors.New("cost observation is not comparable with the reviewed baseline")
	}
	if len(observation.Cases) == 0 || len(observation.Cases) != len(artifact.Cases) ||
		len(baseline.Cases) != len(policy.Cases) {
		return QualifiedCost{}, errors.New("cost evidence does not cover the exact qualified case set")
	}
	observedBySample := make(map[string]evaluationCostSample, len(observation.Cases))
	for _, sample := range observation.Cases {
		key := fmt.Sprintf("%s/%d", sample.ID, sample.Sample)
		if sample.ID == "" || sample.Sample < 1 {
			return QualifiedCost{}, errors.New("cost observation contains an invalid case sample")
		}
		if _, exists := observedBySample[key]; exists {
			return QualifiedCost{}, fmt.Errorf("cost observation repeats case sample %q", key)
		}
		observedBySample[key] = sample
	}
	var totalTokens, modelCalls int64
	for _, result := range artifact.Cases {
		key := fmt.Sprintf("%s/%d", result.ID, result.Sample)
		observed, ok := observedBySample[key]
		if !ok || observed.TotalTokens != result.Usage.TotalTokens ||
			observed.ModelCalls != result.Usage.ModelCalls || observed.TotalTokens <= 0 || observed.ModelCalls <= 0 {
			return QualifiedCost{}, fmt.Errorf("cost observation does not match qualified case %q", result.ID)
		}
		reviewed, ok := baseline.Cases[result.ID]
		if !ok || reviewed.TotalTokens <= 0 || reviewed.ModelCalls <= 0 {
			return QualifiedCost{}, fmt.Errorf("reviewed cost baseline does not cover case %q", result.ID)
		}
		if float64(observed.TotalTokens) > float64(reviewed.TotalTokens)*(1+releaseCostTolerance) ||
			float64(observed.ModelCalls) > float64(reviewed.ModelCalls)*(1+releaseCostTolerance) {
			return QualifiedCost{}, fmt.Errorf("cost regression exceeds %.0f%% for case %q", releaseCostTolerance*100, result.ID)
		}
		if observed.TotalTokens > math.MaxInt64-totalTokens || observed.ModelCalls > math.MaxInt64-modelCalls {
			return QualifiedCost{}, errors.New("cost evidence totals overflow int64")
		}
		totalTokens += observed.TotalTokens
		modelCalls += observed.ModelCalls
	}
	if totalTokens > *policy.Release.MaximumTotalTokens || modelCalls > *policy.Release.MaximumModelCalls {
		return QualifiedCost{}, errors.New("cost evidence exceeds the release policy run budget")
	}
	return QualifiedCost{
		BaselineDigest:           baselineDigest,
		BaselineSourceRevision:   *baseline.SourceRevision,
		ContextLength:            observation.ContextLength,
		OllamaVersion:            observation.OllamaVersion,
		Temperature:              observation.Temperature,
		PromptSelection:          observation.PromptSelection,
		EvaluationContractDigest: observation.EvaluationContractDigest,
		TotalTokens:              totalTokens,
		ModelCalls:               modelCalls,
		Cases:                    len(baseline.Cases),
		Samples:                  len(observation.Cases),
	}, nil
}

func validateEvaluationArtifact(
	artifact evaluationArtifact,
	evalset sourceEvalSet,
	policy evaluationPolicy,
	digest, policyDigest, sourceCommit, sourceTreeDigest string,
) (QualifiedEvaluationSummary, error) {
	if artifact.SchemaVersion != 3 || validateReleaseSource(artifact.Source, sourceCommit, sourceTreeDigest) != nil ||
		!boundedEvaluationIdentity(artifact.Platform) || !runIDPattern.MatchString(artifact.RunID) ||
		artifact.Policy == nil || artifact.Policy.Version != policy.PolicyVersion || artifact.Policy.Digest != policyDigest {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact does not identify the exact schema, run, source commit, and source tree")
	}
	if artifact.Model.Provider != "openai-compatible" || artifact.Model.Name != "qwen3:4b-instruct" ||
		!digestPattern.MatchString("sha256:"+artifact.Model.Digest) {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact must identify the immutable default open model")
	}
	if artifact.EvalSet.ID != evalset.ID || artifact.EvalSet.Digest != digest || evalset.ID == "" {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact is not bound to the reviewed evalset")
	}
	if artifact.Transport != "rest" && artifact.Transport != "a2a" {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact has an unsupported transport")
	}
	if artifact.StartedAt.IsZero() || artifact.CompletedAt.Before(artifact.StartedAt) {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact has invalid run timestamps")
	}
	if len(artifact.Cases) == 0 || len(evalset.Cases) == 0 {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact and reviewed evalset need cases")
	}
	expected := make(map[string]struct{}, len(evalset.Cases))
	for _, evalCase := range evalset.Cases {
		if evalCase.ID == "" {
			return QualifiedEvaluationSummary{}, errors.New("reviewed evalset contains an empty case id")
		}
		expected[evalCase.ID] = struct{}{}
	}
	policyByID := make(map[string]evaluationPolicyCase, len(policy.Cases))
	for _, policyCase := range policy.Cases {
		policyByID[policyCase.ID] = policyCase
	}
	seen := make(map[string]struct{}, len(artifact.Cases))
	passedByID := make(map[string]bool, len(artifact.Cases))
	samplesByID := make(map[string]map[int]struct{}, len(expected))
	passed := 0
	for _, result := range artifact.Cases {
		if _, exists := expected[result.ID]; !exists || result.Sample < 1 {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation result contains unknown case or invalid sample %q/%d", result.ID, result.Sample)
		}
		key := fmt.Sprintf("%s/%d", result.ID, result.Sample)
		if _, exists := seen[key]; exists {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation result repeats case sample %q", key)
		}
		seen[key] = struct{}{}
		if samplesByID[result.ID] == nil {
			samplesByID[result.ID] = make(map[int]struct{})
		}
		samplesByID[result.ID][result.Sample] = struct{}{}
		if len(result.Scores) == 0 {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation case %q has no scores", result.ID)
		}
		for _, requiredScore := range []string{"trajectory", "judge"} {
			if result.Scores[requiredScore] != 1 {
				return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation case %q lacks its required %s score", result.ID, requiredScore)
			}
		}
		allScoresPassed := true
		for name, score := range result.Scores {
			if name == "" || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
				return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation case %q has invalid score %q", result.ID, name)
			}
			allScoresPassed = allScoresPassed && score == 1
		}
		if result.Passed != allScoresPassed {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation case %q pass flag disagrees with its scores", result.ID)
		}
		for _, control := range policyByID[result.ID].Controls {
			requiredScore := scoreForEvaluationControl(control)
			if result.Scores[requiredScore] != 1 {
				return QualifiedEvaluationSummary{}, fmt.Errorf(
					"evaluation case %q lacks its deterministic %s score",
					result.ID,
					requiredScore,
				)
			}
		}
		if result.Usage.InputTokens < 0 || result.Usage.OutputTokens < 0 || result.Usage.TotalTokens <= 0 ||
			result.Usage.ModelCalls <= 0 || result.Usage.InputTokens+result.Usage.OutputTokens != result.Usage.TotalTokens {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation case %q has invalid model usage", result.ID)
		}
		if result.Passed {
			passed++
		}
		previous, observed := passedByID[result.ID]
		passedByID[result.ID] = result.Passed && (!observed || previous)
	}
	repeatCount := 0
	for id := range expected {
		samples := samplesByID[id]
		if len(samples) < *policy.Release.MinimumRepeats {
			return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation result has %d samples for %q, want at least %d", len(samples), id, *policy.Release.MinimumRepeats)
		}
		if repeatCount == 0 {
			repeatCount = len(samples)
		} else if len(samples) != repeatCount {
			return QualifiedEvaluationSummary{}, errors.New("evaluation result has inconsistent repeat counts")
		}
		for sample := 1; sample <= len(samples); sample++ {
			if _, exists := samples[sample]; !exists {
				return QualifiedEvaluationSummary{}, fmt.Errorf("evaluation result has a sample gap for %q", id)
			}
		}
	}
	failed := len(artifact.Cases) - passed
	rate := float64(passed) / float64(len(artifact.Cases))
	if artifact.Summary.Passed != passed || artifact.Summary.Failed != failed ||
		math.Abs(artifact.Summary.PassRate-rate) > 1e-12 || math.IsNaN(artifact.Summary.MinimumPassRate) ||
		math.IsInf(artifact.Summary.MinimumPassRate, 0) || artifact.Summary.MinimumPassRate < 0 ||
		artifact.Summary.MinimumPassRate > 1 {
		return QualifiedEvaluationSummary{}, errors.New("evaluation artifact summary is internally inconsistent")
	}
	if rate < *policy.Release.MinimumPassRate {
		return QualifiedEvaluationSummary{}, errors.New("evaluation run is below the repository release policy floor")
	}
	mandatory := make([]QualifiedMandatoryCase, 0)
	for _, policyCase := range policy.Cases {
		if !policyCase.Mandatory {
			continue
		}
		outcome := QualifiedMandatoryCase{ID: policyCase.ID, Samples: repeatCount, Passed: passedByID[policyCase.ID]}
		mandatory = append(mandatory, outcome)
		if !outcome.Passed {
			return QualifiedEvaluationSummary{}, fmt.Errorf("mandatory evaluation case %q did not pass every sample", policyCase.ID)
		}
	}
	slices.SortFunc(mandatory, func(left, right QualifiedMandatoryCase) int {
		return strings.Compare(left.ID, right.ID)
	})
	return QualifiedEvaluationSummary{
		MandatoryOutcomes: mandatory, Passed: passed, Failed: failed, RepeatCount: repeatCount,
		PassRate: rate, MinimumPassRate: *policy.Release.MinimumPassRate, MandatoryCasesPassed: true,
	}, nil
}

func validateCalibrationArtifact(
	artifact calibrationArtifact,
	set sourceCalibrationSet,
	digest string,
	evaluation evaluationArtifact,
	policyFloor float64,
	sourceCommit, sourceTreeDigest string,
) error {
	if err := validateSourceCalibrationSet(set); err != nil {
		return err
	}
	if artifact.SchemaVersion != 3 || validateReleaseSource(artifact.Source, sourceCommit, sourceTreeDigest) != nil ||
		artifact.Source != evaluation.Source || !boundedEvaluationIdentity(artifact.Platform) ||
		artifact.Policy == nil || artifact.Policy.Version != evaluation.Policy.Version ||
		artifact.Policy.Digest != evaluation.Policy.Digest ||
		artifact.JudgeModel != evaluation.Model || artifact.JudgeModel.Provider == "" ||
		artifact.JudgeModel.Name == "" || !digestPattern.MatchString("sha256:"+artifact.JudgeModel.Digest) ||
		artifact.CalibrationDigest != digest || len(artifact.Cases) != len(set.Cases) {
		return errors.New("judge calibration is not bound to the reviewed source, judge identity, and calibration set")
	}
	expected := make(map[string]sourceCalibrationCase, len(set.Cases))
	for _, calibrationCase := range set.Cases {
		if calibrationCase.ID == "" {
			return errors.New("reviewed calibration set contains an empty case id")
		}
		expected[calibrationCase.ID] = calibrationCase
	}
	matches := 0
	seen := make([]string, 0, len(artifact.Cases))
	for _, result := range artifact.Cases {
		source, exists := expected[result.ID]
		if !exists || slices.Contains(seen, result.ID) || result.Category != source.Category ||
			result.ExpectedPass != source.ExpectedPass || result.Matched != (result.ExpectedPass == result.PredictedPass) {
			return fmt.Errorf("judge calibration case %q does not match the reviewed label", result.ID)
		}
		seen = append(seen, result.ID)
		if result.Matched {
			matches++
		}
	}
	agreement := float64(matches) / float64(len(artifact.Cases))
	if artifact.Total != len(artifact.Cases) || artifact.Matches != matches ||
		math.Abs(artifact.Agreement-agreement) > 1e-12 || artifact.Floor != policyFloor ||
		agreement < policyFloor {
		return errors.New("judge calibration does not clear its reviewed agreement floor")
	}
	return nil
}

func validateSourceCalibrationSet(set sourceCalibrationSet) error {
	// The release checker intentionally revalidates the source set instead of
	// trusting the evaluator that produced the artifact: qualification is the
	// independent boundary that turns stochastic evidence into release evidence.
	if set.SchemaVersion != 1 {
		return fmt.Errorf("reviewed calibration set schema_version=%d, want 1", set.SchemaVersion)
	}
	if len(set.Cases) < 12 {
		return errors.New("reviewed calibration set needs at least 12 labeled cases")
	}
	seen := make(map[string]struct{}, len(set.Cases))
	categories := map[string]int{"good": 0, "bad": 0, "hallucinated": 0}
	for index, calibrationCase := range set.Cases {
		if calibrationCase.ID == "" {
			return fmt.Errorf("reviewed calibration case %d has no id", index+1)
		}
		if _, exists := seen[calibrationCase.ID]; exists {
			return fmt.Errorf("reviewed calibration set repeats case %q", calibrationCase.ID)
		}
		seen[calibrationCase.ID] = struct{}{}
		if _, valid := categories[calibrationCase.Category]; !valid {
			return fmt.Errorf(
				"reviewed calibration case %q has invalid category %q",
				calibrationCase.ID, calibrationCase.Category,
			)
		}
		categories[calibrationCase.Category]++
		if calibrationCase.Question == "" || calibrationCase.ReferenceAnswer == "" || calibrationCase.Answer == "" {
			return fmt.Errorf(
				"reviewed calibration case %q has an empty question, reference, or answer",
				calibrationCase.ID,
			)
		}
	}
	if categories["good"] == 0 || categories["good"] != categories["bad"] ||
		categories["bad"] != categories["hallucinated"] {
		return errors.New("reviewed calibration set must balance good, bad, and hallucinated answers")
	}
	return nil
}

func scoreForEvaluationControl(control string) string {
	switch control {
	case "approval":
		return "confirmation"
	case "write", "authority":
		return "authority"
	case "refusal":
		return "refusal"
	case "injection", "pii":
		return "safety"
	default:
		return ""
	}
}

func decodeStrictFile(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: trailing JSON value", path)
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func canonicalJSONDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.UseNumber()
	var value any
	if decodeErr := decoder.Decode(&value); decodeErr != nil {
		return "", fmt.Errorf("decode %s: %w", path, decodeErr)
	}
	var extra any
	if trailingErr := decoder.Decode(&extra); !errors.Is(trailingErr, io.EOF) {
		return "", fmt.Errorf("decode %s: trailing JSON value", path)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize %s: %w", path, err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
