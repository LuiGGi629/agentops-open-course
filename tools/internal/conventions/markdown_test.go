package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPageURLRequiresSlugAndRejectsURLShadowing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "content", "0. Overview", "0.7. Glossary.md")
	text := "---\ntitle: \"0.7. Glossary\"\ndescription: x\nslug: \"0-7-glossary\"\nurl: \"/wrong/\"\n---\n\n# Duplicate\n\n## Why?\n"
	problems := checkPageURL(root, "content/0. Overview/0.7. Glossary.md", path, text)
	messages := problemMessages(problems)
	if !strings.Contains(messages, "must not define url") || !strings.Contains(messages, "would publish a second <h1>") {
		t.Fatalf("problems = %#v", problems)
	}

	valid := "---\ntitle: \"0.7. Glossary\"\ndescription: x\nslug: \"0-7-glossary\"\n---\n\n## Why?\n"
	if problems := checkPageURL(root, "content/0. Overview/0.7. Glossary.md", path, valid); len(problems) != 0 {
		t.Fatalf("valid slug problems = %#v", problems)
	}
}

func TestCheckPageURLUsesSectionSlugAndLeavesHomeAtRoot(t *testing.T) {
	root := t.TempDir()
	sectionPath := filepath.Join(root, "content", "2. Agents", "_index.md")
	section := "---\ntitle: \"2. Agents\"\ndescription: x\nslug: \"2-agents\"\n---\n\n## Why?\n"
	if problems := checkPageURL(root, "content/2. Agents/_index.md", sectionPath, section); len(problems) != 0 {
		t.Fatalf("section slug problems = %#v", problems)
	}
	homePath := filepath.Join(root, "content", "_index.md")
	home := "---\ntitle: Home\ndescription: x\n---\n\n## Why?\n"
	if problems := checkPageURL(root, "content/_index.md", homePath, home); len(problems) != 0 {
		t.Fatalf("home route problems = %#v", problems)
	}
}

func TestBuildPageRoutesDerivesHierarchyAndRejectsCollisions(t *testing.T) {
	pages := pageSet{
		"content/_index.md":                         "---\ntitle: Home\ndescription: x\n---\n",
		"content/2. Agents/_index.md":               "---\ntitle: Agents\ndescription: x\nslug: 2-agents\n---\n",
		"content/2. Agents/2.1. First Agent.md":     "---\ntitle: First\ndescription: x\nslug: 2-1-first-agent\n---\n",
		"content/2. Agents/2.1. Duplicate Route.md": "---\ntitle: Duplicate\ndescription: x\nslug: 2-1-first-agent\n---\n",
		"content/3. Capabilities/_index.md":         "---\ntitle: Capabilities\ndescription: x\nslug: 3-capabilities\n---\n",
		"content/3. Capabilities/Duplicate Slug.md": "---\ntitle: Duplicate\ndescription: x\nslug: 2-1-first-agent\n---\n",
	}
	routes, problems := buildPageRoutes(pages)
	if got := routes["content/_index.md"].Path; got != "/" {
		t.Errorf("home route = %q, want /", got)
	}
	if got := routes["content/2. Agents/_index.md"].Path; got != "/2-agents/" {
		t.Errorf("section route = %q", got)
	}
	if got := routes["content/2. Agents/2.1. First Agent.md"].Path; got != "/2-agents/2-1-first-agent/" {
		t.Errorf("regular route = %q", got)
	}
	if !strings.Contains(problemMessages(problems), "same permalink") {
		t.Fatalf("collision problems = %#v", problems)
	}
	if !strings.Contains(problemMessages(problems), "duplicate regular-page slug") {
		t.Fatalf("duplicate slug problems = %#v", problems)
	}
}

