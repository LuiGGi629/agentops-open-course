package freshness

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type fakeFetcher struct{}

func (fakeFetcher) JSON(_ context.Context, endpoint string, _ http.Header) (any, error) {
	if strings.Contains(endpoint, "/releases?") {
		tag := "v1.0.0"
		if strings.Contains(endpoint, "k3s-io") {
			tag = "v1.36.2+k3s1"
		} else if strings.Contains(endpoint, "ollama/ollama") {
			tag = "v0.32.5"
		} else if strings.Contains(endpoint, "jdx/mise") {
			tag = "v2026.7.18"
		}
		return []any{map[string]any{"tag_name": tag, "html_url": "https://example.test/release", "assets": []any{}}}, nil
	}
	return map[string]any{"token": "fake"}, nil
}

func (fakeFetcher) Get(_ context.Context, _ string, _ http.Header) ([]byte, http.Header, error) {
	header := make(http.Header)
	header.Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
	return nil, header, nil
}

func TestReportIsDeterministicAndReadOnlyOffline(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	document, err := Report(context.Background(), Options{
		Root: root, RunID: "fixture-42", GeneratedAt: time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		Fetcher: fakeFetcher{},
		MiseOutdated: func(context.Context, string) (map[string]MiseUpdate, error) {
			return map[string]MiseUpdate{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{
		"<!-- freshness-report:fixture-42 -->",
		"## Automated freshness snapshot — 2026-08-08",
		"### Go module authorities",
		"### Go module compatibility holds",
		"chromedp/cdproto",
		"agents/go/go.mod",
		"openai/openai-go/v3",
		"google.golang.org/genai",
		"go.opentelemetry.io/otel/log",
		"HELD",
		"### Static external image pins",
		"This reporter never updates a pin or opens a pull request.",
	} {
		if !strings.Contains(document, wanted) {
			t.Fatalf("report is missing %q", wanted)
		}
	}
}

func TestStaticImageInventoryUsesGoAgentDockerfile(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	images, err := staticImageReferences(root)
	if err != nil {
		t.Fatal(err)
	}
	wantedSources := map[string]bool{
		"agents/go/Dockerfile":           false,
		".github/workflows/platform.yml": false,
		"infra/scripts/gateway-host.sh":  false,
	}
	for _, sources := range images {
		for _, source := range sources {
			if _, ok := wantedSources[source]; ok {
				wantedSources[source] = true
			}
		}
	}
	for source, found := range wantedSources {
		if !found {
			t.Fatalf("static image inventory did not include %s", source)
		}
	}
}

func TestStaticImageInventoryIncludesWorkflowAndProductionInfraScriptAuthorities(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("a", 64)
	files := map[string]string{
		".github/workflows/platform.yml": "env:\n  CURL_IMAGE: curlimages/curl:8.21.0@sha256:" + digest + "\n",
		"infra/scripts/gateway-host.sh":  "readonly image=\"cr.agentgateway.dev/agentgateway:v1.4.1@sha256:" + digest + "\"\n",
		"infra/scripts/test-fixture.sh":  "FAKE_IMAGE=\"registry.invalid/not-an-authority:v1@sha256:" + digest + "\"\n",
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
	images, err := staticImageReferences(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"curlimages/curl:8.21.0@sha256:" + digest:                  ".github/workflows/platform.yml",
		"cr.agentgateway.dev/agentgateway:v1.4.1@sha256:" + digest: "infra/scripts/gateway-host.sh",
	}
	for reference, source := range want {
		if !slices.Contains(images[reference], source) {
			t.Fatalf("images[%q] = %#v, want source %q", reference, images[reference], source)
		}
	}
	for reference := range images {
		if strings.Contains(reference, "not-an-authority") {
			t.Fatalf("test fixture was inventoried as an authority: %q", reference)
		}
	}
}

func TestDependabotAgentBatchExcludesCompatibilityFamilies(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Updates []struct {
			Groups map[string]struct {
				ExcludePatterns []string `yaml:"exclude-patterns"`
			} `yaml:"groups"`
			Ecosystem string `yaml:"package-ecosystem"`
			Directory string `yaml:"directory"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	var exclusions []string
	for _, update := range document.Updates {
		if update.Ecosystem == "gomod" && update.Directory == "/agents/go" {
			exclusions = update.Groups["go-agent-dependencies"].ExcludePatterns
		}
	}
	for _, dependency := range []string{
		"github.com/openai/openai-go/v3",
		"go.opentelemetry.io/otel*",
		"google.golang.org/adk/v2",
		"google.golang.org/genai",
	} {
		if !slices.Contains(exclusions, dependency) {
			t.Fatalf("agent dependency exclusions = %#v, want %q", exclusions, dependency)
		}
	}
}
