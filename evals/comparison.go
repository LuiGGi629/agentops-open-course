package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

// ComparisonArtifactSchemaVersion is 2: schema 1 gained `total_duration_ms_delta`
// and `newly_flaky_cases`, both derived from the run artifact's own rows.
const ComparisonArtifactSchemaVersion = 2

type RunReference struct {
	Model     ModelEvidence   `json:"model"`
	EvalSet   EvalSetEvidence `json:"evalset"`
	RunID     string          `json:"run_id"`
	Transport string          `json:"transport"`
	Source    SourceEvidence  `json:"source"`
}

type ComparisonArtifact struct {
	ScoreDeltas map[string]float64 `json:"score_deltas"`
	// NewlyFlakyCases names cases the candidate passed on some samples and failed on
	// others while the baseline was consistent about them. A pass rate cannot show
	// this: a case that went from three-of-three to two-of-three moves the rate by a
	// third of one case and has changed from a guarantee into a coin toss.
	NewlyFlakyCases   []string     `json:"newly_flaky_cases"`
	Baseline          RunReference `json:"baseline"`
	Candidate         RunReference `json:"candidate"`
	BaselinePassRate  float64      `json:"baseline_pass_rate"`
	CandidatePassRate float64      `json:"candidate_pass_rate"`
	PassRateDelta     float64      `json:"pass_rate_delta"`
	TotalTokensDelta  int64        `json:"total_tokens_delta"`
	ModelCallsDelta   int64        `json:"model_calls_delta"`
	// TotalDurationDelta is the candidate's wall clock minus the baseline's, summed
	// over every sample. Without it a candidate that answers just as well and twice
	// as slowly compares clean, which is the regression Chapter 7 budgets exist for.
	TotalDurationDelta int64 `json:"total_duration_ms_delta"`
	SchemaVersion      int   `json:"schema_version"`
	DeterministicPass  bool  `json:"deterministic_pass"`
}

func totalDuration(cases []CaseResult) int64 {
	var total int64
	for _, result := range cases {
		total += result.DurationMillis
	}
	return total
}

// newlyFlakyCases finds cases the candidate is inconsistent about and the
// baseline was not.
//
// It reads the per-sample rows rather than the serialized summary, for the same
// reason the score comparison does: an artifact's own summary is a claim, and the
// rows are the evidence. A case that appears in only one of the two runs is not
// flaky, it is new or removed, and the score comparison already reports that.
func newlyFlakyCases(baseline, candidate []CaseResult) []string {
	count := func(cases []CaseResult) map[string]CaseConsistency {
		byCase := make(map[string]CaseConsistency, len(cases))
		for _, result := range cases {
			record := byCase[result.ID]
			record.ID, record.Samples = result.ID, record.Samples+1
			if result.Passed {
				record.Passed++
			}
			byCase[result.ID] = record
		}
		return byCase
	}
	before, after := count(baseline), count(candidate)
	var flaky []string
	for caseID, candidateRecord := range after {
		baselineRecord, known := before[caseID]
		if known && !baselineRecord.Flaky() && candidateRecord.Flaky() {
			flaky = append(flaky, caseID)
		}
	}
	slices.Sort(flaky)
	return flaky
}

