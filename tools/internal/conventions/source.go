package conventions

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml"
	"gopkg.in/yaml.v3"
)

var sourceSnippetSurfaces = map[string][2]string{
	"Go module":  {"content/1. Setup/1.1. Go.md", "agents/go/go.mod:runtime-dependencies"},
	"YAML":       {"content/5. Gateway/5.0. Gateway.md", "infra/agentgateway/host/config.yaml:host-mcp-gateway"},
	"shell":      {"content/5. Gateway/5.1. Gateway Setup.md", "infra/scripts/gateway-host.sh:hardened-container-args"},
	"Dockerfile": {"content/6. Platform/6.1. Containers.md", "agents/go/Dockerfile:runtime-base"},
	"Helm":       {"content/6. Platform/6.2. Platform Install.md", "infra/helmfile.yaml:kagent-crds-release"},
	"workflow":   {"content/8. Community/8.2. Releases.md", ".github/workflows/release.yml:release-dispatch-authority"},
}

// CheckCopiedSources runs the source-derived subset used by the scheduled
// freshness report. It deliberately excludes page-shape and rendered checks.
func CheckCopiedSources(root string) []Problem {
	pages, err := loadPages(root)
	if err != nil {
		return []Problem{problem("content", "could not read Markdown pages: %v", err)}
	}
	problems := make([]Problem, 0, 4)
	problems = append(problems, checkDocumentedTasks(root, pages)...)
	problems = append(problems, checkTaskExpansions(root, pages)...)
	problems = append(problems, checkSourceVersions(root, pages)...)
	problems = append(problems, checkPortContracts(root, pages)...)
	return problems
}

func checkSourceSnippetCoverage(pages pageSet) []Problem {
	var problems []Problem
	for formatName, surface := range sourceSnippetSurfaces {
		source, region, _ := strings.Cut(surface[1], ":")
		wanted := fmt.Sprintf(`{{< include path="%s" region="%s"`, source, region)
		if !strings.Contains(pages[surface[0]], wanted) {
			problems = append(problems, problem(surface[0], "source-backed %s example must include %q", formatName, surface[1]))
		}
	}
	return problems
}

func loadTOML(path string) (*toml.Tree, error) {
	return toml.LoadFile(path)
}

func taskNames(path string) map[string]bool {
	document, err := loadTOML(path)
	if err != nil {
		return nil
	}
	tasks, ok := document.Get("tasks").(*toml.Tree)
	if !ok {
		return nil
	}
	result := make(map[string]bool)
	for _, key := range tasks.Keys() {
		result[key] = true
	}
	return result
}

func scalarString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		return "", false
	}
}

func taskExpansion(path, name string) string {
	document, err := loadTOML(path)
	if err != nil {
		return ""
	}
	task, ok := document.GetPath([]string{"tasks", name}).(*toml.Tree)
	if !ok {
		return ""
	}
	run, ok := task.Get("run").(string)
	if !ok {
		return ""
	}
	var prefixes []string
	if environment, ok := task.Get("env").(*toml.Tree); ok {
		keys := environment.Keys()
		slices.Sort(keys)
		for _, key := range keys {
			if key != strings.ToUpper(key) {
				continue
			}
			if value, scalar := scalarString(environment.Get(key)); scalar {
				prefixes = append(prefixes, key+"="+value)
			}
		}
	}
	return strings.Join(append(prefixes, run), " ")
}

func taskDependencies(path, name string) []string {
	document, err := loadTOML(path)
	if err != nil {
		return nil
	}
	task, ok := document.GetPath([]string{"tasks", name}).(*toml.Tree)
	if !ok {
		return nil
	}
	switch values := task.Get("depends").(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if dependency, stringValue := value.(string); stringValue {
				result = append(result, dependency)
			}
		}
		return result
	default:
		return nil
	}
}

func taskCommands(path, name string) []string {
	document, err := loadTOML(path)
	if err != nil {
		return nil
	}
	task, ok := document.GetPath([]string{"tasks", name}).(*toml.Tree)
	if !ok {
		return nil
	}
	switch values := task.Get("run").(type) {
	case string:
		return []string{values}
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if command, stringValue := value.(string); stringValue {
				result = append(result, command)
			}
		}
		return result
	default:
		return nil
	}
}

func checkSerializedModuleChecks(root string) []Problem {
	manifest := filepath.Join(root, "mise.toml")
	sourceDependencies := taskDependencies(manifest, "check:source")
	for _, direct := range []string{"check:go", "check:evals", "check:tools"} {
		if slices.Contains(sourceDependencies, direct) {
			return []Problem{problem("mise.toml", "check:source must serialize Go module linters through check:modules")}
		}
	}
	if !slices.Contains(sourceDependencies, "check:modules") {
		return []Problem{problem("mise.toml", "check:source must depend on serialized check:modules")}
	}
	wanted := []string{"mise run check:go", "mise run check:evals", "mise run check:tools"}
	if !slices.Equal(taskCommands(manifest, "check:modules"), wanted) {
		return []Problem{problem("mise.toml", "check:modules must run the three Go module checks serially")}
	}
	return nil
}

func checkOfflineModuleChecks(root string) []Problem {
	var problems []Problem
	for _, where := range []string{"agents/go/mise.toml", "evals/mise.toml", "tools/mise.toml"} {
		dependencies := taskDependencies(filepath.Join(root, filepath.FromSlash(where)), "check")
		if slices.Contains(dependencies, "check:vuln") {
			problems = append(problems, problem(where, "offline module check must not depend on networked check:vuln"))
		}
	}
	return problems
}

func checkReadOnlyInstalls(root string) []Problem {
	var problems []Problem
	updateBearing := regexp.MustCompile(`\b(?:hugo mod get|go mod tidy|go get|gcloud\s+components\s+install)\b`)
	for _, where := range []string{"mise.toml", "agents/go/mise.toml", "evals/mise.toml", "tools/mise.toml"} {
		path := filepath.Join(root, filepath.FromSlash(where))
		for name := range taskNames(path) {
			if name != "install" && !strings.HasPrefix(name, "install:") {
				continue
			}
			for _, command := range taskCommands(path, name) {
				problems = append(problems, checkMiseInstallFlags(where, fmt.Sprintf("install task %q", name), command)...)
				if strings.Contains(command, "command -v gke-gcloud-auth-plugin") {
					problems = append(problems, problem(where,
						"install task %q accepts an ambient GKE auth plugin as installation proof", name))
				}
				if found := updateBearing.FindString(command); found != "" {
					problems = append(problems, problem(where, "install task %q runs update-bearing %q", name, found))
				}
				if strings.Contains(command, "go mod download") && !strings.Contains(command, "GOFLAGS=-mod=readonly") {
					problems = append(problems, problem(where, "install task %q must run go mod download with GOFLAGS=-mod=readonly", name))
				}
			}
		}
	}
	problems = append(problems, checkAutomationMiseInstallFlags(root)...)

	const docsWorkflow = ".github/workflows/docs.yml"
	docs, err := readFile(filepath.Join(root, filepath.FromSlash(docsWorkflow)))
	if err != nil {
		return append(problems, problem(docsWorkflow, "could not read documentation install contract: %v", err))
	}
	for _, found := range updateBearing.FindAllString(docs, -1) {
		problems = append(problems, problem(docsWorkflow, "documentation install runs update-bearing %q", found))
	}
	for _, line := range splitLines(docs) {
		if strings.Contains(line, "go mod download") && !strings.Contains(line, "GOFLAGS=-mod=readonly") {
			problems = append(problems, problem(docsWorkflow, "documentation install must run go mod download with GOFLAGS=-mod=readonly"))
		}
	}
	return problems
}

