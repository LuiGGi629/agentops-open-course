package evals

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

const RunArtifactSchemaVersion = 3

type ModelEvidence struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Digest   string `json:"digest,omitempty"`
}

type EvalSetEvidence struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type PolicyEvidence struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type CaseResult struct {
	Scores map[string]float64 `json:"scores"`
	ID     string             `json:"id"`
	Usage  Usage              `json:"usage"`
	Sample int                `json:"sample"`
	Passed bool               `json:"passed"`
}

type RunSummary struct {
	Passed              int     `json:"passed"`
	Failed              int     `json:"failed"`
	PassRate            float64 `json:"pass_rate"`
	MinimumPassRate     float64 `json:"minimum_pass_rate"`
	RequiredCasesPassed bool    `json:"required_cases_passed"`
}

// RunArtifact is deliberately content-free. It is safe to attach to a release:
// prompts, answers, tool arguments, tool results, rationales, URLs, and errors
// never cross this boundary.
type RunArtifact struct {
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   time.Time       `json:"completed_at"`
	Model         ModelEvidence   `json:"model"`
	EvalSet       EvalSetEvidence `json:"evalset"`
	Policy        *PolicyEvidence `json:"policy,omitempty"`
	RunID         string          `json:"run_id"`
	Platform      string          `json:"platform_identity"`
	Source        SourceEvidence  `json:"source"`
	Transport     string          `json:"transport"`
	Cases         []CaseResult    `json:"cases"`
	Summary       RunSummary      `json:"summary"`
	SchemaVersion int             `json:"schema_version"`
}

type ClientFactory func(context.Context, EvalCase, int) (AgentClient, func() error, error)

type RunnerConfig struct {
	Domain          Domain
	Recorder        EvidenceRecorder
	Judge           VerdictJudge
	CostBaseline    *CostBaseline
	Clock           func() time.Time
	ClientFactory   ClientFactory
	ReleasePolicy   *ReleasePolicy
	Model           ModelEvidence
	RunID           string
	Platform        string
	Transport       string
	EvalSet         EvalSet
	Source          SourceEvidence
	RequiredCases   []string
	CostTolerance   float64
	MinimumPassRate float64
	Repeat          int
	RequireSchema   bool
	RequireGrounded bool
}

type caseScores struct {
	values map[string]Score
	usage  Usage
}

