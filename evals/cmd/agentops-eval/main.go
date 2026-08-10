package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MLOps-Courses/agentops-open-course-go/evals"
)

const usage = `agentops-eval validates assets and runs black-box agent evaluations.

Usage:
  agentops-eval validate [flags]
  agentops-eval run [flags]
  agentops-eval calibrate [flags]
  agentops-eval compare [flags]
  agentops-eval retrieval [flags]
`

func main() {
	if err := execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "agentops-eval:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	if len(arguments) == 0 {
		return errors.New("command is required; use --help")
	}
	switch arguments[0] {
	case "validate":
		return validateCommand(ctx, arguments[1:], stdout)
	case "run":
		return runCommand(ctx, arguments[1:], stdout, stderr)
	case "calibrate":
		return calibrateCommand(ctx, arguments[1:], stdout)
	case "compare":
		return compareCommand(arguments[1:], stdout)
	case "retrieval":
		return retrievalCommand(ctx, arguments[1:], stdout, stderr)
	case "help", "-h", "--help":
		_, err := io.WriteString(stdout, usage)
		return err
	default:
		return fmt.Errorf("unknown command %q\n%s", arguments[0], usage)
	}
}

func retrievalCommand(ctx context.Context, arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("retrieval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	moduleDir := flags.String("eval-dir", detectEvalDir(), "evaluation module directory")
	dataDir := flags.String("data-dir", "", "immutable agent data directory")
	output := flags.String("output", "retrieval-results.json", "sanitized retrieval artifact")
	binary := flags.String("agent-binary", "", "compiled Go agent binary")
	embeddingModel := flags.String(
		"embedding-model", getenv("AGENT_EMBEDDING_MODEL", "nomic-embed-text"), "embedding model evidence",
	)
	embeddingModelDigest := flags.String(
		"embedding-model-digest", os.Getenv("EVAL_EMBEDDING_MODEL_DIGEST"), "optional immutable embedding model digest",
	)
	configuredSource := flags.String(
		"source-commit", firstEnvironment("AGENT_SOURCE_COMMIT", "GITHUB_SHA"), "expected candidate source identity or revision",
	)
	sourceMode := flags.String("source-mode", getenv("EVAL_SOURCE_MODE", "development"), "source identity mode: development or release")
	platformIdentity := flags.String("platform-identity", os.Getenv("EVAL_PLATFORM_IDENTITY"), "sanitized runtime platform identity")
	requestTimeout := flags.Duration("request-timeout", 5*time.Minute, "one retrieval-tool request deadline")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *requestTimeout < 0 {
		return errors.New("retrieval timeouts must not be negative")
	}
	resolvedSource, err := resolveSourceIdentity(ctx, *configuredSource, *sourceMode)
	if err != nil {
		return err
	}
	if *platformIdentity == "" {
		if *sourceMode == "release" {
			return errors.New("release retrieval requires EVAL_PLATFORM_IDENTITY or --platform-identity")
		}
		*platformIdentity = "development-retrieval"
	}
	paths := assetPaths(*moduleDir, *dataDir)
	if *binary == "" {
		*binary = filepath.Join(*moduleDir, "..", "agents", "go", "bin", "agent")
	}
	artifact, err := evals.RunMCPRetrievalEvaluation(ctx, evals.MCPRetrievalEvaluationConfig{
		Source: resolvedSource, Platform: *platformIdentity, EmbeddingModelDigest: *embeddingModelDigest,
		Runtime: evals.MCPRetrievalRuntimeFactoryConfig{
			Binary: *binary, DataDir: paths.DataDir,
			EmbeddingModel: *embeddingModel,
			Environment:    agentRuntimeEnvironment(), Timeout: *requestTimeout, Output: stderr,
		},
	})
	if err != nil {
		return err
	}
	if err := evals.WriteRetrievalArtifact(resolveFrom(*moduleDir, *output), artifact); err != nil {
		return err
	}
	return writeJSON(stdout, artifact)
}

func compareCommand(arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	moduleDir := flags.String("eval-dir", detectEvalDir(), "evaluation module directory")
	baselinePath := flags.String("baseline", "", "reviewed baseline artifact")
	candidatePath := flags.String("candidate", "", "candidate artifact")
	output := flags.String("output", "prompt-comparison.json", "sanitized comparison artifact")
	requireDistinctSource := flags.Bool("require-distinct-source", false, "require artifacts from different Git revisions")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *baselinePath == "" || *candidatePath == "" {
		return errors.New("compare requires explicit --baseline and --candidate artifacts")
	}
	baseline, err := evals.LoadRunArtifact(resolveFrom(*moduleDir, *baselinePath))
	if err != nil {
		return err
	}
	candidate, err := evals.LoadRunArtifact(resolveFrom(*moduleDir, *candidatePath))
	if err != nil {
		return err
	}
	var comparison evals.ComparisonArtifact
	if *requireDistinctSource {
		comparison, err = evals.ComparePromptRuns(baseline, candidate)
	} else {
		comparison, err = evals.CompareRuns(baseline, candidate)
	}
	if err != nil {
		return err
	}
	if err := evals.WriteJSONArtifact(resolveFrom(*moduleDir, *output), comparison); err != nil {
		return err
	}
	if err := writeJSON(stdout, comparison); err != nil {
		return err
	}
	if !comparison.DeterministicPass {
		return errors.New("candidate regressed a deterministic score or pass rate")
	}
	return nil
}

func validateCommand(ctx context.Context, arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	moduleDir := flags.String("eval-dir", detectEvalDir(), "evaluation module directory")
	dataDir := flags.String("data-dir", "", "immutable agent data directory")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	paths := assetPaths(*moduleDir, *dataDir)
	summary, err := evals.ValidateAssets(ctx, paths)
	if err != nil {
		return err
	}
	return writeJSON(stdout, summary)
}

func runCommand(ctx context.Context, arguments []string, stdout, stderr io.Writer) (returnErr error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	moduleDir := flags.String("eval-dir", detectEvalDir(), "evaluation module directory")
	dataDir := flags.String("data-dir", "", "immutable agent data directory")
	evalsetPath := flags.String("evalset", "ops.evalset.json", "evalset path relative to eval-dir")
	output := flags.String("output", "eval-results.json", "sanitized run artifact path relative to eval-dir")
	costOutput := flags.String("cost-output", "", "optional sanitized cost observation path")
	agentRuntime := flags.String("agent-runtime", getenv("EVAL_AGENT_RUNTIME", "process"), "agent runtime: process or container")
	binary := flags.String("agent-binary", "", "compiled Go agent binary")
	image := flags.String("agent-image", os.Getenv("EVAL_AGENT_IMAGE"), "containerized Go agent image")
	containerEngine := flags.String("container-engine", getenv("EVAL_CONTAINER_ENGINE", "docker"), "OCI container engine")
	transport := flags.String("transport", "rest", "rest or a2a")
	entrypoint := flags.String("entrypoint", "agent", "agent, workflow, or coordinator")
	appName := flags.String("app-name", "", "REST app override")
	streaming := flags.Bool("stream", false, "use ADK REST SSE")
	repeat := flags.Int("repeat", 1, "samples per case")
	minimumPassRate := flags.Float64("min-pass-rate", 1, "minimum run pass rate")
	provider := flags.String("model-provider", getenv("AGENT_MODEL_PROVIDER", "openai-compatible"), "model provider evidence")
	model := flags.String("model", getenv("AGENT_MODEL", "qwen3:4b-instruct"), "model evidence")
	modelDigest := flags.String("model-digest", os.Getenv("EVAL_MODEL_DIGEST"), "immutable model digest evidence")
	configuredSource := flags.String("source-commit", firstEnvironment("AGENT_SOURCE_COMMIT", "GITHUB_SHA"), "expected candidate source identity or revision")
	sourceMode := flags.String("source-mode", getenv("EVAL_SOURCE_MODE", "development"), "source identity mode: development or release")
	platformIdentity := flags.String("platform-identity", os.Getenv("EVAL_PLATFORM_IDENTITY"), "sanitized runtime platform identity")
	runID := flags.String("run-id", "", "evaluation run id")
	requireGrounded := flags.Bool("require-grounded", false, "enable entity groundedness scoring")
	requireSchema := flags.Bool("require-schema", false, "enable strict triage-report scoring")
	requireCost := flags.Bool("require-cost-baseline", false, "enforce cost_baseline.json")
	costTolerance := flags.Float64("cost-tolerance", evals.DefaultCostTolerance, "cost regression tolerance")
	releasePolicyPath := flags.String("release-policy", "", "approved repository release policy relative to eval-dir")
	calibrationPolicyPath := flags.String("calibration-policy", "", "calibration-required policy for an exact-source repeated trial")
	withJudge := flags.Bool("judge", false, "enable calibrated gateway judge scoring")
	otelEndpoint := flags.String("otel-endpoint", os.Getenv("EVAL_OTEL_EXPORTER_OTLP_ENDPOINT"), "evaluation-only OTLP HTTP endpoint")
	var requiredCases stringList
	flags.Var(&requiredCases, "required-case", "case that must pass; repeatable")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	resolvedSource, err := resolveSourceIdentity(ctx, *configuredSource, *sourceMode)
	if err != nil {
		return err
	}
	if *platformIdentity == "" {
		if *sourceMode == "release" {
			return errors.New("release evaluation requires EVAL_PLATFORM_IDENTITY or --platform-identity")
		}
		*platformIdentity = "development-" + *agentRuntime
	}
	if *runID == "" {
		resolved, runIDErr := evals.NewRunID()
		if runIDErr != nil {
			return runIDErr
		}
		*runID = resolved
	}
	paths := assetPaths(*moduleDir, *dataDir)
	evalPath := resolveFrom(*moduleDir, *evalsetPath)
	evalset, err := evals.LoadEvalSet(evalPath)
	if err != nil {
		return err
	}
	domain, err := evals.LoadDomain(paths.DataDir)
	if err != nil {
		return err
	}
	if domainErr := evalset.ValidateDomain(domain); domainErr != nil {
		return domainErr
	}
	var releasePolicy *evals.ReleasePolicy
	var calibrationPolicy *evals.ReleasePolicy
	if *releasePolicyPath != "" && *calibrationPolicyPath != "" {
		return errors.New("--release-policy and --calibration-policy are mutually exclusive")
	}
	if *releasePolicyPath != "" {
		if *sourceMode != "release" {
			return errors.New("release policy requires --source-mode=release")
		}
		if flagWasSet(flags, "repeat") || flagWasSet(flags, "min-pass-rate") || len(requiredCases) > 0 {
			return errors.New("release policy owns repeat, minimum pass rate, and mandatory cases")
		}
		loadedPolicy, policyErr := evals.LoadReleasePolicy(resolveFrom(*moduleDir, *releasePolicyPath))
		if policyErr != nil {
			return policyErr
		}
		minimum, repetitions, mandatory, policyErr := loadedPolicy.RunnerSettings(evalset)
		if policyErr != nil {
			return policyErr
		}
		*minimumPassRate = minimum
		*repeat = repetitions
		requiredCases = mandatory
		releasePolicy = &loadedPolicy
	}
	if *calibrationPolicyPath != "" {
		if *sourceMode != "release" {
			return errors.New("policy calibration requires --source-mode=release")
		}
		if !flagWasSet(flags, "repeat") || flagWasSet(flags, "min-pass-rate") || len(requiredCases) > 0 {
			return errors.New("policy calibration requires an explicit repeat and owns the learner floor and mandatory cases")
		}
		loadedPolicy, policyErr := evals.LoadReleasePolicy(resolveFrom(*moduleDir, *calibrationPolicyPath))
		if policyErr != nil {
			return policyErr
		}
		minimum, mandatory, policyErr := loadedPolicy.CalibrationSettings(evalset, *repeat)
		if policyErr != nil {
			return policyErr
		}
		*minimumPassRate = minimum
		requiredCases = mandatory
		calibrationPolicy = &loadedPolicy
	}
	if *agentRuntime == "process" && *binary == "" {
		*binary = filepath.Join(*moduleDir, "..", "agents", "go", "bin", "agent")
	}
	environment := agentRuntimeEnvironment()
	factory, err := selectClientFactory(clientFactoryConfig{
		Runtime: *agentRuntime,
		Process: evals.ProcessClientFactoryConfig{
			Source: resolvedSource, Binary: *binary, DataDir: paths.DataDir, Transport: *transport,
			Entrypoint: *entrypoint, AppName: *appName, Streaming: *streaming,
			Environment: environment, Output: stderr,
		},
		Container: evals.ContainerClientFactoryConfig{
			Source: resolvedSource, Engine: *containerEngine, Image: *image,
			Transport: *transport, Entrypoint: *entrypoint,
			AppName: *appName, Streaming: *streaming, Environment: environment, Output: stderr,
		},
	}, defaultClientFactoryConstructors(ctx))
	if err != nil {
		return err
	}
	recorder, err := recorder(ctx, *otelEndpoint)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, recorder.Shutdown(ctx)) }()
	config := evals.RunnerConfig{
		EvalSet: evalset, Domain: domain, RunID: *runID, Source: resolvedSource, Platform: *platformIdentity,
		Model:     evals.ModelEvidence{Provider: *provider, Name: *model, Digest: *modelDigest},
		Transport: *transport, Repeat: *repeat, MinimumPassRate: *minimumPassRate,
		RequiredCases: requiredCases, RequireGrounded: *requireGrounded,
		RequireSchema: *requireSchema, CostTolerance: *costTolerance,
		Recorder: recorder, ClientFactory: factory, ReleasePolicy: releasePolicy,
	}
	var costIdentity *evals.CostIdentity
	if *requireCost || *costOutput != "" {
		identity, identityErr := currentCostIdentity(*provider, *model, *modelDigest, resolvedSource, evalset.Digest)
		if identityErr != nil {
			return identityErr
		}
		costIdentity = &identity
	}
	if *requireCost {
		baseline, baselineErr := evals.LoadCostBaseline(paths.CostBaseline)
		if baselineErr != nil {
			return baselineErr
		}
		if comparableErr := baseline.Comparable(*costIdentity); comparableErr != nil {
			return fmt.Errorf("cost baseline is not comparable; run and review a fresh Go-agent baseline: %w", comparableErr)
		}
		config.CostBaseline = &baseline
	}
	if *withJudge {
		judgeClient, judgeErr := gatewayJudgeFromEnvironment()
		if judgeErr != nil {
			return judgeErr
		}
		config.Judge = judgeClient
	}
	artifact, err := evals.Run(ctx, config)
	if err != nil {
		return err
	}
	if calibrationPolicy != nil {
		artifact.Policy = &evals.PolicyEvidence{
			Version: calibrationPolicy.PolicyVersion,
			Digest:  calibrationPolicy.Digest,
		}
	}
	if releasePolicy != nil {
		if err := releasePolicy.ValidateRunBudget(artifact); err != nil {
			return err
		}
	}
	if err := evals.WriteJSONArtifact(resolveFrom(*moduleDir, *output), artifact); err != nil {
		return err
	}
	if *costOutput != "" {
		observation, err := evals.NewCostObservation(artifact, *costIdentity)
		if err != nil {
			return err
		}
		if err := evals.WriteJSONArtifact(resolveFrom(*moduleDir, *costOutput), observation); err != nil {
			return err
		}
	}
	if err := writeJSON(stdout, artifact.Summary); err != nil {
		return err
	}
	return runThresholdError(artifact, calibrationPolicy != nil)
}