func checkNestedMiseTrust(pages pageSet) []Problem {
	const where = "content/1. Setup/1.0. System.md"
	page := pages[where]
	var problems []Problem
	if strings.Contains(page, "mise trust --all") {
		problems = append(problems, problem(where,
			"clean-clone setup must not use mise trust --all because it walks parents and unrelated nested configs"))
	}
	for _, config := range []string{
		"mise.toml",
		"agents/go/mise.toml",
		"agents/data/mise.toml",
		"evals/mise.toml",
		"tools/mise.toml",
	} {
		command := "mise trust " + config
		if !strings.Contains(page, command) {
			problems = append(problems, problem(where,
				"clean-clone setup must trust the reviewed config explicitly with %q", command))
		}
	}
	return problems
}

func checkMiseInstallFlags(where, subject, command string) []Problem {
	fields := strings.Fields(command)
	for index := 0; index+1 < len(fields); index++ {
		if fields[index] != "mise" || fields[index+1] != "install" {
			continue
		}

		arguments := fields[index+2:]
		var problems []Problem
		if !slices.Contains(arguments, "--locked") {
			problems = append(problems, problem(where, "%s must pass --locked to mise install", subject))
		}
		if !slices.Contains(arguments, "-y") && !slices.Contains(arguments, "--yes") {
			problems = append(problems, problem(where, "%s must pass -y or --yes to mise install", subject))
		}
		return problems
	}
	return nil
}

func checkAutomationMiseInstallFlags(root string) []Problem {
	var problems []Problem
	err := filepath.WalkDir(filepath.Join(root, ".github"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		where := relative(root, path)
		workflow := strings.HasPrefix(where, ".github/workflows/") && slices.Contains([]string{".yml", ".yaml"}, filepath.Ext(path))
		action := strings.HasPrefix(where, ".github/actions/") && slices.Contains([]string{"action.yml", "action.yaml"}, filepath.Base(path))
		if !workflow && !action {
			return nil
		}

		content, err := readFile(path)
		if err != nil {
			return err
		}
		for _, line := range splitLines(content) {
			problems = append(problems, checkMiseInstallFlags(where, "automation install", line)...)
		}
		return nil
	})
	if err != nil {
		problems = append(problems, problem(".github", "could not inspect automation installs: %v", err))
	}
	return problems
}

func checkHugoExtendedTool(root string) []Problem {
	var problems []Problem
	if toolVersion(root, "hugo-extended") == "" {
		problems = append(problems, problem("mise.toml", "Hugo Extended must be pinned as the hugo-extended tool"))
	}
	if toolVersion(root, "hugo") != "" {
		problems = append(problems, problem("mise.toml", "the standard Hugo tool cannot satisfy the documented Hugo Extended contract"))
	}
	for _, where := range []string{"mise.toml", ".github/workflows/docs.yml"} {
		text, err := readFile(filepath.Join(root, filepath.FromSlash(where)))
		if err != nil {
			problems = append(problems, problem(where, "could not read the Hugo install contract: %v", err))
			continue
		}
		for _, line := range splitLines(text) {
			fields := strings.Fields(line)
			for index := 0; index+2 < len(fields); index++ {
				if fields[index] == "mise" && fields[index+1] == "install" && fields[index+2] == "hugo" {
					problems = append(problems, problem(where, "installs standard Hugo instead of hugo-extended"))
				}
			}
		}
	}
	return problems
}

func checkLocalActionCheckouts(root string) []Problem {
	directory := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return []Problem{problem(".github/workflows", "could not read workflows: %v", err)}
	}
	var problems []Problem
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}
		where := filepath.ToSlash(filepath.Join(".github", "workflows", entry.Name()))
		content, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			problems = append(problems, problem(where, "could not read workflow: %v", readErr))
			continue
		}
		var workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Uses string `yaml:"uses"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if parseErr := yaml.Unmarshal(content, &workflow); parseErr != nil {
			problems = append(problems, problem(where, "could not parse workflow: %v", parseErr))
			continue
		}
		for name, job := range workflow.Jobs {
			checkedOut := false
			for _, step := range job.Steps {
				if strings.HasPrefix(step.Uses, "actions/checkout@") {
					checkedOut = true
				}
				if strings.HasPrefix(step.Uses, "./") && !checkedOut {
					problems = append(problems, problem(where,
						"job %q uses repository-local action %q before actions/checkout", name, step.Uses))
					break
				}
			}
		}
	}
	return problems
}

func checkNoPagesDeployment(root string) []Problem {
	const where = ".github/workflows/docs.yml"
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(where)))
	if err != nil {
		return []Problem{problem(where, "could not read documentation workflow: %v", err)}
	}
	var workflow struct {
		Jobs map[string]struct {
			Environment any               `yaml:"environment"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return []Problem{problem(where, "could not parse documentation workflow: %v", err)}
	}
	var problems []Problem
	for name, job := range workflow.Jobs {
		pagesAuthority := job.Permissions["pages"] != "" || pagesEnvironment(job.Environment)
		for _, step := range job.Steps {
			for _, action := range []string{
				"actions/configure-pages@",
				"actions/upload-pages-artifact@",
				"actions/deploy-pages@",
			} {
				pagesAuthority = pagesAuthority || strings.HasPrefix(step.Uses, action)
			}
		}
		if pagesAuthority {
			problems = append(problems, problem(where,
				"job %q retains Pages deployment actions or authority; this workflow must validate only", name))
		}
	}
	return problems
}

func pagesEnvironment(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "github-pages"
	case map[string]any:
		name, _ := typed["name"].(string)
		return name == "github-pages"
	default:
		return false
	}
}