func TestCheckPermalinkConfigRequiresSlugPatterns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hugo.toml")
	valid := `[[permalinks]]
pattern = "/:sectionslugs/:slug/"
[permalinks.target]
kind = "page"

[[permalinks]]
pattern = "/:sectionslugs/"
[permalinks.target]
kind = "section"
`
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := checkPermalinkConfig(root); len(problems) != 0 {
		t.Fatalf("valid permalink config problems = %#v", problems)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(valid, ":sectionslugs", ":sections", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if messages := problemMessages(checkPermalinkConfig(root)); !strings.Contains(messages, ":sectionslugs/:slug") {
		t.Fatalf("missing slug contract problems = %s", messages)
	}
}

func TestCheckFrontMatterUsesYAMLParser(t *testing.T) {
	text := "---\ntitle: Example\ndescription: invalid: colon\n---\n\n## Why?\n"
	problems := checkFrontMatter("content/example.md", text)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "not valid YAML") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestCheckSnippetTargetsRequiresOneBoundedRegion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "infra", "example.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# --8<-- [start:trusted]\nvalue: 1\n# --8<-- [end:trusted]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := `{{< include path="infra/example.yaml" region="trusted" lang="yaml" >}}` + "\n"
	if problems := checkSnippetTargets(root, "content/example.md", text); len(problems) != 0 {
		t.Fatalf("problems = %#v", problems)
	}
	if err := os.WriteFile(path, []byte("# --8<-- [start:trusted]\nvalue: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	problems := checkSnippetTargets(root, "content/example.md", text)
	if !strings.Contains(problemMessages(problems), "exactly one start and end marker") {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestCheckSnippetsRejectsFenceAndRetiredSyntax(t *testing.T) {
	text := "```go\n{{< include path=\"a.go\" region=\"b\" >}}\n```\n--8<-- \"a.go:b\"\n"
	messages := problemMessages(checkSnippets("content/example.md", text))
	if !strings.Contains(messages, "must not sit inside") || !strings.Contains(messages, "Material snippet syntax is retired") {
		t.Fatal(messages)
	}
}

func TestCheckExercisesPreservesTemporaryAndProbabilisticFailures(t *testing.T) {
	temporary := `## Your turn: what changes?

- **Mode**: ` + "`temporary experiment`" + `
- **Goal**: Change one thing.
- **Files to touch**: example.go.
- **Preflight**: Start clean.
- **Gate that proves completion**: The test is red.
- **Final state**: Restore it.
`
	if problems := checkExercises("content/example.md", temporary); len(problems) != 2 {
		t.Fatalf("temporary problems = %#v", problems)
	}
	probabilisticText := `## Your turn: what changes?

- **Mode**: ` + "`inspect`" + `
- **Goal**: Observe.
- **Files to touch**: None.
- **Preflight**: None.
- **Gate that proves completion**: It fails without the rule, but may pass.
- **Final state**: Clean.
`
	if !strings.Contains(problemMessages(checkExercises("content/example.md", probabilisticText)), "probabilistic evidence") {
		t.Fatal("mandatory probabilistic red-state was accepted")
	}
}

func TestCheckDiagramAlternativesRatchetsChangedDiagram(t *testing.T) {
	legacy := map[string]bool{digestLines([]string{"flowchart LR", "A --> B"}): true}
	problems, _ := checkDiagramAlternatives("content/example.md", "```mermaid\nflowchart LR\nA --> C\n```\n", legacy)
	if len(problems) != 1 {
		t.Fatalf("problems = %#v", problems)
	}
}

func TestExactCountClaimsIgnoreFences(t *testing.T) {
	text := "Exactly 16 tests pass.\n```text\nExactly 4 tests pass.\n```\n"
	problems := checkExactCountClaims("content/example.md", text)
	if len(problems) != 1 || problems[0].Message != "line 1: replace brittle exact line/test/module count with derived or count-free evidence" {
		t.Fatalf("problems = %#v", problems)
	}
}

func problemMessages(problems []Problem) string {
	values := make([]string, len(problems))
	for index, item := range problems {
		values[index] = item.Message
	}
	return strings.Join(values, "\n")
}