func runThresholdError(artifact evals.RunArtifact, calibrationTrial bool) error {
	// Calibration must preserve unsuccessful samples: they are evidence used to
	// choose policy thresholds, not an already-approved release verdict.
	if artifact.Passed() || calibrationTrial {
		return nil
	}
	return errors.New("evaluation did not clear its pass and required-case thresholds")
}

func calibrateCommand(ctx context.Context, arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	moduleDir := flags.String("eval-dir", detectEvalDir(), "evaluation module directory")
	calibrationPath := flags.String("calibration", "judge-calibration.json", "labeled calibration set")
	output := flags.String("output", "judge-calibration-results.json", "sanitized calibration artifact")
	floor := flags.Float64("floor", 0.75, "minimum agreement")
	releasePolicyPath := flags.String("release-policy", "", "approved repository release policy relative to eval-dir")
	configuredSource := flags.String("source-commit", firstEnvironment("AGENT_SOURCE_COMMIT", "GITHUB_SHA"), "expected candidate source identity or revision")
	sourceMode := flags.String("source-mode", getenv("EVAL_SOURCE_MODE", "development"), "source identity mode: development or release")
	platformIdentity := flags.String("platform-identity", os.Getenv("EVAL_PLATFORM_IDENTITY"), "sanitized judge platform identity")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	var releasePolicy *evals.ReleasePolicy
	if *releasePolicyPath != "" {
		if *sourceMode != "release" {
			return errors.New("release-policy judge calibration requires --source-mode=release")
		}
		if flagWasSet(flags, "floor") {
			return errors.New("release policy owns the judge agreement floor")
		}
		loadedPolicy, policyErr := evals.LoadReleasePolicy(resolveFrom(*moduleDir, *releasePolicyPath))
		if policyErr != nil {
			return policyErr
		}
		policyFloor, policyErr := loadedPolicy.JudgeAgreementFloor()
		if policyErr != nil {
			return policyErr
		}
		*floor = policyFloor
		releasePolicy = &loadedPolicy
	}
	resolvedSource, err := resolveSourceIdentity(ctx, *configuredSource, *sourceMode)
	if err != nil {
		return err
	}
	if *platformIdentity == "" {
		if *sourceMode == "release" {
			return errors.New("release calibration requires EVAL_PLATFORM_IDENTITY or --platform-identity")
		}
		*platformIdentity = "development-judge"
	}
	set, err := evals.LoadCalibrationSet(resolveFrom(*moduleDir, *calibrationPath))
	if err != nil {
		return err
	}
	judge, err := gatewayJudgeFromEnvironment()
	if err != nil {
		return err
	}
	result, err := evals.Calibrate(
		ctx,
		set,
		judge,
		evals.ModelEvidence{
			Provider: getenv("EVAL_JUDGE_PROVIDER", os.Getenv("AGENT_MODEL_PROVIDER")),
			Name:     os.Getenv("EVAL_JUDGE_MODEL"),
			Digest:   getenv("EVAL_JUDGE_MODEL_DIGEST", os.Getenv("EVAL_MODEL_DIGEST")),
		},
		resolvedSource,
		*platformIdentity,
		*floor,
	)
	if err != nil {
		return err
	}
	if releasePolicy != nil {
		result.Policy = &evals.PolicyEvidence{Version: releasePolicy.PolicyVersion, Digest: releasePolicy.Digest}
	}
	if err := evals.WriteJSONArtifact(resolveFrom(*moduleDir, *output), result); err != nil {
		return err
	}
	if err := writeJSON(stdout, result); err != nil {
		return err
	}
	if !result.Passed() {
		return fmt.Errorf("judge agreement %.3f is below floor %.3f", result.Agreement, result.Floor)
	}
	return nil
}