func checkWorkflowShellExpressions(root string) []Problem {
	directory := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return []Problem{problem(".github/workflows", "could not read workflows: %v", err)}
	}
	var problems []Problem
	directMatrix := regexp.MustCompile(`\$\{\{\s*matrix\.`)
	for _, entry := range entries {
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yml" && filepath.Ext(entry.Name()) != ".yaml") {
			continue
		}
		where := filepath.ToSlash(filepath.Join(".github", "workflows", entry.Name()))
		content, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			problems = append(problems, problem(where, "could not read workflow: %v", readErr))
			continue
		}
		var workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Run string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if parseErr := yaml.Unmarshal(content, &workflow); parseErr != nil {
			problems = append(problems, problem(where, "could not parse workflow: %v", parseErr))
			continue
		}
		for name, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if directMatrix.MatchString(step.Run) {
					problems = append(problems, problem(where,
						"job %q interpolates matrix context directly in shell; pass it through env", name))
				}
			}
		}
	}

	actionsRoot := filepath.Join(root, ".github", "actions")
	directInput := regexp.MustCompile(`\$\{\{\s*inputs\.`)
	actions, openErr := os.OpenRoot(actionsRoot)
	if os.IsNotExist(openErr) {
		return problems
	}
	if openErr != nil {
		return append(problems, problem(".github/actions", "could not read actions: %v", openErr))
	}
	walkErr := fs.WalkDir(actions.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (entry.Name() != "action.yml" && entry.Name() != "action.yaml") {
			return nil
		}
		where := filepath.ToSlash(filepath.Join(".github", "actions", path))
		content, readErr := actions.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var action struct {
			Runs struct {
				Steps []struct {
					Run string `yaml:"run"`
				} `yaml:"steps"`
			} `yaml:"runs"`
		}
		if parseErr := yaml.Unmarshal(content, &action); parseErr != nil {
			problems = append(problems, problem(where, "could not parse action: %v", parseErr))
			return nil
		}
		for _, step := range action.Runs.Steps {
			if directInput.MatchString(step.Run) {
				problems = append(problems, problem(where,
					"composite action interpolates an action input directly in shell; pass it through env"))
			}
		}
		return nil
	})
	if walkErr != nil {
		problems = append(problems, problem(".github/actions", "could not read actions: %v", walkErr))
	}
	if closeErr := actions.Close(); closeErr != nil {
		problems = append(problems, problem(".github/actions", "could not close actions root: %v", closeErr))
	}
	return problems
}

func checkReleaseQualificationPrivacy(root string) []Problem {
	workflow, err := readFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		return []Problem{problem(".github/workflows/release.yml", "could not read release qualification privacy contract: %v", err)}
	}
	persistedURL := regexp.MustCompile(`(?m)^\s+(?:-\s+)?url:\s+\$run\.html_url\s*$`)
	if persistedURL.MatchString(workflow) {
		return []Problem{problem(".github/workflows/release.yml", "release qualification must not persist workflow URLs")}
	}
	return nil
}

func checkDocumentedTasks(root string, pages pageSet) []Problem {
	known := map[string]bool{"install:core": true, "watch": true, "scan": true}
	for _, manifest := range []string{"mise.toml", "agents/go/mise.toml", "agents/data/mise.toml", "evals/mise.toml", "tools/mise.toml"} {
		for name := range taskNames(filepath.Join(root, filepath.FromSlash(manifest))) {
			known[name] = true
		}
	}
	ignored := map[string]bool{"doctor:PROFILE": true, "tasks": true}
	pattern := regexp.MustCompile(`\bmise run ([A-Za-z0-9][A-Za-z0-9:_*-]*(?:<[^>]+>)?)`)
	var problems []Problem
	for where, text := range pages {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			name := match[1]
			if !known[name] && !ignored[name] && !strings.ContainsAny(name, "<*") {
				problems = append(problems, problem(where, "documents unknown mise task %q", name))
			}
		}
	}
	return problems
}

func compareContract(where, label, expected, actual string) []Problem {
	if expected == "" {
		return []Problem{problem(where, "could not resolve authoritative %s", label)}
	}
	if expected != actual {
		return []Problem{problem(where, "%s drifted: expected %q, found %q", label, expected, actual)}
	}
	return nil
}

func checkTaskExpansions(root string, pages pageSet) []Problem {
	const where = "content/3. Capabilities/3.0. Packaging.md"
	pattern := regexp.MustCompile(`(?m)^\| ` + "`" + `mise run ([^` + "`" + `]+)` + "`" + `\s+\| ` + "`" + `([^` + "`" + `]+)` + "`" + `\s+\|`)
	documented := make(map[string]string)
	for _, match := range pattern.FindAllStringSubmatch(pages[where], -1) {
		documented[match[1]] = match[2]
	}
	manifest := filepath.Join(root, "agents", "go", "mise.toml")
	problems := make([]Problem, 0, 8)
	for _, name := range []string{"run", "workflow", "coordinator", "web", "mcp", "mcp:http", "a2a", "data:reset"} {
		problems = append(problems, compareContract(where, "mise run "+name+" expansion", taskExpansion(manifest, name), documented[name])...)
	}
	return problems
}

func checkThemePin(root string) []Problem {
	modules, err := readFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return []Problem{problem("go.mod", "could not read the Hextra module pin: %v", err)}
	}
	var problems []Problem
	if !regexp.MustCompile(`(?m)^require github\.com/imfing/hextra v\d+\.\d+\.\d+`).MatchString(modules) {
		problems = append(problems, problem("go.mod", "the Hextra theme must be pinned to an exact version"))
	}
	const manifestWhere = "assets/js/vendor/versions.json"
	manifest, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestWhere)))
	if err != nil {
		return append(problems, problem(manifestWhere, "could not read vendored bundle versions: %v", err))
	}
	var pins map[string]struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}
	if err := json.Unmarshal(manifest, &pins); err != nil {
		return append(problems, problem(manifestWhere, "could not read vendored bundle versions: %v", err))
	}
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(root, "assets", "js", "vendor", name))
		if err != nil {
			problems = append(problems, problem(manifestWhere, "pinned bundle is missing: %s", name))
			continue
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(content))
		if digest != pins[name].SHA256 {
			problems = append(problems, problem("assets/js/vendor/"+name, "bundle does not match its pinned digest (%s)", pins[name].Version))
		}
	}
	return problems
}

func toolVersion(root, key string) string {
	document, err := loadTOML(filepath.Join(root, "mise.toml"))
	if err != nil {
		return ""
	}
	value, _ := document.GetPath([]string{"tools", key}).(string)
	return value
}

func sourceVersion(text string, pattern *regexp.Regexp) (string, bool) {
	matches := pattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return "", false
	}
	version := matches[0][1]
	for _, match := range matches[1:] {
		if match[1] != version {
			return "", false
		}
	}
	return version, true
}

func checkCopiedVersionInventory(
	pages pageSet,
	label string,
	expected string,
	surfaces map[string]int,
	pattern *regexp.Regexp,
) []Problem {
	actual := make(map[string]int)
	var problems []Problem
	for where, text := range pages {
		matches := pattern.FindAllStringSubmatch(text, -1)
		actual[where] = len(matches)
		for _, match := range matches {
			if match[1] != expected {
				problems = append(problems, problem(where, "%s version drifted: expected %q, found %q", label, expected, match[1]))
			}
		}
	}
	paths := make(map[string]bool, len(actual)+len(surfaces))
	for where := range actual {
		paths[where] = true
	}
	for where := range surfaces {
		paths[where] = true
	}
	ordered := make([]string, 0, len(paths))
	for where := range paths {
		ordered = append(ordered, where)
	}
	slices.Sort(ordered)
	for _, where := range ordered {
		if actual[where] != surfaces[where] {
			problems = append(problems, problem(where, "%s copy inventory drifted: expected %d, found %d", label, surfaces[where], actual[where]))
		}
	}
	return problems
}

