package conventions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSourceVersionFixture(t *testing.T, root string) pageSet {
	t.Helper()
	files := map[string]string{
		"mise.toml": `[tools]
go = "1.26.5"
k3d = "1.0.0"
kubectl = "1.0.0"
helm = "1.0.0"
helmfile = "1.0.0"
skaffold = "1.0.0"
kubeconform = "1.0.0"
kube-linter = "1.0.0"
"github:agentgateway/agentgateway" = "1.4.1"

[tasks."load:health"]
run = "mise x k6@2.1.0 -- k6 run load/health.js"

[tasks."load:mcp"]
run = "mise x k6@2.1.0 -- k6 run load/mcp-read.js"

[tasks."load:a2a"]
run = "mise x k6@2.1.0 -- k6 run load/a2a-send.js"
`,
		"agents/go/go.mod":             "module example.test/agent\n\ngo 1.26.5\n",
		"evals/go.mod":                 "module example.test/evals\n\ngo 1.26.5\n",
		"tools/go.mod":                 "module example.test/tools\n\ngo 1.26.5\n",
		"agents/go/Dockerfile":         "FROM golang:1.26.5-alpine@sha256:" + strings.Repeat("a", 64) + " AS build\n",
		"infra/helmfile.yaml":          "# kagent-chart-version: 0.9.12\n",
		"scripts/install-helm-diff.sh": "readonly expected=3.15.10\n",
	}
	for path, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return pageSet{
		"content/1. Setup/1.3. Kubernetes.md": `| k3d | 1.0.0 | x |
| kubectl | 1.0.0 | x |
| helm | 1.0.0 | x |
| helmfile | 1.0.0 | x |
| skaffold | 1.0.0 | x |
| kubeconform | 1.0.0 | x |
| kube-linter | 1.0.0 | x |
| agentgateway | 1.4.1 | x |
helm-diff 3.15.10 ready
The helm-diff plugin at 3.15.10 is pinned.
`,
		"content/6. Platform/6.0. Platform.md":         "The pinned stable chart `0.9.12` is reviewed.\n",
		"content/6. Platform/6.2. Platform Install.md": "helm-diff 3.15.10 ready\nAt exactly `0.9.12`.\nAt exactly `0.9.12`.\n",
		"content/7. Observability/7.2. Monitoring.md":  strings.Repeat("mise x k6@2.1.0 -- k6 run load/health.js\n", 4),
	}
}

func TestCheckSourceVersionsRequiresGoParityAcrossEveryExecutableModule(t *testing.T) {
	tests := []struct {
		where   string
		content string
	}{
		{where: "agents/go/go.mod", content: "module example.test/agent\n\ngo 1.26.4\n"},
		{where: "evals/go.mod", content: "module example.test/evals\n\ngo 1.26.4\n"},
		{where: "tools/go.mod", content: "module example.test/tools\n\ngo 1.26.4\n"},
		{where: "agents/go/Dockerfile", content: "FROM golang:1.26.4-alpine@sha256:" + strings.Repeat("a", 64) + " AS build\n"},
	}
	for _, test := range tests {
		t.Run(test.where, func(t *testing.T) {
			root := t.TempDir()
			pages := writeSourceVersionFixture(t, root)
			path := filepath.Join(root, filepath.FromSlash(test.where))
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			problems := checkSourceVersions(root, pages)
			found := false
			for _, item := range problems {
				found = found || item.Where == test.where && strings.Contains(item.Message, "Go ") && strings.Contains(item.Message, "version")
			}
			if !found {
				t.Fatalf("problems = %#v", problems)
			}
		})
	}
}

func TestCheckSourceVersionsRejectsAuthorityMutationsForCopiedPins(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		old         string
		new         string
		wantMessage string
	}{
		{name: "kagent chart", path: "infra/helmfile.yaml", old: "0.9.12", new: "9.9.9", wantMessage: "kagent chart version drifted"},
		{name: "helm diff", path: "scripts/install-helm-diff.sh", old: "3.15.10", new: "9.9.9", wantMessage: "helm-diff version drifted"},
		{name: "k6", path: "mise.toml", old: "2.1.0", new: "9.9.9", wantMessage: "k6 version drifted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			pages := writeSourceVersionFixture(t, root)
			path := filepath.Join(root, filepath.FromSlash(test.path))
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mutated := strings.ReplaceAll(string(content), test.old, test.new)
			if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
				t.Fatal(err)
			}
			messages := problemMessages(checkSourceVersions(root, pages))
			if !strings.Contains(messages, test.wantMessage) {
				t.Fatalf("problems = %s", messages)
			}
		})
	}
}

