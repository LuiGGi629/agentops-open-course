package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
)

const (
	DefaultCostTolerance = 0.25
	// PromptAuthorityGit names the immutable source revision as the prompt
	// authority without baking that revision into the comparison key.
	PromptAuthorityGit = "git"
)

type CostCase struct {
	TotalTokens int64 `json:"total_tokens"`
	ModelCalls  int64 `json:"model_calls"`
}

type CostBaseline struct {
	ModelDigest              *string             `json:"model_digest"`
	SourceRevision           *string             `json:"source_revision"`
	ContextLength            *int64              `json:"context_length"`
	OllamaVersion            *string             `json:"ollama_version"`
	Temperature              *float64            `json:"temperature"`
	Cases                    map[string]CostCase `json:"cases"`
	ModelProvider            string              `json:"model_provider"`
	Model                    string              `json:"model"`
	PromptSelection          string              `json:"prompt_selection"`
	EvaluationContractDigest string              `json:"evaluation_contract_digest"`
	SchemaVersion            int                 `json:"schema_version"`
}

type CostIdentity struct {
	ModelDigest              *string
	ContextLength            *int64
	OllamaVersion            *string
	Temperature              *float64
	ModelProvider            string
	Model                    string
	PromptSelection          string
	EvaluationContractDigest string
	Source                   SourceEvidence
}

func LoadCostBaseline(path string) (CostBaseline, error) {
	file, err := os.Open(path)
	if err != nil {
		return CostBaseline{}, fmt.Errorf("open cost baseline %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var baseline CostBaseline
	if err := decoder.Decode(&baseline); err != nil {
		return CostBaseline{}, fmt.Errorf("decode cost baseline %s: %w", path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return CostBaseline{}, fmt.Errorf("decode cost baseline %s: %w", path, err)
	}
	if err := baseline.Validate(); err != nil {
		return CostBaseline{}, fmt.Errorf("validate cost baseline %s: %w", path, err)
	}
	return baseline, nil
}

func (b CostBaseline) Validate() error {
	var problems []error
	if b.SchemaVersion != 2 {
		problems = append(problems, fmt.Errorf("schema_version=%d, want 2", b.SchemaVersion))
	}
	for field, value := range map[string]string{
		"model_provider":             b.ModelProvider,
		"model":                      b.Model,
		"prompt_selection":           b.PromptSelection,
		"evaluation_contract_digest": b.EvaluationContractDigest,
	} {
		if value == "" {
			problems = append(problems, fmt.Errorf("%s is required", field))
		}
	}
	if b.SourceRevision != nil && *b.SourceRevision == "" {
		problems = append(problems, errors.New("source_revision must be null or non-empty"))
	}
	if b.ModelDigest != nil && *b.ModelDigest == "" {
		problems = append(problems, errors.New("model_digest must be null or non-empty"))
	}
	if b.ContextLength != nil && *b.ContextLength <= 0 {
		problems = append(problems, errors.New("context_length must be null or positive"))
	}
	if b.OllamaVersion != nil && *b.OllamaVersion == "" {
		problems = append(problems, errors.New("ollama_version must be null or non-empty"))
	}
	if b.Temperature != nil && (math.IsNaN(*b.Temperature) || math.IsInf(*b.Temperature, 0) || *b.Temperature < 0 || *b.Temperature > 2) {
		problems = append(problems, errors.New("temperature must be null or between 0 and 2"))
	}
	if len(b.Cases) == 0 {
		problems = append(problems, errors.New("cases must not be empty"))
	}
	for caseID, usage := range b.Cases {
		if caseID == "" || usage.TotalTokens <= 0 || usage.ModelCalls <= 0 {
			problems = append(problems, fmt.Errorf("case %q needs positive total_tokens and model_calls", caseID))
		}
	}
	return errors.Join(problems...)
}

func (b CostBaseline) Comparable(identity CostIdentity) error {
	checks := []struct {
		baseline any
		observed any
		name     string
	}{
		{name: "model_provider", baseline: b.ModelProvider, observed: identity.ModelProvider},
		{name: "model", baseline: b.Model, observed: identity.Model},
		{name: "model_digest", baseline: b.ModelDigest, observed: identity.ModelDigest},
		{name: "prompt_selection", baseline: b.PromptSelection, observed: identity.PromptSelection},
		{name: "evaluation_contract_digest", baseline: b.EvaluationContractDigest, observed: identity.EvaluationContractDigest},
		{name: "context_length", baseline: b.ContextLength, observed: identity.ContextLength},
		{name: "ollama_version", baseline: b.OllamaVersion, observed: identity.OllamaVersion},
		{name: "temperature", baseline: b.Temperature, observed: identity.Temperature},
	}
	var problems []error
	for _, check := range checks {
		baseline, err := json.Marshal(check.baseline)
		if err != nil {
			return fmt.Errorf("encode baseline %s: %w", check.name, err)
		}
		observed, err := json.Marshal(check.observed)
		if err != nil {
			return fmt.Errorf("encode observed %s: %w", check.name, err)
		}
		if !slices.Equal(baseline, observed) {
			problems = append(problems, fmt.Errorf(
				"%s baseline %s does not match observed %s",
				check.name,
				baseline,
				observed,
			))
		}
	}
	return errors.Join(problems...)
}

func CostRegressions(observed, baseline map[string]CostCase, tolerance float64) []string {
	if math.IsNaN(tolerance) || math.IsInf(tolerance, 0) || tolerance < 0 {
		return []string{fmt.Sprintf("tolerance %v is not finite and non-negative", tolerance)}
	}
	problems := make([]string, 0)
	for caseID := range baseline {
		if _, found := observed[caseID]; !found {
			problems = append(problems, caseID+": missing from the observation")
		}
	}
	for caseID := range observed {
		if _, found := baseline[caseID]; !found {
			problems = append(problems, caseID+": no reviewed baseline")
		}
	}
	slices.Sort(problems)
	caseIDs := make([]string, 0, len(baseline))
	for caseID := range baseline {
		if _, found := observed[caseID]; found {
			caseIDs = append(caseIDs, caseID)
		}
	}
	slices.Sort(caseIDs)
	for _, caseID := range caseIDs {
		current := observed[caseID]
		reviewed := baseline[caseID]
		if float64(current.TotalTokens) > float64(reviewed.TotalTokens)*(1+tolerance) {
			problems = append(problems, fmt.Sprintf(
				"%s total_tokens: %d exceeds baseline %d + %.0f%%",
				caseID,
				current.TotalTokens,
				reviewed.TotalTokens,
				tolerance*100,
			))
		}
		if float64(current.ModelCalls) > float64(reviewed.ModelCalls)*(1+tolerance) {
			problems = append(problems, fmt.Sprintf(
				"%s model_calls: %d exceeds baseline %d + %.0f%%",
				caseID,
				current.ModelCalls,
				reviewed.ModelCalls,
				tolerance*100,
			))
		}
	}
	return problems
}

func CostScore(caseID string, usage Usage, baseline CostBaseline, tolerance float64) Score {
	reviewed, found := baseline.Cases[caseID]
	if !found {
		return NewBinaryScore("cost", false, "case has no reviewed baseline")
	}
	observed := map[string]CostCase{caseID: {TotalTokens: usage.TotalTokens, ModelCalls: usage.ModelCalls}}
	problems := CostRegressions(observed, map[string]CostCase{caseID: reviewed}, tolerance)
	if len(problems) > 0 {
		return NewBinaryScore("cost", false, problems[0])
	}
	return NewBinaryScore("cost", true, "token and model-call usage are within the reviewed tolerance")
}