func checkSourceVersions(root string, pages pageSet) []Problem {
	var problems []Problem
	setup := "content/1. Setup/1.3. Kubernetes.md"
	rowPattern := regexp.MustCompile(`(?m)^\|\s*([^|]+?)\s*\|\s*([v\d][^|]*?)\s*\|`)
	rows := make(map[string]string)
	for _, match := range rowPattern.FindAllStringSubmatch(pages[setup], -1) {
		rows[strings.ToLower(strings.TrimSpace(match[1]))] = strings.TrimSpace(match[2])
	}
	for label, key := range map[string]string{
		"k3d": "k3d", "kubectl": "kubectl", "helm": "helm", "helmfile": "helmfile",
		"skaffold": "skaffold", "kubeconform": "kubeconform", "kube-linter": "kube-linter",
		"agentgateway": "github:agentgateway/agentgateway",
	} {
		problems = append(problems, compareContract(setup, label+" table version", toolVersion(root, key), rows[label])...)
	}

	goPin := toolVersion(root, "go")
	goDirective := regexp.MustCompile(`(?m)^go (\d+\.\d+\.\d+)$`)
	for _, where := range []string{"agents/go/go.mod", "evals/go.mod", "tools/go.mod"} {
		module, err := readFile(filepath.Join(root, filepath.FromSlash(where)))
		if err != nil {
			problems = append(problems, problem(where, "could not read Go version authority: %v", err))
			continue
		}
		actual, _ := sourceVersion(module, goDirective)
		problems = append(problems, compareContract(where, "Go toolchain version", goPin, actual)...)
	}
	dockerfile, err := readFile(filepath.Join(root, "agents", "go", "Dockerfile"))
	if err != nil {
		problems = append(problems, problem("agents/go/Dockerfile", "could not read Go build image authority: %v", err))
	} else {
		match := regexp.MustCompile(`(?m)^FROM golang:([^@\s]+)@sha256:`).FindStringSubmatch(dockerfile)
		actual := ""
		if match != nil {
			actual = strings.TrimSuffix(match[1], "-alpine")
		}
		problems = append(problems, compareContract("agents/go/Dockerfile", "Go build image version", goPin, actual)...)
	}

	rootMise, miseErr := readFile(filepath.Join(root, "mise.toml"))
	helmfile, helmErr := readFile(filepath.Join(root, "infra", "helmfile.yaml"))
	helmDiff, helmDiffErr := readFile(filepath.Join(root, "scripts", "install-helm-diff.sh"))
	authorities := []struct {
		pattern  *regexp.Regexp
		copies   *regexp.Regexp
		surfaces map[string]int
		readErr  error
		where    string
		label    string
		text     string
	}{
		{
			where: "infra/helmfile.yaml", label: "kagent chart", text: helmfile, readErr: helmErr,
			pattern: regexp.MustCompile(`(?m)^# kagent-chart-version: (\d+\.\d+\.\d+)$`),
			surfaces: map[string]int{
				"content/6. Platform/6.0. Platform.md":         1,
				"content/6. Platform/6.2. Platform Install.md": 2,
			},
			copies: regexp.MustCompile(`(?i)(?:pinned to|pinned stable chart|chart version|at exactly)\s+` + "`?" + `v?(\d+\.\d+\.\d+)`),
		},
		{
			where: "scripts/install-helm-diff.sh", label: "helm-diff", text: helmDiff, readErr: helmDiffErr,
			pattern: regexp.MustCompile(`(?m)^readonly expected=(\d+\.\d+\.\d+)$`),
			surfaces: map[string]int{
				"content/1. Setup/1.3. Kubernetes.md":          2,
				"content/6. Platform/6.2. Platform Install.md": 1,
			},
			copies: regexp.MustCompile(`(?i)helm-diff[^\n]{0,80}?(\d+\.\d+\.\d+)`),
		},
		{
			where: "mise.toml", label: "k6", text: rootMise, readErr: miseErr,
			pattern: regexp.MustCompile(`mise x k6@(\d+\.\d+\.\d+) -- k6 run`),
			surfaces: map[string]int{
				"content/7. Observability/7.2. Monitoring.md": 4,
			},
			copies: regexp.MustCompile(`(?i)k6@(\d+\.\d+\.\d+)`),
		},
	}
	for _, authority := range authorities {
		if authority.readErr != nil {
			problems = append(problems, problem(authority.where, "could not read the %s version authority: %v", authority.label, authority.readErr))
			continue
		}
		expected, ok := sourceVersion(authority.text, authority.pattern)
		if !ok {
			problems = append(problems, problem(authority.where, "could not parse one consistent %s version authority", authority.label))
			continue
		}
		problems = append(problems, checkCopiedVersionInventory(pages, authority.label, expected, authority.surfaces, authority.copies)...)
	}

	versionPatterns := []struct {
		pattern  *regexp.Regexp
		label    string
		expected string
	}{
		{regexp.MustCompile(`(?i)(?:agentgateway|pinned gateway)[^\n]{0,160}?v?(\d+\.\d+\.\d+)`), "agentgateway", toolVersion(root, "github:agentgateway/agentgateway")},
	}
	for _, contract := range versionPatterns {
		for where, text := range pages {
			for _, match := range contract.pattern.FindAllStringSubmatch(text, -1) {
				if match[1] != contract.expected {
					problems = append(problems, problem(where, "%s version drifted: expected %q, found %q", contract.label, contract.expected, match[1]))
				}
			}
		}
	}

	externalImage := regexp.MustCompile(`(?m)(?:--image=|^\s*image:\s+)((?:docker\.io|ghcr\.io|cgr\.dev|cr\.[^/\s]+\.dev)/[^\s` + "`" + `]+)`)
	for where, text := range pages {
		for _, match := range externalImage.FindAllStringSubmatch(text, -1) {
			image := strings.TrimRight(match[1], `"')],`)
			if !strings.Contains(image, "@sha256:") {
				problems = append(problems, problem(where, "external image %q must include an immutable sha256 digest", image))
			}
		}
	}
	return problems
}

func requireTokens(where, text string, tokens ...string) []Problem {
	problems := make([]Problem, 0, len(tokens))
	for _, token := range tokens {
		if !strings.Contains(text, token) {
			problems = append(problems, problem(where, "source contract is missing %q", token))
		}
	}
	return problems
}

func checkCurrentSourceContracts(root string, pages pageSet) []Problem {
	problems := make([]Problem, 0, 9)
	problems = append(problems, checkDependencyAuditContract(root, pages)...)
	problems = append(problems, checkStateCourseContract(root, pages)...)
	problems = append(problems, checkRetrievalCourseContract(root, pages)...)
	problems = append(problems, checkDomainCourseContract(root, pages)...)
	problems = append(problems, checkOutcomeEvidenceContract(pages)...)
	problems = append(problems, checkCapacityContract(root, pages)...)
	problems = append(problems, checkProviderContract(root, pages)...)
	problems = append(problems, checkReleaseFreshnessContract(root, pages)...)
	problems = append(problems, checkArtifactRetentionContract(root, pages)...)
	return problems
}