func assetPaths(moduleDir, dataDir string) evals.AssetPaths {
	if dataDir == "" {
		dataDir = filepath.Join(moduleDir, "..", "agents", "data")
	}
	return evals.AssetPaths{
		ModuleDir: moduleDir, DataDir: dataDir,
		EvalSets: []string{
			filepath.Join(moduleDir, "ops.evalset.json"),
			filepath.Join(moduleDir, "workflow.evalset.json"),
			filepath.Join(moduleDir, "triage-report.evalset.json"),
		},
		Calibration:   filepath.Join(moduleDir, "judge-calibration.json"),
		CostBaseline:  filepath.Join(moduleDir, "cost_baseline.json"),
		Dashboard:     filepath.Join(moduleDir, "grafana-dashboard.json"),
		ReleasePolicy: filepath.Join(moduleDir, "release-policy.json"),
	}
}

func recorder(ctx context.Context, endpoint string) (evals.EvidenceRecorder, error) {
	if endpoint != "" {
		return evals.NewOTLPRecorder(ctx, endpoint)
	}
	return evals.NewNoopRecorder()
}

func gatewayJudgeFromEnvironment() (*evals.GatewayJudge, error) {
	return evals.NewGatewayJudge(evals.GatewayJudgeConfig{
		BaseURL: os.Getenv("EVAL_JUDGE_BASE_URL"),
		Model:   os.Getenv("EVAL_JUDGE_MODEL"),
		APIKey:  os.Getenv("EVAL_JUDGE_API_KEY"),
	})
}

