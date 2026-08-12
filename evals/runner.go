package evals

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

const RunArtifactSchemaVersion = 4

type ModelEvidence struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Digest   string `json:"digest,omitempty"`
}

type EvalSetEvidence struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

type CaseResult struct {
	Scores map[string]float64 `json:"scores"`
	ID     string             `json:"id"`
	Usage  Usage              `json:"usage"`
	Sample int                `json:"sample"`
	Passed bool               `json:"passed"`
	// DeterministicPassed drops judged verdicts. It never serializes: the artifact
	// keeps one `passed` per sample, and this decides `required_cases_passed` alone.
	DeterministicPassed bool `json:"-"`
}

type RunSummary struct {
	Passed              int     `json:"passed"`
	Failed              int     `json:"failed"`
	PassRate            float64 `json:"pass_rate"`
	MinimumPassRate     float64 `json:"minimum_pass_rate"`
	RequiredCasesPassed bool    `json:"required_cases_passed"`
}

// RunArtifact is deliberately content-free: prompts, answers, tool arguments,
// tool results, rationales, URLs, and errors never cross this boundary. What
// remains is what a reader needs to reproduce the run — the checkout, the
// model, the evalset, the per-case scores, and the per-case usage.
type RunArtifact struct {
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   time.Time       `json:"completed_at"`
	Model         ModelEvidence   `json:"model"`
	EvalSet       EvalSetEvidence `json:"evalset"`
	RunID         string          `json:"run_id"`
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
	Clock           func() time.Time
	ClientFactory   ClientFactory
	Model           ModelEvidence
	RunID           string
	Transport       string
	EvalSet         EvalSet
	Source          SourceEvidence
	RequiredCases   []string
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
		Source:        config.Source,
		Model:         config.Model,
		EvalSet:       EvalSetEvidence{ID: config.EvalSet.ID, Digest: config.EvalSet.Digest},
		Transport:     config.Transport,
		StartedAt:     clock().UTC(),
		Cases:         make([]CaseResult, 0, len(config.EvalSet.Cases)*repeat),
	}
	runCtx, endRun, err := config.Recorder.StartRun(ctx, RunEvidence{
		RunID: config.RunID, Source: config.Source,
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
	if config.Judge != nil {
		verdict, err := config.Judge.Judge(caseCtx, JudgeInput{
			Questions: questions, Answers: answers, ReferenceAnswers: references,
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("judge: %w", err)
		}
		evaluated.merge(NewStochasticBinaryScore("judge", verdict.Passed, verdict.Rationale))
	}
	result = CaseResult{
		ID: evalCase.ID, Sample: sample, Passed: evaluated.passed(),
		DeterministicPassed: evaluated.deterministicPassed(),
		Scores:              evaluated.sanitized(), Usage: evaluated.usage,
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
	return errors.Join(problems...)
}

func summarizeCases(results []CaseResult, required []string, minimumPassRate float64) RunSummary {
	summary := RunSummary{MinimumPassRate: minimumPassRate, RequiredCasesPassed: true}
	casePass := make(map[string]bool)
	deterministicPass := make(map[string]bool)
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
		priorDeterministic, seen := deterministicPass[result.ID]
		deterministicPass[result.ID] = result.DeterministicPassed && (!seen || priorDeterministic)
	}
	if len(results) > 0 {
		summary.PassRate = float64(summary.Passed) / float64(len(results))
	}
	// A required case is a safety claim, so it folds over deterministic scores only.
	// The judge still costs the run its pass rate above.
	for _, caseID := range required {
		if !deterministicPass[caseID] {
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

// deterministicPassed answers the same question as passed(), ignoring judged verdicts.
//
// A required case is a safety claim — confirmation was asked for, the skill was loaded,
// prior context was recalled — and every one of those is decidable by a rule. Letting a
// 4B model judging its own family veto that claim would make the strictest gate in the
// harness the least reliable one. Like passed(), an empty map is not a pass.
func (s caseScores) deterministicPassed() bool {
	found := false
	for _, score := range s.values {
		if score.Stochastic {
			continue
		}
		found = true
		if !score.Passed {
			return false
		}
	}
	return found
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
