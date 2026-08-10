package freshness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoCompatibilityHoldRequiresPinnedOwnerAndValidator(t *testing.T) {
	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	if err := os.Mkdir(toolsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goMod := `module example.test/tools

go 1.26.5

require (
	github.com/chromedp/cdproto v0.0.0-20260714215040-dc233986426f // compatibility hold: owner=chromedp@v0.16.0 constraint=dc233986426f validator=real Chrome accessibility acceptance
	github.com/chromedp/chromedp v0.16.0
)
`
	if err := os.WriteFile(filepath.Join(toolsDir, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}

	holds, err := goCompatibilityHolds(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(holds) != 1 {
		t.Fatalf("holds = %d, want 1", len(holds))
	}
	if status := goCompatibilityHoldStatus(root, holds[0]); status != "HELD" {
		t.Fatalf("status = %q, want HELD", status)
	}

	holds[0].Owner = "chromedp@v0.17.0"
	if status := goCompatibilityHoldStatus(root, holds[0]); status != "MISMATCH" {
		t.Fatalf("status with unpinned owner = %q, want MISMATCH", status)
	}
	holds[0].Owner = "chromedp@v0.16.0"
	holds[0].Validator = ""
	if status := goCompatibilityHoldStatus(root, holds[0]); status != "MISMATCH" {
		t.Fatalf("status without validator = %q, want MISMATCH", status)
	}
}

func TestGoCompatibilityHoldsDiscoverEveryModuleAndCrossNamespaceOwner(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"agents/go/go.mod": `module example.test/agent

go 1.26.5

require (
	github.com/openai/openai-go/v3 v3.8.1 // compatibility hold: owner=google.golang.org/adk/v2@v2.1.0 constraint=v3.8.1 validator=agent check and test
	google.golang.org/adk/v2 v2.1.0
)
`,
		"tools/go.mod": `module example.test/tools

go 1.26.5

require (
	github.com/chromedp/cdproto v0.0.0-20260714215040-dc233986426f // compatibility hold: owner=github.com/chromedp/chromedp@v0.16.0 constraint=dc233986426f validator=real Chrome accessibility acceptance
	github.com/chromedp/chromedp v0.16.0
)
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
	holds, err := goCompatibilityHolds(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(holds) != 2 {
		t.Fatalf("holds = %#v, want two", holds)
	}
	for _, hold := range holds {
		if status := goCompatibilityHoldStatus(root, hold); status != "HELD" {
			t.Fatalf("%s status = %q, want HELD", hold.Module, status)
		}
	}
}

func TestGoCompatibilityHoldsRejectMalformedStructuredComment(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "evals")
	if err := os.MkdirAll(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	goMod := "module example.test/evals\n\ngo 1.26.5\n\nrequire example.test/held v1.0.0 // compatibility hold: owner=missing-fields\n"
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := goCompatibilityHolds(root); err == nil || !strings.Contains(err.Error(), "evals/go.mod:5: malformed compatibility hold") {
		t.Fatalf("goCompatibilityHolds error = %v", err)
	}
}

func TestRepositoryCompatibilityHoldsAreStructuredAndValidated(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	holds, err := goCompatibilityHolds(root)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"github.com/chromedp/cdproto":    false,
		"github.com/openai/openai-go/v3": false,
		"go.opentelemetry.io/otel":       false,
		"go.opentelemetry.io/otel/log":   false,
		"google.golang.org/genai":        false,
	}
	for _, hold := range holds {
		if _, ok := wanted[hold.Module]; !ok {
			continue
		}
		if status := goCompatibilityHoldStatus(root, hold); status != "HELD" {
			t.Fatalf("%s status = %q, want HELD", hold.Module, status)
		}
		wanted[hold.Module] = true
	}
	for module, found := range wanted {
		if !found {
			t.Fatalf("repository compatibility holds omit %s", module)
		}
	}
}