func currentCostIdentity(provider, model, digest string, source evals.SourceEvidence, evalDigest string) (evals.CostIdentity, error) {
	identity := evals.CostIdentity{
		ModelProvider: provider, Model: model, PromptSelection: evals.PromptAuthorityGit,
		EvaluationContractDigest: evalDigest,
		Source:                   source,
	}
	identity.ModelDigest = optionalString(digest)
	identity.OllamaVersion = optionalString(os.Getenv("EVAL_OLLAMA_VERSION"))
	if raw := os.Getenv("EVAL_CONTEXT_LENGTH"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return evals.CostIdentity{}, errors.New("EVAL_CONTEXT_LENGTH must be a positive integer")
		}
		identity.ContextLength = &value
	}
	if raw := os.Getenv("AGENT_MODEL_TEMPERATURE"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 2 {
			return evals.CostIdentity{}, errors.New("AGENT_MODEL_TEMPERATURE must be a finite number between 0 and 2")
		}
		identity.Temperature = &value
	}
	return identity, nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func flagWasSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == name {
			found = true
		}
	})
	return found
}

func detectEvalDir() string {
	if _, err := os.Stat("ops.evalset.json"); err == nil {
		return "."
	}
	return "evals"
}

func resolveFrom(directory, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(directory, path)
}

