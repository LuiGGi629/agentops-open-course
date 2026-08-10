package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
)

type PolicyStatus string

const (
	PolicyCalibrationRequired PolicyStatus = "calibration-required"
	PolicyApproved            PolicyStatus = "approved"
)

type CaseCategory string

const (
	CategorySafetyCritical     CaseCategory = "safety-critical"
	CategoryRequiredCapability CaseCategory = "required-capability"
	CategoryQualityReliability CaseCategory = "quality-reliability"
	CategoryCost               CaseCategory = "cost"
	CategoryExploratory        CaseCategory = "exploratory"
)

type SafetyControl string

const (
	ControlApproval  SafetyControl = "approval"
	ControlWrite     SafetyControl = "write"
	ControlInjection SafetyControl = "injection"
	ControlAuthority SafetyControl = "authority"
	ControlPII       SafetyControl = "pii"
	ControlRefusal   SafetyControl = "refusal"
)

var policyVersionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

type ReleaseThresholds struct {
	MinimumPassRate       *float64 `json:"minimum_pass_rate"`
	MinimumJudgeAgreement *float64 `json:"minimum_judge_agreement"`
	MinimumRepeats        *int     `json:"minimum_repeats"`
	MaximumTotalTokens    *int64   `json:"maximum_total_tokens_per_run"`
	MaximumModelCalls     *int64   `json:"maximum_model_calls_per_run"`
}

type ReleasePolicyCase struct {
	ID        string          `json:"id"`
	Category  CaseCategory    `json:"category"`
	Controls  []SafetyControl `json:"controls"`
	Mandatory bool            `json:"mandatory"`
}

// ReleasePolicy keeps learner demonstration thresholds separate from the
// owner-reviewed values which may qualify a release.
type ReleasePolicy struct {
	Release                ReleaseThresholds   `json:"release"`
	PolicyVersion          string              `json:"policy_version"`
	Status                 PolicyStatus        `json:"status"`
	Digest                 string              `json:"-"`
	Cases                  []ReleasePolicyCase `json:"cases"`
	LearnerMinimumPassRate float64             `json:"learner_minimum_pass_rate"`
	SchemaVersion          int                 `json:"schema_version"`
}

