package evals

import (
	"errors"
	"fmt"
)

const CostObservationSchemaVersion = 3

type CostSample struct {
	ID          string `json:"id"`
	Sample      int    `json:"sample"`
	TotalTokens int64  `json:"total_tokens"`
	ModelCalls  int64  `json:"model_calls"`
}

// CostObservation is the content-free handoff produced by a model-backed run.
// A reviewer can promote its positive usage into cost_baseline.json only after
// confirming that every identity field describes the candidate runtime.
type CostObservation struct {
	ContextLength            *int64          `json:"context_length"`
	OllamaVersion            *string         `json:"ollama_version"`
	Temperature              *float64        `json:"temperature"`
	Model                    ModelEvidence   `json:"model"`
	EvalSet                  EvalSetEvidence `json:"evalset"`
	RunID                    string          `json:"run_id"`
	Transport                string          `json:"transport"`
	PromptSelection          string          `json:"prompt_selection"`
	EvaluationContractDigest string          `json:"evaluation_contract_digest"`
	Source                   SourceEvidence  `json:"source"`
	Cases                    []CostSample    `json:"cases"`
	SchemaVersion            int             `json:"schema_version"`
}

func NewCostObservation(artifact RunArtifact, identity CostIdentity) (CostObservation, error) {
	if err := validateCostObservationIdentity(artifact, identity); err != nil {
		return CostObservation{}, err
	}
	observation := CostObservation{
		SchemaVersion:            CostObservationSchemaVersion,
		RunID:                    artifact.RunID,
		Source:                   artifact.Source,
		Model:                    artifact.Model,
		EvalSet:                  artifact.EvalSet,
		Transport:                artifact.Transport,
		ContextLength:            identity.ContextLength,
		OllamaVersion:            identity.OllamaVersion,
		Temperature:              identity.Temperature,
		PromptSelection:          identity.PromptSelection,
		EvaluationContractDigest: identity.EvaluationContractDigest,
		Cases:                    make([]CostSample, 0, len(artifact.Cases)),
	}
	seen := make(map[string]struct{}, len(artifact.Cases))
	for _, result := range artifact.Cases {
		if result.Usage.TotalTokens <= 0 || result.Usage.ModelCalls <= 0 {
			return CostObservation{}, fmt.Errorf(
				"case %q sample %d has non-positive model usage",
				result.ID,
				result.Sample,
			)
		}
		if result.Sample < 1 {
			return CostObservation{}, fmt.Errorf("case %q has invalid sample %d", result.ID, result.Sample)
		}
		key := fmt.Sprintf("%s/%d", result.ID, result.Sample)
		if _, exists := seen[key]; exists {
			return CostObservation{}, fmt.Errorf("cost observation repeats case sample %q", key)
		}
		seen[key] = struct{}{}
		observation.Cases = append(observation.Cases, CostSample{
			ID:          result.ID,
			Sample:      result.Sample,
			TotalTokens: result.Usage.TotalTokens,
			ModelCalls:  result.Usage.ModelCalls,
		})
	}
	if len(observation.Cases) == 0 {
		return CostObservation{}, errors.New("cost observation has no cases")
	}
	return observation, nil
}

func validateCostObservationIdentity(artifact RunArtifact, identity CostIdentity) error {
	var problems []error
	if identity.ModelProvider != artifact.Model.Provider || identity.Model != artifact.Model.Name {
		problems = append(problems, errors.New("cost identity model does not match the run artifact"))
	}
	digest := ""
	if identity.ModelDigest != nil {
		digest = *identity.ModelDigest
	}
	if digest != artifact.Model.Digest {
		problems = append(problems, errors.New("cost identity model digest does not match the run artifact"))
	}
	if identity.Source != artifact.Source {
		problems = append(problems, errors.New("cost identity source does not match the run artifact"))
	}
	if identity.EvaluationContractDigest != artifact.EvalSet.Digest {
		problems = append(problems, errors.New("cost identity evalset digest does not match the run artifact"))
	}
	if identity.PromptSelection == "" {
		problems = append(problems, errors.New("cost identity prompt selection is required"))
	}
	return errors.Join(problems...)
}