func checkDependencyAuditContract(root string, pages pageSet) []Problem {
	const licenseScriptWhere = "scripts/check-licenses.sh"
	const licensePageWhere = "content/8. Community/8.1. License.md"
	const lintPageWhere = "content/4. Quality/4.1. Linting.md"
	licenseScript, _ := readFile(filepath.Join(root, filepath.FromSlash(licenseScriptWhere)))
	licensePage := pages[licensePageWhere]
	lintPage := pages[lintPageWhere]
	problems := requireTokens(licenseScriptWhere, licenseScript,
		"check_repository_licenses", `"${lib_dir}/trivy-repository.sh" licenses`,
	)
	problems = append(problems, requireTokens(licensePageWhere, licensePage,
		"trivy-repository.sh", "three Go modules", "agents/go/go.mod", "evals/go.mod", "tools/go.mod",
	)...)
	problems = append(problems, requireTokens(lintPageWhere, lintPage,
		"govulncheck", "check:vuln", "agents/go", "evals", "tools",
	)...)
	for _, stale := range []string{"pip-licenses", "lock-owned profiles", "MLflow", "Python compatibility"} {
		if strings.Contains(licensePage, stale) {
			problems = append(problems, problem(licensePageWhere, "documents retired Python dependency auditing through %q", stale))
		}
	}
	return problems
}

func checkStateCourseContract(root string, pages pageSet) []Problem {
	const where = "content/6. Platform/6.6. Platform Delivery.md"
	stateSource, _ := readFile(filepath.Join(root, "agents", "go", "state", "state.go"))
	text := pages[where]
	var problems []Problem
	for label, pattern := range map[string]*regexp.Regexp{
		"manifest":          regexp.MustCompile(`(?m)^\s*manifestName\s*=\s*"([^"]+)"$`),
		"completion marker": regexp.MustCompile(`(?m)^\s*completeName\s*=\s*"([^"]+)"$`),
	} {
		match := pattern.FindStringSubmatch(stateSource)
		if match == nil {
			problems = append(problems, problem("agents/go/state/state.go", "could not resolve snapshot %s filename", label))
		} else {
			problems = append(problems, requireTokens(where, text, match[1])...)
		}
	}
	problems = append(problems, requireTokens(where, text, "manifest_sha256", "format_version", `args: ["state", "restore",`)...)
	if strings.Contains(text, "databases=") {
		problems = append(problems, problem(where, "documents the retired databases=N completion-marker format"))
	}
	cronjob, _ := readFile(filepath.Join(root, "infra", "k8s", "base", "state-backup.yaml"))
	if !strings.Contains(cronjob, "- state\n") || !strings.Contains(cronjob, "- backup\n") || strings.Contains(cronjob, "command:") {
		problems = append(problems, problem("infra/k8s/base/state-backup.yaml", "backup CronJob must use the shared agent state CLI"))
	}
	drill, _ := readFile(filepath.Join(root, "infra", "scripts", "backup-drill.sh"))
	match := regexp.MustCompile(`(?m)^echo "([^"]+)"$`).FindStringSubmatch(drill)
	if match == nil {
		problems = append(problems, problem("infra/scripts/backup-drill.sh", "could not resolve the drill completion line"))
	} else if !strings.Contains(text, match[1]) {
		problems = append(problems, problem(where, "state drill completion line drifted: expected %q, found %q", match[1], ""))
	}
	return problems
}

func goStructFields(path, typeName string) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil
	}
	var fields []string
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			structure, structureOK := typeSpec.Type.(*ast.StructType)
			if !ok || !structureOK || typeSpec.Name.Name != typeName {
				continue
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					fields = append(fields, name.Name)
				}
			}
		}
	}
	return fields
}

func checkRetrievalCourseContract(root string, pages pageSet) []Problem {
	const where = "content/3. Capabilities/3.4. Memory.md"
	fields := goStructFields(filepath.Join(root, "agents", "go", "memory", "retrieval.go"), "Provenance")
	if len(fields) == 0 {
		return []Problem{problem("agents/go/memory/retrieval.go", "could not resolve semantic index provenance fields")}
	}
	text := pages[where]
	var problems []Problem
	for _, field := range fields {
		if !strings.Contains(text, field) && !strings.Contains(strings.ToLower(text), strings.ToLower(field)) {
			problems = append(problems, problem(where, "source contract is missing %q", "`"+field+"`"))
		}
	}
	if !strings.Contains(text, "Historical checkpoint (") || strings.Contains(text, "\nMeasured checkpoint (") {
		problems = append(problems, problem(where, "the superseded retrieval result must be labeled as a historical checkpoint"))
	}
	return problems
}

func checkDomainCourseContract(root string, pages pageSet) []Problem {
	const where = "content/8. Community/8.7. Capstone.md"
	fields := goStructFields(filepath.Join(root, "agents", "go", "domain", "vocabulary.go"), "Vocabulary")
	if len(fields) == 0 {
		return []Problem{problem("agents/go/domain/vocabulary.go", "could not resolve Vocabulary fields")}
	}
	text := pages[where]
	var problems []Problem
	for _, field := range fields {
		if !strings.Contains(text, "`"+field+"`") {
			problems = append(problems, problem(where, "source contract is missing %q", "`"+field+"`"))
		}
	}
	problems = append(problems, requireTokens(where, text, "agents/go/domain/vocabulary.go", "domain/pivot_test.go", "Reference", "Pivot", "AdaptDataset")...)
	if regexp.MustCompile(`(?i)reference does not ship (?:that|the) seam`).MatchString(text) {
		problems = append(problems, problem(where, "claims the shipped portability seam is absent"))
	}
	return problems
}

func checkOutcomeEvidenceContract(pages pageSet) []Problem {
	const overview = "content/0. Overview/0.0. Course.md"
	matrix := ""
	for _, line := range splitLines(pages[overview]) {
		if strings.HasPrefix(line, "| Evaluate a behavioral boundary") {
			matrix = strings.ToLower(line)
			break
		}
	}
	evaluation := pages["content/4. Quality/4.4. Evaluations.md"]
	_, exercise, _ := strings.Cut(evaluation, "## How would you add an evaluation case?")
	exercise, _, _ = strings.Cut(exercise, "\n## ")
	var problems []Problem
	for _, action := range []string{"diagnose", "repair"} {
		if strings.Contains(matrix, action) && !strings.Contains(strings.ToLower(exercise), action) {
			problems = append(problems, problem(overview, "evaluation outcome promises %q but the linked exercise does not require it", action))
		}
	}
	return problems
}

