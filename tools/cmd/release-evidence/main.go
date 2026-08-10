package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/releasecheck"
)

func main() {
	input := releasecheck.EvaluationQualificationInput{}
	flag.StringVar(&input.EvaluationPath, "evaluation", "", "sanitized eval-results.json")
	flag.StringVar(&input.CalibrationPath, "calibration", "", "sanitized judge-calibration-results.json")
	flag.StringVar(&input.CostObservationPath, "cost-observation", "", "sanitized cost-observed.json")
	flag.StringVar(&input.CostBaselinePath, "cost-baseline", "", "reviewed cost baseline")
	flag.StringVar(&input.EvalSetPath, "evalset", "", "reviewed source evalset")
	flag.StringVar(&input.PolicyPath, "policy", "", "approved repository release policy")
	flag.StringVar(&input.CalibrationSet, "calibration-set", "", "reviewed judge calibration set")
	flag.StringVar(&input.Repository, "repository", "", "owner/repository")
	flag.StringVar(&input.SHA, "sha", "", "full source commit")
	flag.StringVar(&input.TreeDigest, "tree-digest", "", "exact qualified source tree digest")
	flag.Int64Var(&input.WorkflowRunID, "run-id", 0, "GitHub Eval workflow run ID")
	flag.Int64Var(&input.WorkflowAttempt, "run-attempt", 0, "GitHub Eval workflow attempt")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "release-evidence does not accept positional arguments")
		os.Exit(2)
	}
	evidence, err := releasecheck.QualifyEvaluation(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-evidence:", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(evidence); err != nil {
		fmt.Fprintln(os.Stderr, "release-evidence:", err)
		os.Exit(1)
	}
}
