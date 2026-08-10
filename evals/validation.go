package evals

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const forbiddenAgentImportPrefix = "github.com/MLOps-Courses/agentops-open-course-go/agents/go"

type AssetPaths struct {
	ModuleDir     string
	DataDir       string
	Calibration   string
	CostBaseline  string
	Dashboard     string
	ReleasePolicy string
	EvalSets      []string
}

type ValidationSummary struct {
	ReleasePolicyStatus PolicyStatus `json:"release_policy_status"`
	EvalSets            int          `json:"evalsets"`
	Cases               int          `json:"cases"`
	CalibrationCases    int          `json:"calibration_cases"`
	CostCases           int          `json:"cost_cases"`
	ReleasePolicyReady  bool         `json:"release_policy_ready"`
}

func ValidateAssets(ctx context.Context, paths AssetPaths) (ValidationSummary, error) {
	domain, err := LoadDomain(paths.DataDir)
	if err != nil {
		return ValidationSummary{}, err
	}
	summary := ValidationSummary{}
	loaded := make(map[string]EvalSet, len(paths.EvalSets))
	for _, path := range paths.EvalSets {
		evalset, loadErr := LoadEvalSet(path)
		if loadErr != nil {
			return ValidationSummary{}, loadErr
		}
		if domainErr := evalset.ValidateDomain(domain); domainErr != nil {
			return ValidationSummary{}, fmt.Errorf("validate evalset domain %s: %w", path, domainErr)
		}
		loaded[filepath.Base(path)] = evalset
		summary.EvalSets++
		summary.Cases += len(evalset.Cases)
		if strings.Contains(filepath.Base(path), "triage-report") {
			for _, evalCase := range evalset.Cases {
				for _, invocation := range evalCase.Conversation {
					if _, reportErr := ParseTriageReport(invocation.FinalResponse.Text()); reportErr != nil {
						return ValidationSummary{}, fmt.Errorf("reference report %q: %w", evalCase.ID, reportErr)
					}
				}
			}
		}
	}
	calibration, err := LoadCalibrationSet(paths.Calibration)
	if err != nil {
		return ValidationSummary{}, err
	}
	summary.CalibrationCases = len(calibration.Cases)
	baseline, err := LoadCostBaseline(paths.CostBaseline)
	if err != nil {
		return ValidationSummary{}, err
	}
	summary.CostCases = len(baseline.Cases)
	core, found := loaded["ops.evalset.json"]
	if !found {
		return ValidationSummary{}, errors.New("asset validation needs ops.evalset.json")
	}
	if caseSetErr := equalCaseSet(core, baseline); caseSetErr != nil {
		return ValidationSummary{}, caseSetErr
	}
	policy, err := LoadReleasePolicy(paths.ReleasePolicy)
	if err != nil {
		return ValidationSummary{}, err
	}
	if err := policy.ValidateEvalSet(core); err != nil {
		return ValidationSummary{}, err
	}
	summary.ReleasePolicyStatus = policy.Status
	summary.ReleasePolicyReady = policy.ReleaseReady()
	if err := validateDashboard(paths.Dashboard); err != nil {
		return ValidationSummary{}, err
	}
	if err := ValidateImportBoundary(ctx, paths.ModuleDir); err != nil {
		return ValidationSummary{}, err
	}
	return summary, nil
}

func ValidateImportBoundary(ctx context.Context, moduleDir string) error {
	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return fmt.Errorf("read evaluation go.mod: %w", err)
	}
	if strings.Contains(string(goMod), forbiddenAgentImportPrefix) {
		return errors.New("evaluation go.mod requires the agent module")
	}
	command := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "./...")
	command.Dir = moduleDir
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("resolve evaluation import graph: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		if scanner.Text() == forbiddenAgentImportPrefix || strings.HasPrefix(scanner.Text(), forbiddenAgentImportPrefix+"/") {
			return fmt.Errorf("evaluation import graph crosses into %s", scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan evaluation import graph: %w", err)
	}
	return nil
}

func equalCaseSet(evalset EvalSet, baseline CostBaseline) error {
	evalIDs := make([]string, 0, len(evalset.Cases))
	for _, evalCase := range evalset.Cases {
		evalIDs = append(evalIDs, evalCase.ID)
	}
	baselineIDs := make([]string, 0, len(baseline.Cases))
	for caseID := range baseline.Cases {
		baselineIDs = append(baselineIDs, caseID)
	}
	slices.Sort(evalIDs)
	slices.Sort(baselineIDs)
	if !slices.Equal(evalIDs, baselineIDs) {
		return fmt.Errorf("cost baseline cases %v do not match core evalset %v", baselineIDs, evalIDs)
	}
	return nil
}

func validateDashboard(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open evaluation dashboard %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	var dashboard struct {
		UID    string            `json:"uid"`
		Title  string            `json:"title"`
		Panels []json.RawMessage `json:"panels"`
	}
	if err := decoder.Decode(&dashboard); err != nil {
		return fmt.Errorf("decode evaluation dashboard %s: %w", path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode evaluation dashboard %s: %w", path, err)
	}
	if dashboard.UID == "" || dashboard.Title == "" || len(dashboard.Panels) < 2 {
		return errors.New("evaluation dashboard needs a uid, title, and at least two panels")
	}
	return nil
}
