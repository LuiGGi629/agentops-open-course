package freshness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml"
)

func readText(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func misePins(root string) (map[string]string, error) {
	document, err := toml.LoadFile(filepath.Join(root, "mise.toml"))
	if err != nil {
		return nil, err
	}
	tools, ok := document.Get("tools").(*toml.Tree)
	if !ok {
		return nil, errors.New("mise.toml has no [tools] table")
	}
	pins := make(map[string]string)
	for _, name := range tools.Keys() {
		if value, ok := tools.Get(name).(string); ok {
			pins[name] = value
		}
	}
	return pins, nil
}

type miseOutdatedFunc func(context.Context, string) (map[string]MiseUpdate, error)

type goCompatibilityHold struct {
	Manifest   string
	Module     string
	Version    string
	Owner      string
	Constraint string
	Validator  string
}

func goCompatibilityHolds(root string) ([]goCompatibilityHold, error) {
	var manifests []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "site", "vendor":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Name() == "go.mod" {
			manifests = append(manifests, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(manifests)
	pattern := regexp.MustCompile(`^\s*(\S+)\s+(v\S+)\s+// compatibility hold: owner=(\S+) constraint=(\S+) validator=(\S(?:.*\S)?)\s*$`)
	var holds []goCompatibilityHold
	for _, manifest := range manifests {
		text, readErr := readText(manifest)
		if readErr != nil {
			return nil, readErr
		}
		where, relativeErr := filepath.Rel(root, manifest)
		if relativeErr != nil {
			return nil, relativeErr
		}
		where = filepath.ToSlash(where)
		for index, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, "// compatibility hold:") {
				continue
			}
			match := pattern.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("%s:%d: malformed compatibility hold", where, index+1)
			}
			holds = append(holds, goCompatibilityHold{
				Manifest: where, Module: match[1], Version: match[2], Owner: match[3], Constraint: match[4], Validator: strings.TrimSpace(match[5]),
			})
		}
	}
	return holds, nil
}

func goCompatibilityHoldStatus(root string, hold goCompatibilityHold) string {
	separator := strings.LastIndex(hold.Owner, "@")
	if separator <= 0 || separator == len(hold.Owner)-1 || hold.Validator == "" || !strings.Contains(hold.Version, hold.Constraint) {
		return "MISMATCH"
	}
	ownerName, ownerVersion := hold.Owner[:separator], hold.Owner[separator+1:]
	manifest := hold.Manifest
	if manifest == "" {
		manifest = "tools/go.mod"
	}
	text, err := readText(filepath.Join(root, filepath.FromSlash(manifest)))
	if err != nil {
		return "MISMATCH"
	}
	ownerModule := ownerName
	if !strings.Contains(ownerName, "/") {
		// Short sibling names remain valid for the existing chromedp hold; cross-
		// namespace owners use their full module path and bypass this derivation.
		ownerModule = filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(hold.Module)), ownerName))
	}
	if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(ownerModule) + `\s+` + regexp.QuoteMeta(ownerVersion) + `(?:\s|$)`).MatchString(text) {
		return "MISMATCH"
	}
	return "HELD"
}

func runMiseOutdated(ctx context.Context, root string) (map[string]MiseUpdate, error) {
	executable, err := exec.LookPath("mise")
	if err != nil {
		return nil, errors.New("mise is not available")
	}
	timeoutContext, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	command := exec.CommandContext(timeoutContext, executable, "outdated", "--json")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, errors.New(cleanError(string(exit.Stderr)))
		}
		return nil, errors.New(cleanError(err))
	}
	return parseMiseOutdatedJSON(output)
}