func checkCapacityContract(root string, pages pageSet) []Problem {
	support, _ := readFile(filepath.Join(root, "SUPPORT.md"))
	match := regexp.MustCompile(`(?m)^<!-- local-platform-capacity: total-ram-gib=(\d+) free-disk-gib=(\d+) -->$`).FindAllStringSubmatch(support, -1)
	if len(match) != 1 {
		return []Problem{problem("SUPPORT.md", "expected exactly one machine-readable local-platform-capacity contract")}
	}
	ram, disk := match[0][1], match[0][2]
	problems := requireTokens("SUPPORT.md", support, "**"+ram+" GiB total RAM**", "**"+disk+" GiB free disk**")
	doctor, _ := readFile(filepath.Join(root, "scripts", "doctor.sh"))
	problems = append(problems, requireTokens("scripts/doctor.sh", doctor, "local-platform-capacity:", "platform_total_ram_gib", "platform_free_disk_gib")...)
	for where, text := range pages {
		for _, value := range []string{ram + " GiB total RAM", disk + " GiB free disk"} {
			if strings.Contains(text, value) {
				problems = append(problems, problem(where, "capacity literal %q belongs only in SUPPORT.md", value))
			}
		}
	}
	return problems
}

func checkProviderContract(root string, pages pageSet) []Problem {
	const envWhere = ".env.example"
	const pageWhere = "content/1. Setup/1.4. Providers.md"
	environment, _ := readFile(filepath.Join(root, envWhere))
	page := pages[pageWhere]
	problems := requireTokens(envWhere, environment, "GOOGLE_CLOUD_PROJECT=your-gcp-project-id")
	problems = append(problems, requireTokens(pageWhere, page,
		"GOOGLE_CLOUD_PROJECT=your-gcp-project-id",
		"GCP_PROJECT_ID=your-gcp-project-id mise run doctor:gcp",
		"does not load `GOOGLE_CLOUD_PROJECT` from `.env`",
	)...)
	assignment := regexp.MustCompile(`(?m)^\s*#?\s*(?:GOOGLE_CLOUD_PROJECT|GCP_PROJECT_ID)\s*=\s*agentops-open-course\s*$`)
	for where, text := range map[string]string{envWhere: environment, pageWhere: page} {
		if assignment.MatchString(text) {
			problems = append(problems, problem(where, "learner GCP example targets the maintainer-owned project"))
		}
	}
	return problems
}

func checkReleaseFreshnessContract(root string, pages pageSet) []Problem {
	const workflowWhere = ".github/workflows/release.yml"
	const pageWhere = "content/8. Community/8.2. Releases.md"
	workflow, _ := readFile(filepath.Join(root, filepath.FromSlash(workflowWhere)))
	problems := requireTokens(workflowWhere, workflow,
		"freshness_evidence:", "issues: read", "application/vnd.github.full+json",
		"gh api --method POST markdown --input -", "--checklist-template-html freshness-template.html",
		"tools/bin/release-freshness", "freshness: $freshness",
		`repos/${GITHUB_REPOSITORY}/git/tags`, `repos/${GITHUB_REPOSITORY}/git/refs`,
	)
	if strings.Contains(workflow, "git push") {
		problems = append(problems, problem(workflowWhere,
			"release tag publication must keep checkout credentials disabled and use the Git Database API"))
	}
	problems = append(problems, requireTokens(pageWhere, pages[pageWhere],
		"issue:<number>", "waiver:<reviewed reason>", "120 days", "every checklist box checked",
		"reject tag updates and deletion", "do not restrict tag creation", "break-glass bypass",
	)...)
	return problems
}

func checkArtifactRetentionContract(root string, pages pageSet) []Problem {
	agents, _ := readFile(filepath.Join(root, "AGENTS.md"))
	match := regexp.MustCompile(`caps artifact and log retention at \*\*(\d+) days\*\*`).FindStringSubmatch(agents)
	if match == nil {
		return []Problem{problem("AGENTS.md", "Actions artifact retention policy is missing")}
	}
	limit, _ := strconv.Atoi(match[1])
	var problems []Problem
	workflows, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	slices.Sort(workflows)
	retentionPattern := regexp.MustCompile(`(?m)^\s*retention-days:\s*(\d+)\s*$`)
	for _, path := range workflows {
		text, _ := readFile(path)
		values := retentionPattern.FindAllStringSubmatch(text, -1)
		where := relative(root, path)
		if strings.Count(text, "uses: actions/upload-artifact@") != len(values) {
			problems = append(problems, problem(where, "every upload-artifact step must declare one retention-days value"))
		}
		for _, value := range values {
			days, _ := strconv.Atoi(value[1])
			if days > limit {
				problems = append(problems, problem(where, "Actions artifact retention %d days exceeds the %d-day policy", days, limit))
			}
		}
	}
	const pageWhere = "content/8. Community/8.2. Releases.md"
	problems = append(problems, requireTokens(pageWhere, pages[pageWhere], fmt.Sprintf("**%d days**", limit), "immutable GitHub release", "OCI image")...)
	return problems
}

func containsInOrder(text string, values ...string) bool {
	position := 0
	for _, value := range values {
		found := strings.Index(text[position:], value)
		if found < 0 {
			return false
		}
		position += found + len(value)
	}
	return true
}

func checkQuickstarts(root string, pages pageSet) []Problem {
	sequence := []string{"mise run install", "mise run doctor", "mise run check:core", "mise run test"}
	readme, _ := readFile(filepath.Join(root, "README.md"))
	var problems []Problem
	for where, text := range map[string]string{"README.md": readme, "content/1. Setup/1.0. System.md": pages["content/1. Setup/1.0. System.md"]} {
		if !containsInOrder(text, sequence...) {
			problems = append(problems, problem(where, "guarded quickstart must contain the canonical install → doctor → check → test sequence"))
		}
	}
	landing := pages[landingPage]
	if !strings.Contains(landing, "<!-- quickstart: unverified-preview -->") || !strings.Contains(strings.ToLower(landing), "unverified preview") {
		problems = append(problems, problem(landingPage, "shorter model-first quickstart must be marked as an unverified preview"))
	}
	ci, _ := readFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if !containsInOrder(ci, "run: mise run install:validation", "run: mise run doctor", "run: mise run check", "run: mise run test") {
		problems = append(problems, problem(".github/workflows/ci.yml", "CI must execute install, the learner base doctor, check, and test in order"))
	}
	linting := pages["content/4. Quality/4.1. Linting.md"]
	if strings.Count(linting, "install:validation") != 1 ||
		!strings.Contains(linting, "documentation workflow installs only its pinned documentation and browser toolchains") ||
		strings.Contains(linting, "install:maintainer") {
		problems = append(problems, problem("content/4. Quality/4.1. Linting.md", "CI prose must distinguish the validation installer from the documentation workflow's narrower toolchain"))
	}
	return problems
}

func hookTaskContract(text string) string {
	before, after, ok := strings.Cut(text, "\npre-push:\n")
	if !ok {
		return ""
	}
	pattern := regexp.MustCompile(`(?m)^\s+run: mise run ([^\s{]+)`)
	collect := func(value string) string {
		matches := pattern.FindAllStringSubmatch(value, -1)
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, match[1])
		}
		return strings.Join(names, ",")
	}
	return "pre-commit=" + collect(before) + "; pre-push=" + collect(after)
}