func TestCheckSourceVersionsRejectsCopiedPinInventoryDrift(t *testing.T) {
	root := t.TempDir()
	pages := writeSourceVersionFixture(t, root)
	pages["content/8. Community/8.6. AAIF.md"] = "Use mise x k6@2.1.0 -- k6 run load/health.js.\n"
	messages := problemMessages(checkSourceVersions(root, pages))
	if !strings.Contains(messages, "k6 copy inventory drifted") {
		t.Fatalf("problems = %s", messages)
	}
}

func TestCheckEvalReleaseOrchestrationRequiresOneSharedModelRun(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"evals/mise.toml": `[tasks.eval]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --release-policy release-policy.json --judge --cost-output cost-observed.json"

[tasks."eval:policy-trial"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --repeat 3 --calibration-policy release-policy.json --judge"

[tasks."eval:a2a"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --release-policy release-policy.json --judge"

[tasks."eval:a2a:policy-trial"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --repeat 3 --calibration-policy release-policy.json --judge"

[tasks."eval:dev"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --repeat 3 --min-pass-rate 0.33"

[tasks."eval:workflow"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --repeat 3 --min-pass-rate 0.33"

[tasks."eval:report"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run"

[tasks."eval:cost"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run"

[tasks."eval:cost:observe"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run"

[tasks."eval:ground"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run"

[tasks."eval:judge-calibration"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval calibrate --release-policy release-policy.json"

[tasks."eval:judge-calibration:trial"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval calibrate --floor 0.75"

[tasks."eval:retrieval"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval retrieval"

[tasks."eval:ab"]
usage = '''
flag "--baseline <path>"
flag "--candidate <path>"
'''
run = '''
go run ./cmd/agentops-eval compare \
  --baseline "$usage_baseline" \
  --candidate "$usage_candidate" \
  --require-distinct-source
'''
`,
		"evals/release-policy.json": `{}`,
		"mise.toml": `[tasks."eval:ab"]
usage = '''
flag "--baseline <path>"
flag "--candidate <path>"
'''
run = '''
cd evals
mise run eval:ab -- \
  --baseline "$usage_baseline" \
  --candidate "$usage_candidate"
'''
`,
		".github/workflows/eval.yml": `steps:
  - name: Run canonical trajectory evaluation and capture cost evidence
    run: mise run eval
`,
	}
	for path, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if problems := checkEvalReleaseOrchestration(root); len(problems) != 0 {
		t.Fatalf("valid release orchestration problems = %#v", problems)
	}

	workflowPath := filepath.Join(root, ".github", "workflows", "eval.yml")
	broken := `steps:
  - name: Run canonical trajectory evaluation
    run: mise run eval
  - name: Capture cost evidence in a different run
    run: mise run eval:cost:observe
`
	if err := os.WriteFile(workflowPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	messages := problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "same model run") {
		t.Fatalf("broken release orchestration problems = %s", messages)
	}

	validWorkflow := files[".github/workflows/eval.yml"]
	if err := os.WriteFile(workflowPath, []byte(validWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "evals", "mise.toml")
	missingPolicy := strings.Replace(files["evals/mise.toml"], " --release-policy release-policy.json", "", 1)
	if err := os.WriteFile(manifestPath, []byte(missingPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "release-policy") {
		t.Fatalf("missing release policy problems = %s", messages)
	}

	weakenedDevelopment := strings.Replace(files["evals/mise.toml"], "--repeat 3 --min-pass-rate 0.33", "--repeat 1 --min-pass-rate 0.33", 1)
	if err := os.WriteFile(manifestPath, []byte(weakenedDevelopment), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "eval:dev") {
		t.Fatalf("weakened development policy problems = %s", messages)
	}

	overwritingCostTask := strings.Replace(
		files["evals/mise.toml"],
		"[tasks.\"eval:cost\"]\nenv = { _.file = { path = \"../.env\", redact = true } }\nrun = \"go run ./cmd/agentops-eval run\"",
		"[tasks.\"eval:cost\"]\nenv = { _.file = { path = \"../.env\", redact = true } }\nrun = \"go run ./cmd/agentops-eval run --output cost-results.json --cost-output cost-observed.json\"",
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(overwritingCostTask), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "must not overwrite") {
		t.Fatalf("overwriting cost task problems = %s", messages)
	}

	if err := os.WriteFile(manifestPath, []byte(files["evals/mise.toml"]), 0o600); err != nil {
		t.Fatal(err)
	}
	missingDotenv := strings.Replace(files["evals/mise.toml"], "env = { _.file = { path = \"../.env\", redact = true } }\n", "", 1)
	if err := os.WriteFile(manifestPath, []byte(missingDotenv), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "redacted root .env") {
		t.Fatalf("missing dotenv problems = %s", messages)
	}

	brokenWorkflow := strings.Replace(
		files["evals/mise.toml"],
		`[tasks."eval:workflow"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run --repeat 3 --min-pass-rate 0.33"`,
		`[tasks."eval:workflow"]
env = { _.file = { path = "../.env", redact = true } }
run = "go run ./cmd/agentops-eval run"`,
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(brokenWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "three samples") {
		t.Fatalf("workflow sampling problems = %s", messages)
	}

	fixedPromptArtifacts := strings.Replace(
		files["evals/mise.toml"],
		`--baseline "$usage_baseline"`,
		"--baseline prompt-baseline-results.json",
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(fixedPromptArtifacts), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "explicit prompt artifacts") {
		t.Fatalf("fixed prompt artifact problems = %s", messages)
	}

	if err := os.WriteFile(manifestPath, []byte(files["evals/mise.toml"]), 0o600); err != nil {
		t.Fatal(err)
	}
	rootManifestPath := filepath.Join(root, "mise.toml")
	brokenForwarding := strings.Replace(files["mise.toml"], `flag "--candidate <path>"`, "", 1)
	if err := os.WriteFile(rootManifestPath, []byte(brokenForwarding), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "forward explicit prompt artifacts") {
		t.Fatalf("broken root prompt forwarding problems = %s", messages)
	}
	if err := os.WriteFile(rootManifestPath, []byte(files["mise.toml"]), 0o600); err != nil {
		t.Fatal(err)
	}

	fixedRetrievalArtifacts := strings.Replace(
		files["evals/mise.toml"],
		"agentops-eval retrieval",
		"agentops-eval compare --baseline retrieval-keyword-results.json --candidate retrieval-semantic-results.json",
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(fixedRetrievalArtifacts), 0o600); err != nil {
		t.Fatal(err)
	}
	messages = problemMessages(checkEvalReleaseOrchestration(root))
	if !strings.Contains(messages, "end-to-end retrieval") {
		t.Fatalf("fixed retrieval artifact problems = %s", messages)
	}
}

func TestCheckOfflineModuleChecksRejectsNetworkedVulnerabilityDependencies(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"agents/go/mise.toml", "evals/mise.toml", "tools/mise.toml"} {
		content := "[tasks.check]\ndepends = [\"check:format\", \"check:vuln\"]\n"
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	messages := problemMessages(checkOfflineModuleChecks(root))
	if strings.Count(messages, "check:vuln") != 3 {
		t.Fatalf("offline module check problems = %s", messages)
	}
}

func TestCheckReadOnlyInstallsRejectsDependencyUpdates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"mise.toml": `[tasks.install]
run = ["hugo mod get", "GOFLAGS=-mod=readonly go mod download", "gcloud components install gke-gcloud-auth-plugin", "command -v gke-gcloud-auth-plugin", "mise install go"]
`,
		"agents/go/mise.toml": `[tasks.install]
run = "go mod tidy"
`,
		"evals/mise.toml": `[tasks.install]
run = "go mod download"
`,
		"tools/mise.toml": `[tasks.install]
run = "GOFLAGS=-mod=readonly go mod download"
`,
		".github/workflows/docs.yml": `jobs:
  docs:
    steps:
      - run: hugo mod get
      - run: mise install hugo-extended
`,
		".github/workflows/eval.yml": `jobs:
  eval:
    steps:
      - run: mise install go
`,
	}
	for where, content := range files {
		path := filepath.Join(root, filepath.FromSlash(where))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	problems := checkReadOnlyInstalls(root)
	messages := problemMessages(problems)
	for _, want := range []string{"hugo mod get", "go mod tidy", "gcloud components install", "ambient GKE auth plugin", "-mod=readonly", "--locked", "-y"} {
		if !strings.Contains(messages, want) {
			t.Errorf("problems = %s, want %q", messages, want)
		}
	}
	if !slices.ContainsFunc(problems, func(item Problem) bool {
		return item.Where == ".github/workflows/eval.yml" && strings.Contains(item.Message, "--locked")
	}) {
		t.Errorf("problems = %#v, want eval workflow locked-install problem", problems)
	}
}

func TestCheckHugoExtendedToolRejectsTheStandardBinary(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	files := map[string]string{
		"mise.toml": `[tools]
hugo = "0.164.0"
[tasks.install]
run = "mise install hugo go"
`,
		".github/workflows/docs.yml": `jobs:
  docs:
    steps:
      - run: mise install hugo go
`,
	}
	for where, content := range files {
		path := filepath.Join(root, filepath.FromSlash(where))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	messages := problemMessages(checkHugoExtendedTool(root))
	for _, want := range []string{"Hugo Extended", "standard Hugo", "hugo-extended"} {
		if !strings.Contains(messages, want) {
			t.Errorf("problems = %s, want %q", messages, want)
		}
	}
}

func TestCheckLocalActionsRequireAnEarlierCheckout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  missing:
    steps:
      - uses: ./.github/actions/setup-buildx
  late:
    steps:
      - uses: ./.github/actions/setup-buildx
      - uses: actions/checkout@0123456789abcdef
  valid:
    steps:
      - uses: actions/checkout@0123456789abcdef
      - uses: ./.github/actions/setup-buildx
`
	if err := os.WriteFile(filepath.Join(directory, "release.yml"), []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}

	messages := problemMessages(checkLocalActionCheckouts(root))
	if !strings.Contains(messages, `job "missing"`) || !strings.Contains(messages, `job "late"`) {
		t.Fatalf("local-action checkout problems = %s", messages)
	}
	if strings.Contains(messages, `job "valid"`) {
		t.Fatalf("valid local-action job was rejected: %s", messages)
	}
}

func TestCheckWorkflowShellExpressionsRejectsDirectMatrixInterpolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	workflow := `jobs:
  unsafe:
    steps:
      - run: docker build --file "${{ matrix.dockerfile }}" .
  safe:
    steps:
      - env:
          DOCKERFILE: ${{ matrix.dockerfile }}
        run: docker build --file "$DOCKERFILE" .
`
	if err := os.WriteFile(filepath.Join(directory, "scan.yml"), []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}

	messages := problemMessages(checkWorkflowShellExpressions(root))
	if !strings.Contains(messages, `job "unsafe"`) || !strings.Contains(messages, "matrix context directly in shell") {
		t.Fatalf("workflow shell-expression problems = %s", messages)
	}
	if strings.Contains(messages, `job "safe"`) {
		t.Fatalf("env-mediated workflow step was rejected: %s", messages)
	}
}

func TestCheckNestedMiseTrustRequiresEveryModuleConfig(t *testing.T) {
	t.Parallel()

	const where = "content/1. Setup/1.0. System.md"
	rootOnly := problemMessages(checkNestedMiseTrust(pageSet{where: "mise trust mise.toml\n"}))
	if !strings.Contains(rootOnly, "agents/go/mise.toml") {
		t.Fatalf("root-only trust problems = %s, want missing nested config", rootOnly)
	}

	broad := problemMessages(checkNestedMiseTrust(pageSet{where: "mise trust --all\n"}))
	if !strings.Contains(broad, "must not use mise trust --all") {
		t.Fatalf("broad trust problems = %s, want scope refusal", broad)
	}

	explicit := strings.Join([]string{
		"mise trust mise.toml",
		"mise trust agents/go/mise.toml",
		"mise trust agents/data/mise.toml",
		"mise trust evals/mise.toml",
		"mise trust tools/mise.toml",
	}, "\n")
	if problems := checkNestedMiseTrust(pageSet{where: explicit}); len(problems) != 0 {
		t.Fatalf("explicit trust problems = %#v, want none", problems)
	}
}

func TestCheckWorkflowShellExpressionsRejectsDirectActionInputInterpolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]string{
		"unsafe": `runs:
  using: composite
  steps:
    - shell: bash
      run: scan '${{ inputs.image }}'
`,
		"safe": `runs:
  using: composite
  steps:
    - shell: bash
      env:
        IMAGE: ${{ inputs.image }}
      run: scan "$IMAGE"
`,
	} {
		path := filepath.Join(root, ".github", "actions", name, "action.yml")
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(action), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	problems := checkWorkflowShellExpressions(root)
	if len(problems) != 1 || problems[0].Where != ".github/actions/unsafe/action.yml" ||
		!strings.Contains(problems[0].Message, "action input directly in shell") {
		t.Fatalf("action shell-expression problems = %#v", problems)
	}
}

func TestCheckReleaseQualificationPrivacyRejectsPersistedWorkflowURL(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "release.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("workflows:\n  - url: $run.html_url\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	messages := problemMessages(checkReleaseQualificationPrivacy(root))
	if !strings.Contains(messages, "persist workflow URLs") {
		t.Fatalf("release qualification privacy problems = %s", messages)
	}
}

func TestCheckSerializedModuleChecksRejectsParallelLinterDependencies(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "mise.toml")
	parallel := `[tasks."check:source"]
depends = ["check:go", "check:evals", "check:tools"]
`
	if err := os.WriteFile(manifest, []byte(parallel), 0o600); err != nil {
		t.Fatal(err)
	}
	if messages := problemMessages(checkSerializedModuleChecks(root)); !strings.Contains(messages, "serialize") {
		t.Fatalf("parallel module check problems = %s", messages)
	}

	serialized := `[tasks."check:source"]
depends = ["check:modules"]

[tasks."check:modules"]
run = ["mise run check:go", "mise run check:evals", "mise run check:tools"]
`
	if err := os.WriteFile(manifest, []byte(serialized), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := checkSerializedModuleChecks(root); len(problems) != 0 {
		t.Fatalf("serialized module check problems = %#v", problems)
	}
}

func TestCheckLiteralRepositoryPathsRejectsMissingExecutableReferences(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"agents/go/compose/skills_test.go", "evals/client.go"} {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pages := pageSet{
		"content/example.md": "Read `agents/go/compose/skills_test.go`, not `agents/go/tests/test_compose/skills.go`.\nThe evaluator lives in `evals/client.go`, not `evals/transport_a2a.go`.\n```bash\nsed -n '1,20p' agents/go/compose/skills_test.go\nsed -n '1,20p' evals/fenced_missing.go\nsed -n '1,20p' ./evals/relative_fenced_missing.go\n```\n",
	}
	messages := problemMessages(checkLiteralRepositoryPaths(root, pages))
	for _, missing := range []string{"agents/go/tests/test_compose/skills.go", "evals/transport_a2a.go", "evals/fenced_missing.go", "evals/relative_fenced_missing.go"} {
		if !strings.Contains(messages, missing) {
			t.Errorf("missing path %q was accepted: %s", missing, messages)
		}
	}
	for _, existing := range []string{"agents/go/compose/skills_test.go", "evals/client.go"} {
		if strings.Contains(messages, existing) {
			t.Errorf("existing path %q was rejected: %s", existing, messages)
		}
	}
}

func TestCheckDocumentedGoPackagesUsesTheBlockWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"agents/go/compose/compose.go", "evals/client.go"} {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pages := pageSet{
		"content/example.md": "Inline: `cd agents/go && go test ./inline-missing`.\n```bash\ncd agents/go\ngo test ./compose ./sessiondb tests/test_compose/skills.go -run Skill -count=1\ncd ../../evals\ngo test ./...\n```\n",
	}
	messages := problemMessages(checkDocumentedGoPackages(root, pages))
	for _, missing := range []string{"./inline-missing", "./sessiondb", "tests/test_compose/skills.go"} {
		if !strings.Contains(messages, missing) {
			t.Errorf("missing Go argument %q was accepted: %s", missing, messages)
		}
	}
	if strings.Contains(messages, "./compose") || strings.Contains(messages, "./...") {
		t.Fatalf("valid Go packages were rejected: %s", messages)
	}
}

func TestCheckNoPagesDeploymentRejectsPagesActionsAndAuthority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows", "docs.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	pages := `jobs:
  build:
    steps:
      - uses: actions/configure-pages@0123456789abcdef
      - uses: actions/upload-pages-artifact@0123456789abcdef
  deploy:
    permissions:
      pages: write
      id-token: write
    environment:
      name: github-pages
    steps:
      - uses: actions/deploy-pages@0123456789abcdef
`
	if err := os.WriteFile(path, []byte(pages), 0o600); err != nil {
		t.Fatal(err)
	}
	messages := problemMessages(checkNoPagesDeployment(root))
	for _, want := range []string{"build", "deploy", "Pages deployment"} {
		if !strings.Contains(messages, want) {
			t.Errorf("Pages deployment problems = %s, want %q", messages, want)
		}
	}

	validationOnly := `jobs:
  build:
    permissions:
      contents: read
    steps:
      - run: mise run check:docs
  accessibility:
    steps:
      - run: mise run check:accessibility
`
	if err := os.WriteFile(path, []byte(validationOnly), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := checkNoPagesDeployment(root); len(problems) != 0 {
		t.Fatalf("validation-only docs workflow problems = %#v", problems)
	}
}