func Run(ctx context.Context, config RunnerConfig) (RunArtifact, error) {
	if err := validateRunnerConfig(config); err != nil {
		return RunArtifact{}, err
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	repeat := config.Repeat
	if repeat == 0 {
		repeat = 1
	}
	artifact := RunArtifact{
		SchemaVersion: RunArtifactSchemaVersion,
		RunID:         config.RunID,
		Platform:      config.Platform,
		Source:        config.Source,
		Model:         config.Model,
		EvalSet:       EvalSetEvidence{ID: config.EvalSet.ID, Digest: config.EvalSet.Digest},
		Transport:     config.Transport,
		StartedAt:     clock().UTC(),
		Cases:         make([]CaseResult, 0, len(config.EvalSet.Cases)*repeat),
	}
	if config.ReleasePolicy != nil {
		artifact.Policy = &PolicyEvidence{
			Version: config.ReleasePolicy.PolicyVersion,
			Digest:  config.ReleasePolicy.Digest,
		}
	}
	runCtx, endRun, err := config.Recorder.StartRun(ctx, RunEvidence{
		RunID: config.RunID, Source: config.Source, Platform: config.Platform,
		Model: config.Model.Name, EvalSet: config.EvalSet.ID, Transport: config.Transport,
	})
	if err != nil {
		return RunArtifact{}, fmt.Errorf("start run evidence: %w", err)
	}
	runPassed := false
	defer func() { endRun(RunOutcome{Passed: runPassed}) }()

	for sample := 1; sample <= repeat; sample++ {
		for _, evalCase := range config.EvalSet.Cases {
			result, err := runCase(runCtx, config, evalCase, sample)
			if err != nil {
				return RunArtifact{}, fmt.Errorf("case %q sample %d: %w", evalCase.ID, sample, err)
			}
			artifact.Cases = append(artifact.Cases, result)
		}
	}
	artifact.Summary = summarizeCases(artifact.Cases, config.RequiredCases, config.MinimumPassRate)
	artifact.CompletedAt = clock().UTC()
	runPassed = artifact.Passed()
	return artifact, nil
}

func (a RunArtifact) Passed() bool {
	return a.Summary.PassRate >= a.Summary.MinimumPassRate && a.Summary.RequiredCasesPassed
}

func runCase(ctx context.Context, config RunnerConfig, evalCase EvalCase, sample int) (result CaseResult, err error) {
	caseCtx, endCase, err := config.Recorder.StartCase(ctx, CaseEvidence{CaseID: evalCase.ID, Sample: sample})
	if err != nil {
		return CaseResult{}, fmt.Errorf("start case evidence: %w", err)
	}
	outcome := CaseOutcome{}
	defer func() { endCase(outcome) }()

	client, cleanup, err := config.ClientFactory(caseCtx, evalCase, sample)
	if err != nil {
		return CaseResult{}, fmt.Errorf("start isolated agent client: %w", err)
	}
	defer func() {
		err = errors.Join(err, client.Close(), cleanup())
	}()
	sessionID, err := client.CreateSession(caseCtx)
	if err != nil {
		return CaseResult{}, err
	}
	evaluated := caseScores{values: make(map[string]Score)}
	questions := make([]string, 0, len(evalCase.Conversation))
	answers := make([]string, 0, len(evalCase.Conversation))
	references := make([]string, 0, len(evalCase.Conversation))
	for _, invocation := range evalCase.Conversation {
		question := invocation.UserContent.Text()
		turn, err := client.Send(caseCtx, sessionID, question)
		if err != nil {
			return CaseResult{}, err
		}
		if turn.Failed() {
			// Remote error codes are provider-controlled and can contain echoed content.
			return CaseResult{}, errors.New("provider turn failed")
		}
		evaluated.usage, err = evaluated.usage.add(turn.Usage)
		if err != nil {
			return CaseResult{}, fmt.Errorf("aggregate usage: %w", err)
		}
		evaluated.merge(TrajectoryScore(turn, invocation))
		for _, score := range DeterministicControlScores(turn, invocation.DeterministicChecks) {
			evaluated.merge(score)
		}
		if config.RequireGrounded {
			evaluated.merge(GroundednessScore(turn, question, config.Domain))
		}
		if config.RequireSchema {
			evaluated.merge(SchemaScore(turn))
		}
		questions = append(questions, question)
		answers = append(answers, turn.Text)
		references = append(references, invocation.FinalResponse.Text())
	}
	if config.CostBaseline != nil {
		evaluated.merge(CostScore(evalCase.ID, evaluated.usage, *config.CostBaseline, config.CostTolerance))
	}
	if config.Judge != nil {
		verdict, err := config.Judge.Judge(caseCtx, JudgeInput{
			Questions: questions, Answers: answers, ReferenceAnswers: references,
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("judge: %w", err)
		}
		evaluated.merge(NewBinaryScore("judge", verdict.Passed, verdict.Rationale))
	}
	result = CaseResult{
		ID: evalCase.ID, Sample: sample, Passed: evaluated.passed(),
		Scores: evaluated.sanitized(), Usage: evaluated.usage,
	}
	outcome = CaseOutcome{Passed: result.Passed, Usage: result.Usage}
	for _, name := range sortedScoreNames(evaluated.values) {
		if err := config.Recorder.RecordScore(caseCtx, evaluated.values[name]); err != nil {
			return CaseResult{}, err
		}
	}
	return result, nil
}

func validateRunnerConfig(config RunnerConfig) error {
	var problems []error
	if err := config.EvalSet.Validate(); err != nil {
		problems = append(problems, err)
	}
	if config.EvalSet.Digest == "" {
		problems = append(problems, errors.New("evalset digest is required"))
	}
	if config.RunID == "" || config.Model.Provider == "" || config.Model.Name == "" {
		problems = append(problems, errors.New("run id, model provider, and model name are required"))
	}
	if err := config.Source.Validate(); err != nil {
		problems = append(problems, err)
	}
	if !validPlatformIdentity(config.Platform) {
		problems = append(problems, errors.New("platform identity must be non-empty, bounded, trimmed, and single-line"))
	}
	if config.Transport != "rest" && config.Transport != "a2a" {
		problems = append(problems, fmt.Errorf("unsupported transport %q", config.Transport))
	}
	if config.Repeat < 0 {
		problems = append(problems, errors.New("repeat must not be negative"))
	}
	if math.IsNaN(config.MinimumPassRate) || math.IsInf(config.MinimumPassRate, 0) ||
		config.MinimumPassRate < 0 || config.MinimumPassRate > 1 {
		problems = append(problems, errors.New("minimum pass rate must be between 0 and 1"))
	}
	if config.ClientFactory == nil || config.Recorder == nil {
		problems = append(problems, errors.New("client factory and evidence recorder are required"))
	}
	caseIDs := make(map[string]struct{}, len(config.EvalSet.Cases))
	for _, evalCase := range config.EvalSet.Cases {
		caseIDs[evalCase.ID] = struct{}{}
	}
	for _, caseID := range config.RequiredCases {
		if _, found := caseIDs[caseID]; !found {
			problems = append(problems, fmt.Errorf("required case %q is not in evalset", caseID))
		}
	}
	if config.ReleasePolicy != nil {
		minimum, repeat, required, err := config.ReleasePolicy.RunnerSettings(config.EvalSet)
		if err != nil {
			problems = append(problems, err)
		} else {
			actualRequired := slices.Clone(config.RequiredCases)
			slices.Sort(actualRequired)
			if !validSHA256Digest(config.ReleasePolicy.Digest) ||
				config.MinimumPassRate != minimum || config.Repeat != repeat || !slices.Equal(actualRequired, required) {
				problems = append(problems, errors.New("runner settings do not match the approved release policy"))
			}
		}
	}
	return errors.Join(problems...)
}

func summarizeCases(results []CaseResult, required []string, minimumPassRate float64) RunSummary {
	summary := RunSummary{MinimumPassRate: minimumPassRate, RequiredCasesPassed: true}
	casePass := make(map[string]bool)
	for _, result := range results {
		previous, observed := casePass[result.ID]
		if result.Passed {
			summary.Passed++
			if !observed {
				casePass[result.ID] = true
			} else {
				casePass[result.ID] = previous
			}
		} else {
			summary.Failed++
			casePass[result.ID] = false
		}
	}
	if len(results) > 0 {
		summary.PassRate = float64(summary.Passed) / float64(len(results))
	}
	for _, caseID := range required {
		if !casePass[caseID] {
			summary.RequiredCasesPassed = false
		}
	}
	return summary
}

func (s *caseScores) merge(score Score) {
	if previous, found := s.values[score.Name]; !found || score.Value < previous.Value {
		s.values[score.Name] = score
	}
}

func (s caseScores) passed() bool {
	for _, score := range s.values {
		if !score.Passed {
			return false
		}
	}
	return len(s.values) > 0
}

func (s caseScores) sanitized() map[string]float64 {
	result := make(map[string]float64, len(s.values))
	for name, score := range s.values {
		result[name] = score.Value
	}
	return result
}

func sortedScoreNames(scores map[string]Score) []string {
	names := make([]string, 0, len(scores))
	for name := range scores {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