func doctorToolArrays(text string) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	pattern := regexp.MustCompile(`(?ms)^readonly -a (\w+)=\((.*?)\)$`)
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		set := make(map[string]bool)
		for _, value := range strings.Fields(match[2]) {
			set[value] = true
		}
		result[match[1]] = set
	}
	return result
}

func sourceMetrics(root string) map[string]bool {
	metrics := make(map[string]bool)
	_ = filepath.WalkDir(filepath.Join(root, "agents", "go"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.ValueSpec:
				for index, name := range typed.Names {
					if !strings.HasSuffix(name.Name, "Metric") || index >= len(typed.Values) {
						continue
					}
					if literal, ok := typed.Values[index].(*ast.BasicLit); ok && literal.Kind == token.STRING {
						if value, unquoteErr := strconv.Unquote(literal.Value); unquoteErr == nil && strings.HasPrefix(value, "agentops.") {
							metrics[value] = true
						}
					}
				}
			case *ast.CallExpr:
				selector, ok := typed.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Int64Counter" || len(typed.Args) == 0 {
					return true
				}
				if literal, ok := typed.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
					if value, unquoteErr := strconv.Unquote(literal.Value); unquoteErr == nil && strings.HasPrefix(value, "agentops.") {
						metrics[value] = true
					}
				}
			}
			return true
		})
		return nil
	})
	return metrics
}

func checkMaintainerDrift(root string, pages pageSet) []Problem {
	var problems []Problem
	hook, _ := readFile(filepath.Join(root, "lefthook.yml"))
	marker := "<!-- lefthook-tasks: " + hookTaskContract(hook) + " -->"
	for _, where := range []string{"content/1. Setup/1.5. Workspace.md", "content/4. Quality/4.1. Linting.md"} {
		if !strings.Contains(pages[where], marker) {
			problems = append(problems, problem(where, "lefthook task description drifted from lefthook.yml"))
		}
	}
	doctor, _ := readFile(filepath.Join(root, "scripts", "doctor.sh"))
	const systemWhere = "content/1. Setup/1.0. System.md"
	system := pages[systemWhere]
	if !strings.Contains(system, `{{< include path="scripts/doctor.sh" region="doctor-tool-tiers"`) {
		problems = append(problems, problem(systemWhere, "doctor tool tiers must include the source-owned arrays"))
	}
	arrays := doctorToolArrays(doctor)
	expected := map[string]map[string]bool{
		"base": arrays["base_tools"], "model": arrays["model_tools"], "gateway": arrays["gateway_tools"],
		"platform": arrays["platform_tools"], "gcp": make(map[string]bool),
	}
	for name := range arrays["gcp_platform_tools"] {
		expected["gcp"][name] = true
	}
	for name := range arrays["gcp_tools"] {
		expected["gcp"][name] = true
	}
	known := make(map[string]bool)
	for _, set := range expected {
		for value := range set {
			known[value] = true
		}
	}
	for tier, wanted := range expected {
		prefix := "- **" + tier + "**"
		if tier == "base" {
			prefix = "The base doctor checks"
		}
		line := ""
		for _, candidate := range splitLines(system) {
			if strings.HasPrefix(strings.TrimLeft(candidate, " \t"), prefix) {
				line = candidate
				break
			}
		}
		documented := make(map[string]bool)
		for _, match := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(line, -1) {
			if known[match[1]] {
				documented[match[1]] = true
			}
		}
		if !mapsEqual(wanted, documented) {
			problems = append(problems, problem(systemWhere, "doctor %s tool list drifted: expected %v, found %v", tier, sortedKeys(wanted), sortedKeys(documented)))
		}
	}
	workflow, _ := readFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	match := regexp.MustCompile(`(?m)^\s+run: mise run (install:[\w-]+)\s*$`).FindStringSubmatch(workflow)
	if match == nil {
		problems = append(problems, problem(".github/workflows/ci.yml", "CI must declare one scoped install task"))
	} else {
		for _, where := range []string{"content/1. Setup/1.5. Workspace.md", "content/4. Quality/4.1. Linting.md", "content/8. Community/8.5. Contributions.md"} {
			if !strings.Contains(pages[where], match[1]) {
				problems = append(problems, problem(where, "CI prose must name `%s` from ci.yml", match[1]))
			}
		}
	}
	const metricsWhere = "content/4. Quality/4.3. Metrics.md"
	section := pages[metricsWhere]
	_, section, _ = strings.Cut(section, "## Which application metrics ship?")
	section, _, _ = strings.Cut(section, "\n## ")
	documented := make(map[string]bool)
	for _, match := range regexp.MustCompile("`(agentops\\.[^`]+)`").FindAllStringSubmatch(section, -1) {
		documented[match[1]] = true
	}
	expectedMetrics := sourceMetrics(root)
	if !mapsEqual(expectedMetrics, documented) {
		problems = append(problems, problem(metricsWhere, "custom metric inventory drifted: expected %v, found %v", sortedKeys(expectedMetrics), sortedKeys(documented)))
	}
	return problems
}

func mapsEqual(left, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if !right[value] {
			return false
		}
	}
	return true
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func checkEvalRuntimeBaseline(root string) []Problem {
	workflow, workflowErr := readFile(filepath.Join(root, ".github", "workflows", "eval.yml"))
	baselinePath := filepath.Join(root, "evals", "cost_baseline.json")
	baseline, baselineErr := os.ReadFile(baselinePath)
	if workflowErr != nil || baselineErr != nil {
		return []Problem{problem("evals/cost_baseline.json", "could not read eval runtime evidence")}
	}
	release := regexp.MustCompile(`/ollama/ollama/releases/download/(v\d+\.\d+\.\d+)/ollama-`).FindStringSubmatch(workflow)
	if release == nil {
		return []Problem{problem(".github/workflows/eval.yml", "could not identify the pinned Ollama release")}
	}
	var document map[string]any
	if err := json.Unmarshal(baseline, &document); err != nil {
		return []Problem{problem("evals/cost_baseline.json", "could not read eval runtime evidence: %v", err)}
	}
	expected := "ollama version is " + strings.TrimPrefix(release[1], "v")
	actual, _ := document["ollama_version"].(string)
	if actual == expected {
		return nil
	}
	return []Problem{problem("evals/cost_baseline.json", "reviewed Ollama runtime is %q; scheduled Eval pins %q", actual, expected)}
}