type sourceIdentity struct {
	Mode       string `json:"mode"`
	Display    string `json:"display"`
	Revision   string `json:"revision"`
	TreeDigest string `json:"tree_digest"`
	Dirty      bool   `json:"dirty"`
	Shallow    bool   `json:"shallow"`
}

type sourceCommand func(context.Context, string, ...string) ([]byte, error)

func resolveSourceIdentity(ctx context.Context, configured, mode string) (evals.SourceEvidence, error) {
	identity, err := readSourceIdentity(ctx, mode)
	if err != nil {
		return evals.SourceEvidence{}, err
	}
	if configured != "" && configured != identity.Display && configured != identity.Revision {
		return evals.SourceEvidence{}, fmt.Errorf(
			"configured source identity %q does not match checkout identity %q",
			configured, identity.Display,
		)
	}
	evidence := evals.SourceEvidence{
		Mode: evals.SourceMode(identity.Mode), Identity: identity.Display, Revision: identity.Revision,
		TreeDigest: identity.TreeDigest, Dirty: identity.Dirty, Shallow: identity.Shallow,
	}
	if err := evidence.Validate(); err != nil {
		return evals.SourceEvidence{}, fmt.Errorf("validate source identity: %w", err)
	}
	return evidence, nil
}

func readSourceIdentity(ctx context.Context, mode string) (sourceIdentity, error) {
	return readSourceIdentityWithCommand(ctx, mode, executeSourceCommand)
}