func LoadReleasePolicy(path string) (ReleasePolicy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReleasePolicy{}, fmt.Errorf("read release policy %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	var policy ReleasePolicy
	if decodeErr := decoder.Decode(&policy); decodeErr != nil {
		return ReleasePolicy{}, fmt.Errorf("decode release policy %s: %w", path, decodeErr)
	}
	if eofErr := requireJSONEOF(decoder); eofErr != nil {
		return ReleasePolicy{}, fmt.Errorf("decode release policy %s: %w", path, eofErr)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return ReleasePolicy{}, fmt.Errorf("canonicalize release policy %s: %w", path, err)
	}
	digest := sha256.Sum256(canonical)
	policy.Digest = hex.EncodeToString(digest[:])
	if err := policy.Validate(); err != nil {
		return ReleasePolicy{}, fmt.Errorf("validate release policy %s: %w", path, err)
	}
	return policy, nil
}

func (p ReleasePolicy) Validate() error {
	var problems []error
	if p.SchemaVersion != 1 {
		problems = append(problems, fmt.Errorf("schema_version=%d, want 1", p.SchemaVersion))
	}
	if !policyVersionPattern.MatchString(p.PolicyVersion) {
		problems = append(problems, fmt.Errorf("invalid policy_version %q", p.PolicyVersion))
	}
	if p.Status != PolicyCalibrationRequired && p.Status != PolicyApproved {
		problems = append(problems, fmt.Errorf("invalid status %q", p.Status))
	}
	if !finiteUnitInterval(p.LearnerMinimumPassRate) || p.LearnerMinimumPassRate == 0 {
		problems = append(problems, errors.New("learner_minimum_pass_rate must be finite and greater than 0 through 1"))
	}
	if len(p.Cases) == 0 {
		problems = append(problems, errors.New("cases must not be empty"))
	}
	seenCases := make(map[string]struct{}, len(p.Cases))
	coveredControls := make(map[SafetyControl]struct{})
	for index, policyCase := range p.Cases {
		if !evalIDPattern.MatchString(policyCase.ID) {
			problems = append(problems, fmt.Errorf("case %d has invalid id %q", index+1, policyCase.ID))
		}
		if _, exists := seenCases[policyCase.ID]; exists {
			problems = append(problems, fmt.Errorf("duplicate policy case %q", policyCase.ID))
		}
		seenCases[policyCase.ID] = struct{}{}
		if !validCaseCategory(policyCase.Category) {
			problems = append(problems, fmt.Errorf("case %q has invalid category %q", policyCase.ID, policyCase.Category))
		}
		seenControls := make(map[SafetyControl]struct{}, len(policyCase.Controls))
		for _, control := range policyCase.Controls {
			if !validSafetyControl(control) {
				problems = append(problems, fmt.Errorf("case %q has invalid control %q", policyCase.ID, control))
			}
			if _, exists := seenControls[control]; exists {
				problems = append(problems, fmt.Errorf("case %q repeats control %q", policyCase.ID, control))
			}
			seenControls[control] = struct{}{}
			coveredControls[control] = struct{}{}
		}
		if (policyCase.Category == CategorySafetyCritical || policyCase.Category == CategoryRequiredCapability || len(policyCase.Controls) > 0) && !policyCase.Mandatory {
			problems = append(problems, fmt.Errorf("case %q must be mandatory", policyCase.ID))
		}
	}
	problems = append(problems, p.Release.validate(p.Status)...)
	if p.Status == PolicyApproved {
		for _, control := range allSafetyControls() {
			if _, covered := coveredControls[control]; !covered {
				problems = append(problems, fmt.Errorf("approved policy has no mandatory %s case", control))
			}
		}
	}
	return errors.Join(problems...)
}

func (p ReleasePolicy) ValidateEvalSet(evalset EvalSet) error {
	if err := p.Validate(); err != nil {
		return err
	}
	policyIDs := make([]string, 0, len(p.Cases))
	for _, policyCase := range p.Cases {
		policyIDs = append(policyIDs, policyCase.ID)
	}
	evalIDs := make([]string, 0, len(evalset.Cases))
	for _, evalCase := range evalset.Cases {
		evalIDs = append(evalIDs, evalCase.ID)
	}
	slices.Sort(policyIDs)
	slices.Sort(evalIDs)
	if !slices.Equal(policyIDs, evalIDs) {
		return fmt.Errorf("release policy cases %v do not match core evalset %v", policyIDs, evalIDs)
	}
	evalCases := make(map[string]EvalCase, len(evalset.Cases))
	for _, evalCase := range evalset.Cases {
		evalCases[evalCase.ID] = evalCase
	}
	var problems []error
	for _, policyCase := range p.Cases {
		for _, control := range policyCase.Controls {
			score := scoreForControl(control)
			if !caseDeclaresScore(evalCases[policyCase.ID], score) {
				problems = append(problems, fmt.Errorf(
					"case %q control %q has no deterministic %s evidence",
					policyCase.ID,
					control,
					score,
				))
			}
		}
	}
	return errors.Join(problems...)
}

func (p ReleasePolicy) ReleaseReady() bool {
	return p.Status == PolicyApproved && p.Validate() == nil
}

func (p ReleasePolicy) JudgeAgreementFloor() (float64, error) {
	if !p.ReleaseReady() || p.Release.MinimumJudgeAgreement == nil {
		return 0, errors.New("judge agreement floor requires an approved release policy")
	}
	return *p.Release.MinimumJudgeAgreement, nil
}

func (p ReleasePolicy) RunnerSettings(evalset EvalSet) (float64, int, []string, error) {
	if err := p.ValidateEvalSet(evalset); err != nil {
		return 0, 0, nil, err
	}
	if !p.ReleaseReady() {
		return 0, 0, nil, errors.New("release policy requires calibrated Go trials and approval")
	}
	required := make([]string, 0, len(p.Cases))
	for _, policyCase := range p.Cases {
		if policyCase.Mandatory {
			required = append(required, policyCase.ID)
		}
	}
	slices.Sort(required)
	return *p.Release.MinimumPassRate, *p.Release.MinimumRepeats, required, nil
}

// CalibrationSettings derives an exact-source trial from the same case
// classification that will later govern qualification. It deliberately uses
// the learner floor and a caller-selected repeat count: those are observations
// to calibrate, never release thresholds.
func (p ReleasePolicy) CalibrationSettings(evalset EvalSet, repeat int) (float64, []string, error) {
	if err := p.ValidateEvalSet(evalset); err != nil {
		return 0, nil, err
	}
	if p.Status != PolicyCalibrationRequired {
		return 0, nil, errors.New("policy calibration trials require calibration-required status")
	}
	if repeat < 2 {
		return 0, nil, errors.New("policy calibration trials require at least two repeats")
	}
	required := make([]string, 0, len(p.Cases))
	for _, policyCase := range p.Cases {
		if policyCase.Mandatory {
			required = append(required, policyCase.ID)
		}
	}
	slices.Sort(required)
	return p.LearnerMinimumPassRate, required, nil
}

func (p ReleasePolicy) ValidateRunBudget(artifact RunArtifact) error {
	if !p.ReleaseReady() {
		return errors.New("release policy requires calibrated Go trials and approval")
	}
	usage, err := totalUsage(artifact.Cases)
	if err != nil {
		return fmt.Errorf("evaluation usage is invalid: %w", err)
	}
	if usage.TotalTokens > *p.Release.MaximumTotalTokens {
		return fmt.Errorf("evaluation total tokens %d exceed release budget %d", usage.TotalTokens, *p.Release.MaximumTotalTokens)
	}
	if usage.ModelCalls > *p.Release.MaximumModelCalls {
		return fmt.Errorf("evaluation model calls %d exceed release budget %d", usage.ModelCalls, *p.Release.MaximumModelCalls)
	}
	return nil
}

func (r ReleaseThresholds) validate(status PolicyStatus) []error {
	var problems []error
	if r.MinimumPassRate != nil && (!finiteUnitInterval(*r.MinimumPassRate) || *r.MinimumPassRate == 0) {
		problems = append(problems, errors.New("release minimum_pass_rate must be finite and greater than 0 through 1"))
	}
	if r.MinimumRepeats != nil && *r.MinimumRepeats < 2 {
		problems = append(problems, errors.New("release minimum_repeats must be at least 2"))
	}
	if r.MinimumJudgeAgreement != nil && (!finiteUnitInterval(*r.MinimumJudgeAgreement) || *r.MinimumJudgeAgreement == 0) {
		problems = append(problems, errors.New("release minimum_judge_agreement must be finite and greater than 0 through 1"))
	}
	if r.MaximumTotalTokens != nil && *r.MaximumTotalTokens < 1 {
		problems = append(problems, errors.New("release maximum_total_tokens_per_run must be positive"))
	}
	if r.MaximumModelCalls != nil && *r.MaximumModelCalls < 1 {
		problems = append(problems, errors.New("release maximum_model_calls_per_run must be positive"))
	}
	if status == PolicyApproved && (r.MinimumPassRate == nil || r.MinimumJudgeAgreement == nil || r.MinimumRepeats == nil || r.MaximumTotalTokens == nil || r.MaximumModelCalls == nil) {
		problems = append(problems, errors.New("approved policy requires calibrated release thresholds, judge agreement, repeats, and budgets"))
	}
	return problems
}

func finiteUnitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validCaseCategory(category CaseCategory) bool {
	return slices.Contains([]CaseCategory{
		CategorySafetyCritical, CategoryRequiredCapability, CategoryQualityReliability,
		CategoryCost, CategoryExploratory,
	}, category)
}

func validSafetyControl(control SafetyControl) bool {
	return slices.Contains(allSafetyControls(), control)
}

func allSafetyControls() []SafetyControl {
	return []SafetyControl{ControlApproval, ControlWrite, ControlInjection, ControlAuthority, ControlPII, ControlRefusal}
}

func scoreForControl(control SafetyControl) string {
	switch control {
	case ControlApproval:
		return "confirmation"
	case ControlWrite, ControlAuthority:
		return "authority"
	case ControlRefusal:
		return "refusal"
	case ControlInjection, ControlPII:
		return "safety"
	default:
		return ""
	}
}

func caseDeclaresScore(evalCase EvalCase, score string) bool {
	for _, invocation := range evalCase.Conversation {
		checks := invocation.DeterministicChecks
		switch score {
		case "confirmation":
			if checks.RequiredConfirmation != nil {
				return true
			}
		case "authority":
			if checks.RequiredConfirmation != nil || len(checks.ForbiddenTools) > 0 {
				return true
			}
		case "refusal":
			if len(checks.RequiredOutput) > 0 {
				return true
			}
		case "safety":
			if len(checks.ForbiddenTools) > 0 || len(checks.ForbiddenOutput) > 0 {
				return true
			}
		}
	}
	return false
}

func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
