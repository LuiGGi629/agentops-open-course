package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckNavigationRejectsMissingPage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "nav.yaml"), []byte("- page: _index.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pages := pageSet{
		"content/_index.md":              "",
		"content/9. Ghost/9.0. Ghost.md": "",
	}
	problems := checkNavigation(root, pages)
	if !strings.Contains(problemMessages(problems), "navigation omits pages") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestCompareContractReportsAuthorityAndDrift(t *testing.T) {
	if problems := compareContract("content/example.md", "pin", "", "1"); len(problems) != 1 || !strings.Contains(problems[0].Message, "authoritative") {
		t.Fatalf("missing authority problems = %#v", problems)
	}
	problems := compareContract("content/example.md", "pin", "2.0.0", "1.9.0")
	if len(problems) != 1 || problems[0].Message != `pin drifted: expected "2.0.0", found "1.9.0"` {
		t.Fatalf("drift problems = %#v", problems)
	}
}

func TestDependencyAuditContractRejectsRetiredPythonInventory(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "scripts", "check-licenses.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("check_repository_licenses\n\"${lib_dir}/trivy-repository.sh\" licenses\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pages := pageSet{
		"content/8. Community/8.1. License.md": "trivy-repository.sh three Go modules agents/go/go.mod evals/go.mod tools/go.mod pip-licenses",
		"content/4. Quality/4.1. Linting.md":   "govulncheck check:vuln agents/go evals tools",
	}
	problems := checkDependencyAuditContract(root, pages)
	if !strings.Contains(problemMessages(problems), "retired Python dependency auditing") {
		t.Fatalf("problems = %#v", problems)
	}
}