func readSourceIdentityWithCommand(
	ctx context.Context, mode string, run sourceCommand,
) (sourceIdentity, error) {
	if run == nil {
		return sourceIdentity{}, errors.New("source identity command is unavailable")
	}
	rootOutput, err := run(ctx, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return sourceIdentity{}, fmt.Errorf("locate source checkout: %w", err)
	}
	root := strings.TrimSpace(string(rootOutput))
	if root == "" {
		return sourceIdentity{}, errors.New("locate source checkout: git returned an empty root")
	}
	toolDir := filepath.Join(root, "tools")
	output, err := run(
		ctx, "go", "-C", toolDir, "run", "./cmd/source-identity", "--root", root, "--mode", mode,
	)
	if err != nil {
		return sourceIdentity{}, fmt.Errorf("resolve %s source identity: %w", mode, err)
	}
	var identity sourceIdentity
	if err := json.Unmarshal(output, &identity); err != nil {
		return sourceIdentity{}, fmt.Errorf("decode source identity: %w", err)
	}
	if identity.Display == "" || identity.TreeDigest == "" || identity.Mode != mode {
		return sourceIdentity{}, errors.New("source identity command returned incomplete or inconsistent data")
	}
	return identity, nil
}

func executeSourceCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return output, nil
}

func firstEnvironment(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

type clientFactoryConfig struct {
	Runtime   string
	Process   evals.ProcessClientFactoryConfig
	Container evals.ContainerClientFactoryConfig
}

type clientFactoryConstructors struct {
	process   func(evals.ProcessClientFactoryConfig) (evals.ClientFactory, error)
	container func(evals.ContainerClientFactoryConfig) (evals.ClientFactory, error)
}

func defaultClientFactoryConstructors(ctx context.Context) clientFactoryConstructors {
	return clientFactoryConstructors{
		process: func(config evals.ProcessClientFactoryConfig) (evals.ClientFactory, error) {
			return evals.NewProcessClientFactory(ctx, config)
		},
		container: func(config evals.ContainerClientFactoryConfig) (evals.ClientFactory, error) {
			return evals.NewContainerClientFactory(ctx, config)
		},
	}
}

func selectClientFactory(
	config clientFactoryConfig, constructors clientFactoryConstructors,
) (evals.ClientFactory, error) {
	switch config.Runtime {
	case "process":
		if constructors.process == nil {
			return nil, errors.New("process runtime constructor is unavailable")
		}
		return constructors.process(config.Process)
	case "container":
		if constructors.container == nil {
			return nil, errors.New("container runtime constructor is unavailable")
		}
		return constructors.container(config.Container)
	default:
		return nil, fmt.Errorf("unsupported agent runtime %q; use process or container", config.Runtime)
	}
}

func agentRuntimeEnvironment() map[string]string {
	environment := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "AGENT_") {
			environment[key] = value
			continue
		}
		switch key {
		case "OPENAI_BASE_URL", "OPENAI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_USE_ENTERPRISE",
			"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION":
			environment[key] = value
		}
	}
	return environment
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	if value == "" {
		return errors.New("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}