func LoadRunArtifact(path string) (RunArtifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return RunArtifact{}, fmt.Errorf("open evaluation artifact %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var artifact RunArtifact
	if decodeErr := decoder.Decode(&artifact); decodeErr != nil {
		return RunArtifact{}, fmt.Errorf("decode evaluation artifact %s: %w", path, decodeErr)
	}
	if eofErr := requireJSONEOF(decoder); eofErr != nil {
		return RunArtifact{}, fmt.Errorf("decode evaluation artifact %s: %w", path, eofErr)
	}
	if err := validateComparisonRunIdentity(artifact); err != nil {
		return RunArtifact{}, fmt.Errorf("evaluation artifact %s has incomplete identity", path)
	}
	if len(artifact.Cases) == 0 || artifact.Summary.MinimumPassRate < 0 || artifact.Summary.MinimumPassRate > 1 {
		return RunArtifact{}, fmt.Errorf("evaluation artifact %s has invalid cases or threshold", path)
	}
	if _, usageErr := totalUsage(artifact.Cases); usageErr != nil {
		return RunArtifact{}, fmt.Errorf("evaluation artifact %s has invalid usage: %w", path, usageErr)
	}
	return artifact, nil
}

func CompareRuns(baseline, candidate RunArtifact) (ComparisonArtifact, error) {
	if err := validateComparisonRunIdentity(baseline); err != nil {
		return ComparisonArtifact{}, fmt.Errorf("baseline comparison artifact has incomplete identity: %w", err)
	}
	if err := validateComparisonRunIdentity(candidate); err != nil {
		return ComparisonArtifact{}, fmt.Errorf("candidate comparison artifact has incomplete identity: %w", err)
	}
	if baseline.EvalSet != candidate.EvalSet {
		return ComparisonArtifact{}, errors.New("comparison artifacts must use the same evalset id and digest")
	}
	if err := validateComparisonScores("baseline", baseline.Cases); err != nil {
		return ComparisonArtifact{}, err
	}
	if err := validateComparisonScores("candidate", candidate.Cases); err != nil {
		return ComparisonArtifact{}, err
	}
	baselineContract, err := comparisonCaseContract("baseline", baseline.Cases)
	if err != nil {
		return ComparisonArtifact{}, err
	}
	candidateContract, err := comparisonCaseContract("candidate", candidate.Cases)
	if err != nil {
		return ComparisonArtifact{}, err
	}
	baselineCases := caseKeys(baselineContract)
	candidateCases := caseKeys(candidateContract)
	if !slices.Equal(baselineCases, candidateCases) {
		return ComparisonArtifact{}, errors.New("comparison artifacts must contain identical case samples")
	}
	for _, key := range baselineCases {
		if !slices.Equal(baselineContract[key], candidateContract[key]) {
			return ComparisonArtifact{}, fmt.Errorf(
				"comparison artifacts must contain identical score names for case sample %q", key,
			)
		}
	}
	baselineSummary := summarizeComparisonCases(baseline.Cases)
	candidateSummary := summarizeComparisonCases(candidate.Cases)
	result := ComparisonArtifact{
		SchemaVersion: ComparisonArtifactSchemaVersion,
		Baseline:      runReference(baseline), Candidate: runReference(candidate),
		BaselinePassRate: baselineSummary.passRate, CandidatePassRate: candidateSummary.passRate,
		PassRateDelta: candidateSummary.passRate - baselineSummary.passRate,
		ScoreDeltas:   make(map[string]float64),
		// Required-case identities are not part of the sanitized artifact schema,
		// so its serialized summary cannot be independently recomputed here. The
		// comparison trusts only the case results it can actually verify.
		DeterministicPass: candidateSummary.passRate >= baselineSummary.passRate,
	}
	for name, baselineValue := range baselineSummary.scores {
		candidateValue, found := candidateSummary.scores[name]
		if !found {
			result.DeterministicPass = false
			continue
		}
		delta := candidateValue - baselineValue
		result.ScoreDeltas[name] = delta
		if delta < 0 {
			result.DeterministicPass = false
		}
	}
	baselineUsage, err := totalUsage(baseline.Cases)
	if err != nil {
		return ComparisonArtifact{}, fmt.Errorf("baseline comparison artifact has invalid usage: %w", err)
	}
	candidateUsage, err := totalUsage(candidate.Cases)
	if err != nil {
		return ComparisonArtifact{}, fmt.Errorf("candidate comparison artifact has invalid usage: %w", err)
	}
	result.TotalTokensDelta = candidateUsage.TotalTokens - baselineUsage.TotalTokens
	result.ModelCallsDelta = candidateUsage.ModelCalls - baselineUsage.ModelCalls
	result.TotalDurationDelta = totalDuration(candidate.Cases) - totalDuration(baseline.Cases)
	result.NewlyFlakyCases = newlyFlakyCases(baseline.Cases, candidate.Cases)
	// A case that became a coin toss is a regression even when the rate held: it is
	// the same evidence a single run would have shown as a pass.
	if len(result.NewlyFlakyCases) > 0 {
		result.DeterministicPass = false
	}
	return result, nil
}

func validateComparisonRunIdentity(artifact RunArtifact) error {
	if artifact.SchemaVersion != RunArtifactSchemaVersion || artifact.RunID == "" {
		return errors.New("schema version and run id are required")
	}
	if artifact.Source.Validate() != nil {
		return errors.New("a valid source identity is required")
	}
	if artifact.Model.Provider == "" || artifact.Model.Name == "" ||
		artifact.EvalSet.ID == "" || artifact.EvalSet.Digest == "" {
		return errors.New("model and evalset identities are required")
	}
	if artifact.Transport != "rest" && artifact.Transport != "a2a" {
		return errors.New("transport identity must be rest or a2a")
	}
	return nil
}

func validateComparisonScores(role string, cases []CaseResult) error {
	if len(cases) == 0 {
		return fmt.Errorf("%s comparison artifact has no cases", role)
	}
	for _, result := range cases {
		if len(result.Scores) == 0 {
			return fmt.Errorf("%s case %q sample %d has no deterministic scores", role, result.ID, result.Sample)
		}
		allScoresPassed := true
		for name, value := range result.Scores {
			if err := validateScoreValue(name, value); err != nil {
				return fmt.Errorf("%s case %q sample %d: %w", role, result.ID, result.Sample, err)
			}
			allScoresPassed = allScoresPassed && value == 1
		}
		if result.Passed != allScoresPassed {
			return fmt.Errorf(
				"%s case %q sample %d pass flag disagrees with its deterministic scores",
				role, result.ID, result.Sample,
			)
		}
	}
	return nil
}

// ComparePromptRuns keeps Git as the only instruction authority. Comparing two
// runs of the same revision would label model variance as a prompt change, and
// a dirty tree cannot name the instructions it ran.
func ComparePromptRuns(baseline, candidate RunArtifact) (ComparisonArtifact, error) {
	if !isCleanExactRevision(baseline.Source) || !isCleanExactRevision(candidate.Source) {
		return ComparisonArtifact{}, errors.New("prompt comparison artifacts must use clean exact revisions")
	}
	if baseline.Source.Revision == candidate.Source.Revision {
		return ComparisonArtifact{}, errors.New("prompt comparison artifacts must use distinct clean exact revisions")
	}
	if baseline.Model != candidate.Model {
		return ComparisonArtifact{}, errors.New("prompt comparison artifacts must use the same model identity")
	}
	if baseline.Transport != candidate.Transport {
		return ComparisonArtifact{}, errors.New("prompt comparison artifacts must use the same transport")
	}
	return CompareRuns(baseline, candidate)
}

func isCleanExactRevision(source SourceEvidence) bool {
	return source.Validate() == nil && !source.Dirty && source.Revision != ""
}

func runReference(artifact RunArtifact) RunReference {
	return RunReference{
		RunID: artifact.RunID, Source: artifact.Source,
		Model: artifact.Model, EvalSet: artifact.EvalSet, Transport: artifact.Transport,
	}
}

func comparisonCaseContract(role string, cases []CaseResult) (map[string][]string, error) {
	contract := make(map[string][]string, len(cases))
	for _, result := range cases {
		if result.ID == "" || result.Sample < 1 {
			return nil, fmt.Errorf("%s comparison artifact needs a valid case id and positive sample", role)
		}
		key := fmt.Sprintf("%s/%d", result.ID, result.Sample)
		if _, duplicate := contract[key]; duplicate {
			return nil, fmt.Errorf("%s comparison artifact contains duplicate case sample %q", role, key)
		}
		scoreNames := make([]string, 0, len(result.Scores))
		for name := range result.Scores {
			scoreNames = append(scoreNames, name)
		}
		slices.Sort(scoreNames)
		contract[key] = scoreNames
	}
	return contract, nil
}

func caseKeys(contract map[string][]string) []string {
	keys := make([]string, 0, len(contract))
	for key := range contract {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

type comparisonSummary struct {
	scores   map[string]float64
	passRate float64
}

func summarizeComparisonCases(cases []CaseResult) comparisonSummary {
	summary := comparisonSummary{scores: make(map[string]float64)}
	counts := make(map[string]int)
	for _, result := range cases {
		if result.Passed {
			summary.passRate++
		}
		for name, value := range result.Scores {
			summary.scores[name] += value
			counts[name]++
		}
	}
	if len(cases) > 0 {
		summary.passRate /= float64(len(cases))
	}
	for name := range summary.scores {
		summary.scores[name] /= float64(counts[name])
	}
	return summary
}

func totalUsage(cases []CaseResult) (Usage, error) {
	var usage Usage
	for _, result := range cases {
		var err error
		usage, err = usage.add(result.Usage)
		if err != nil {
			return Usage{}, fmt.Errorf("case %q sample %d: %w", result.ID, result.Sample, err)
		}
	}
	return usage, nil
}