func releasePins(root, helmVersion string) (map[string]string, error) {
	k3s, err := readText(filepath.Join(root, "infra", "k3d.yaml"))
	if err != nil {
		return nil, err
	}
	ollama, err := readText(filepath.Join(root, ".github", "workflows", "eval.yml"))
	if err != nil {
		return nil, err
	}
	freshness, err := readText(filepath.Join(root, ".github", "workflows", "freshness.yml"))
	if err != nil {
		return nil, err
	}
	value := func(pattern *regexp.Regexp, text string) string {
		match := pattern.FindStringSubmatch(text)
		if match == nil {
			return "not found"
		}
		return match[1]
	}
	kagent := "not found"
	if helmVersion != "" {
		kagent = "v" + helmVersion
	}
	mise := value(regexp.MustCompile(`(?m)^\s*version:\s*(\d{4}\.\d+\.\d+)\s*$`), freshness)
	if mise != "not found" {
		mise = "v" + mise
	}
	return map[string]string{
		"k3s":    value(regexp.MustCompile(`rancher/k3s:(v[^@\s]+)@sha256:`), k3s),
		"Ollama": value(regexp.MustCompile(`/releases/download/(v\d+\.\d+\.\d+)/ollama-`), ollama),
		"kagent": kagent,
		"mise":   mise,
	}, nil
}

func staticImageReferences(root string) (map[string][]string, error) {
	candidates := []string{
		filepath.Join(root, "agents", "go", "Dockerfile"),
		filepath.Join(root, "infra", "k3d.yaml"),
		filepath.Join(root, "infra", "observability", "compose.yaml"),
		filepath.Join(root, "scripts", "smoke-host.sh"),
	}
	walk := func(directory string, include func(string, os.DirEntry) bool) error {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(directory)), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && include(path, entry) {
				candidates = append(candidates, path)
			}
			return nil
		})
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := walk("infra/k8s", func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml"
	}); err != nil {
		return nil, err
	}
	if err := walk(".github/workflows", func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml"
	}); err != nil {
		return nil, err
	}
	if err := walk("infra/scripts", func(path string, _ os.DirEntry) bool {
		// Test fixtures intentionally contain fake registries and digests; they
		// are assertions, not authorities that the freshness report should query.
		return !strings.HasPrefix(filepath.Base(path), "test-")
	}); err != nil {
		return nil, err
	}
	slices.Sort(candidates)
	candidates = slices.Compact(candidates)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*FROM\s+([^\s]+)`),
		regexp.MustCompile(`(?m)^\s*image:\s*["']?([^"'\s#]+)`),
		regexp.MustCompile(`(?m)^\s*(?:IMAGE|[A-Z][A-Z0-9_]*_IMAGE):\s*["']?([^"'\s#]+)`),
		regexp.MustCompile(`(?mi)^\s*(?:readonly\s+)?(?:image|[a-z_][a-z0-9_]*_image)=["']?([^"'\s#]+)`),
	}
	references := make(map[string][]string)
	for _, path := range candidates {
		text, readErr := readText(path)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}
			return nil, readErr
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(text, -1) {
				if externalImage(match[1]) {
					relative, _ := filepath.Rel(root, path)
					references[match[1]] = append(references[match[1]], filepath.ToSlash(relative))
				}
			}
		}
	}
	for reference, sources := range references {
		slices.Sort(sources)
		references[reference] = slices.Compact(sources)
	}
	return references, nil
}

func externalImage(reference string) bool {
	if strings.ContainsAny(reference, "$[]") {
		return false
	}
	name := reference
	name, _, _ = strings.Cut(name, "@")
	last := name[strings.LastIndex(name, "/")+1:]
	last, _, _ = strings.Cut(last, ":")
	return !strings.HasPrefix(last, "agentops-")
}

func imageValidationTier(reference string, sources []string) string {
	joined := strings.Join(sources, " ")
	switch {
	case strings.Contains(reference, "rancher/k3s") || strings.Contains(joined, "infra/k8s"):
		return "check:infra + Platform"
	case strings.Contains(reference, "agentgateway") || strings.Contains(joined, "smoke-host"):
		return "smoke:host + check:infra"
	case strings.Contains(joined, "observability"):
		return "check:infra + observability smoke"
	default:
		return "build + scan"
	}
}

func miseValidationTier(name string) string {
	platform := map[string]bool{
		"k3d": true, "kubectl": true, "helm": true, "helmfile": true, "skaffold": true,
		"kubeconform": true, "kube-linter": true, "opentofu": true, "tflint": true,
		"sops": true, "age": true, "github:mikefarah/yq": true,
		"github:agentgateway/agentgateway": true,
	}
	if platform[name] {
		return "install:platform + check:infra"
	}
	if name == "gh" {
		return "install:maintainer + check:workflows"
	}
	return "install + check:core"
}