func checkEvalReleaseOrchestration(root string) []Problem {
	manifest := filepath.Join(root, "evals", "mise.toml")
	workflowPath := filepath.Join(root, ".github", "workflows", "eval.yml")
	workflow, err := readFile(workflowPath)
	if err != nil {
		return []Problem{problem(".github/workflows/eval.yml", "could not read evaluation release orchestration: %v", err)}
	}
	var problems []Problem
	command := taskExpansion(manifest, "eval")
	if !strings.Contains(command, "--cost-output cost-observed.json") {
		problems = append(problems, problem("evals/mise.toml", "the canonical eval task must emit eval-results.json and cost-observed.json from the same model run"))
	}
	canonicalRun := regexp.MustCompile(`(?m)^\s+(?:run:\s+)?mise run eval\s*$`)
	separateCostRun := regexp.MustCompile(`(?m)^\s+run:\s+mise run eval:cost(?::observe)?\s*$`)
	if !canonicalRun.MatchString(workflow) || separateCostRun.MatchString(workflow) {
		problems = append(problems, problem(".github/workflows/eval.yml", "release evaluation and cost evidence must come from the same model run"))
	}
	if strings.Contains(taskExpansion(manifest, "eval:cost"), "--cost-output cost-observed.json") {
		problems = append(problems, problem("evals/mise.toml", "eval:cost must not overwrite the canonical cost-observed.json handoff"))
	}
	policyPath := filepath.Join(root, "evals", "release-policy.json")
	if info, statErr := os.Stat(policyPath); statErr != nil || !info.Mode().IsRegular() {
		problems = append(problems, problem("evals/release-policy.json", "the repository-owned release policy is missing"))
	}
	for _, task := range []string{"eval", "eval:a2a"} {
		expansion := taskExpansion(manifest, task)
		if strings.Count(expansion, "--release-policy release-policy.json") != 1 {
			problems = append(problems, problem("evals/mise.toml", "task %s must load --release-policy release-policy.json exactly once", task))
		}
		if strings.Contains(expansion, "--required-case") || strings.Contains(expansion, "--min-pass-rate") {
			problems = append(problems, problem("evals/mise.toml", "task %s must defer release minimums and mandatory cases to release-policy.json", task))
		}
		if strings.Count(expansion, "--judge") != 1 {
			problems = append(problems, problem("evals/mise.toml", "task %s must use the calibrated judge exactly once", task))
		}
	}
	for _, task := range []string{"eval:policy-trial", "eval:a2a:policy-trial"} {
		expansion := taskExpansion(manifest, task)
		if strings.Count(expansion, "--calibration-policy release-policy.json") != 1 ||
			strings.Count(expansion, "--judge") != 1 || strings.Count(expansion, "--repeat 3") != 1 {
			problems = append(problems, problem("evals/mise.toml", "task %s must collect three policy-bound samples with the calibrated judge", task))
		}
	}
	canonicalCalibration := taskExpansion(manifest, "eval:judge-calibration")
	if strings.Count(canonicalCalibration, "--release-policy release-policy.json") != 1 || strings.Contains(canonicalCalibration, "--floor") {
		problems = append(problems, problem("evals/mise.toml", "eval:judge-calibration must defer its agreement floor to release-policy.json"))
	}
	trialCalibration := taskExpansion(manifest, "eval:judge-calibration:trial")
	if strings.Count(trialCalibration, "--floor 0.75") != 1 || strings.Contains(trialCalibration, "--release-policy") {
		problems = append(problems, problem("evals/mise.toml", "eval:judge-calibration:trial must keep its explicit non-release observation floor"))
	}
	developmentCommand := taskExpansion(manifest, "eval:dev")
	if strings.Count(developmentCommand, "--repeat 3") != 1 || strings.Count(developmentCommand, "--min-pass-rate 0.33") != 1 || strings.Contains(developmentCommand, "--release-policy") {
		problems = append(problems, problem("evals/mise.toml", "eval:dev must keep three learner samples at the 0.33 development floor without a release policy"))
	}
	for _, task := range []string{
		"eval", "eval:policy-trial", "eval:dev", "eval:workflow", "eval:report", "eval:a2a",
		"eval:a2a:policy-trial", "eval:cost", "eval:cost:observe", "eval:ground",
		"eval:judge-calibration", "eval:judge-calibration:trial", "eval:retrieval",
	} {
		if !taskLoadsRedactedEnv(manifest, task, "../.env") {
			problems = append(problems, problem("evals/mise.toml", "live task %s must load the redacted root .env", task))
		}
	}
	for _, task := range []string{"eval:validate", "eval:ab"} {
		if taskLoadsRedactedEnv(manifest, task, "../.env") {
			problems = append(problems, problem("evals/mise.toml", "offline or artifact-only task %s must not load the root .env", task))
		}
	}
	workflowCommand := taskExpansion(manifest, "eval:workflow")
	if strings.Count(workflowCommand, "--repeat 3") != 1 || strings.Count(workflowCommand, "--min-pass-rate 0.33") != 1 {
		problems = append(problems, problem("evals/mise.toml", "eval:workflow must preserve three samples at the reviewed 0.33 pass-rate floor"))
	}
	promptCommand := taskExpansion(manifest, "eval:ab")
	if !strings.Contains(promptCommand, "--require-distinct-source") || !taskAcceptsPromptArtifacts(manifest, "eval:ab") {
		problems = append(problems, problem("evals/mise.toml", "eval:ab must accept explicit prompt artifacts from distinct source revisions"))
	}
	rootManifest := filepath.Join(root, "mise.toml")
	rootPromptCommand := taskExpansion(rootManifest, "eval:ab")
	if !taskAcceptsPromptArtifacts(rootManifest, "eval:ab") || !strings.Contains(rootPromptCommand, "mise run eval:ab --") {
		problems = append(problems, problem("mise.toml", "root eval:ab must forward explicit prompt artifacts to the eval module"))
	}
	retrievalCommand := taskExpansion(manifest, "eval:retrieval")
	if !strings.Contains(retrievalCommand, "agentops-eval retrieval") || strings.Contains(retrievalCommand, "agentops-eval compare") {
		problems = append(problems, problem("evals/mise.toml", "eval:retrieval must run the end-to-end retrieval-quality evaluation"))
	}
	return problems
}

func taskAcceptsPromptArtifacts(path, name string) bool {
	document, err := loadTOML(path)
	if err != nil {
		return false
	}
	usage, ok := document.GetPath([]string{"tasks", name, "usage"}).(string)
	if !ok || !strings.Contains(usage, `flag "--baseline <path>"`) ||
		!strings.Contains(usage, `flag "--candidate <path>"`) {
		return false
	}
	command := taskExpansion(path, name)
	return strings.Count(command, `--baseline "$usage_baseline"`) == 1 &&
		strings.Count(command, `--candidate "$usage_candidate"`) == 1 &&
		strings.Count(command, "--baseline") == 1 && strings.Count(command, "--candidate") == 1
}

func taskLoadsRedactedEnv(path, name, expectedPath string) bool {
	document, err := loadTOML(path)
	if err != nil {
		return false
	}
	pathValue, pathOK := document.GetPath([]string{"tasks", name, "env", "_", "file", "path"}).(string)
	redact, redactOK := document.GetPath([]string{"tasks", name, "env", "_", "file", "redact"}).(bool)
	return pathOK && redactOK && pathValue == expectedPath && redact
}
