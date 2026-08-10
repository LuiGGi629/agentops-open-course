package conventions

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	glanceOpen     = `{{% admonition abstract "In one glance" %}}`
	maxCollapsible = 3
)

var (
	frontMatterPattern = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`)
	fencePattern       = regexp.MustCompile(`^\s*(` + "`{3,}" + `|~{3,})\s*(\S*)`)
	machinePathPattern = regexp.MustCompile(`/home/[^ /]+|file:///|k3d-registry\.localhost`)
	collapsiblePattern = regexp.MustCompile(`^\{\{% collapsible \w+ "([^"]*)" %\}\}`)
	bareNumberLabel    = regexp.MustCompile(`\[(\d+(?:\.\d+)*)\]\(`)
	timeLinePattern    = regexp.MustCompile(`(?m)^- \*\*Time:\*\* (.+)$`)
	admonitionPattern  = regexp.MustCompile(`^\{\{% (?:admonition|collapsible) ([a-z-]+)(?: "[^"]*")? %\}\}$`)
	orderedItemPattern = regexp.MustCompile(`^(\s*)(\d+)\.\s`)
	fullPageLink       = regexp.MustCompile(`\[([0-8]\.\d+\.\s+[^]]+)\]\(\{\{< relref "([^"#]+)`)
	permalinkSlug      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	exerciseMode       = regexp.MustCompile(`\*\*Mode\*\*:\s*` + "`" + `(inspect|temporary experiment|keep|capstone carry-forward)` + "`")
	probabilistic      = regexp.MustCompile(`(?i)\bmay pass\b`)
	mandatoryRed       = regexp.MustCompile(`(?i)\b(?:must fail|fails? without|exits? non-zero)\b`)
	directoryRestore   = regexp.MustCompile(`git restore -- (?:agents/data|infra/k8s)(?:\s|` + "`" + `|$)`)
)

var (
	glanceFields = []string{"**You will:**", "**You need:**", "**Time:**"}
	// Both spellings are accepted while the pages migrate. The question forms are the
	// original FAQ frame; the declarative forms belong to the teaching-style contract,
	// where a page closes by naming what the learner can now do rather than by asking
	// itself whether it worked.
	closingHeads = []string{
		"## What proves this page worked?",
		"## What proves this chapter worked?",
		"## How should you use this page later?",
		"## What you can do now",
		"## What this chapter proved",
		"## How to use this page later",
	}
	pageKinds          = []string{"concept", "hands-on", "reference", "orientation", "lookup"}
	allowedAdmonitions = map[string]bool{
		"abstract": true, "danger": true, "info": true, "note": true,
		"success": true, "tip": true, "warning": true,
	}
)

type numberedLine struct {
	text   string
	number int
}

type fencedBlock struct {
	body  []string
	start int
	end   int
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n")
}

func linesOutsideFences(text string) []numberedLine { return linesByFence(text, false) }

func linesInsideFences(text string) []numberedLine { return linesByFence(text, true) }

func linesByFence(text string, inside bool) []numberedLine {
	fenced := false
	result := make([]numberedLine, 0)
	for index, line := range splitLines(text) {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced == inside {
			result = append(result, numberedLine{number: index + 1, text: line})
		}
	}
	return result
}

func fencedBlocks(text, language string) []fencedBlock {
	var result []fencedBlock
	var delimiter, blockLanguage string
	start := 0
	body := make([]string, 0)
	for index, line := range splitLines(text) {
		number := index + 1
		match := fencePattern.FindStringSubmatch(line)
		if delimiter == "" {
			if match != nil {
				delimiter, blockLanguage, start = match[1], match[2], number
				body = body[:0]
			}
			continue
		}
		if match != nil && match[2] == "" && match[1][0] == delimiter[0] && len(match[1]) >= len(delimiter) {
			if blockLanguage == language {
				result = append(result, fencedBlock{start: start, end: number, body: slices.Clone(body)})
			}
			delimiter = ""
			continue
		}
		body = append(body, line)
	}
	return result
}

func parseFrontMatter(text string) (map[string]any, string) {
	match := frontMatterPattern.FindStringSubmatch(text)
	if match == nil {
		return nil, "expected YAML front matter at the start of the file"
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(match[1]), &document); err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		return nil, fmt.Sprintf("front matter is not valid YAML (%s); quote values containing ': '", first)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, "front matter must be a YAML mapping"
	}
	metadata := make(map[string]any)
	if err := document.Content[0].Decode(&metadata); err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		return nil, fmt.Sprintf("front matter is not valid YAML (%s); quote values containing ': '", first)
	}
	return metadata, ""
}

func declaredKind(text string) string {
	match := timeLinePattern.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	found := make([]string, 0, 1)
	for _, kind := range pageKinds {
		pattern := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(kind) + `([^A-Za-z0-9_-]|$)`)
		if pattern.MatchString(match[1]) {
			found = append(found, kind)
		}
	}
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

func digestLines(lines []string) string {
	normalized := strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))
}

func checkFrontMatter(where, text string) []Problem {
	metadata, message := parseFrontMatter(text)
	if metadata == nil {
		return []Problem{problem(where, "%s", message)}
	}
	description, ok := metadata["description"].(string)
	if !ok || strings.TrimSpace(description) == "" {
		return []Problem{problem(where, "front matter must define a non-empty description")}
	}
	return nil
}

func checkPageURL(root, where, path, text string) []Problem {
	metadata, _ := parseFrontMatter(text)
	if metadata == nil {
		return nil
	}
	title, ok := metadata["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		return []Problem{problem(where, "front matter must define a non-empty title (Hextra renders it as the page <h1>)")}
	}
	var problems []Problem
	if _, shadowsSlug := metadata["url"]; shadowsSlug {
		problems = append(problems, problem(where, "front matter must not define url; it shadows the reviewed Hugo slug and permalink contract"))
	}
	isHome := filepath.Clean(path) == filepath.Join(root, "content", "_index.md")
	slug, hasSlug := metadata["slug"].(string)
	if isHome {
		if _, manufactured := metadata["slug"]; manufactured {
			problems = append(problems, problem(where, "the home page stays at / and must not manufacture a slug"))
		}
	} else if !hasSlug || !permalinkSlug.MatchString(slug) {
		problems = append(problems, problem(where, "front matter slug must be explicit lowercase kebab-case, found %q", metadata["slug"]))
	}
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(line.text, "# ") {
			problems = append(problems, problem(where, "the page title lives in front matter; a Markdown H1 would publish a second <h1>"))
			break
		}
	}
	return problems
}

// checkHeadings requires a page to be sectioned, and nothing more for now.
//
// It used to require every H2 to end with a question mark, which turned all 76 pages
// into one flat FAQ where no heading could carry more weight than its neighbors. The
// teaching-style contract inverts that: a heading states what the section does, and one
// question per page is reserved for the tension the page actually resolves. The cap that
// enforces the new rule lands once every page has been rewritten to meet it; until then
// this stays permissive, so a rewritten page and an untouched one both pass.
func checkHeadings(where, text string) []Problem {
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(line.text, "## ") {
			return nil
		}
	}
	return []Problem{problem(where, "expected at least one H2 section heading")}
}

func checkGlance(where, text string) []Problem {
	inside := false
	seen := make(map[string]bool)
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(line.text, "## ") {
			break
		}
		if line.text == glanceOpen {
			inside = true
			continue
		}
		if inside {
			for _, field := range glanceFields {
				seen[field] = seen[field] || strings.HasPrefix(line.text, "- "+field)
			}
		}
	}
	if inside && len(seen) == len(glanceFields) {
		return nil
	}
	missing := make([]string, 0)
	for _, field := range glanceFields {
		if !seen[field] {
			missing = append(missing, field)
		}
	}
	return []Problem{problem(where, `expected an "In one glance" abstract block between the H1 and the first H2 (missing: %s)`, strings.Join(missing, ", "))}
}

func checkClosing(where, text string) []Problem {
	var headings []string
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(line.text, "## ") {
			headings = append(headings, line.text)
		}
	}
	if len(headings) == 0 || slices.Contains(closingHeads, headings[len(headings)-1]) {
		return nil
	}
	allowed := make([]string, len(closingHeads))
	for index, heading := range closingHeads {
		allowed[index] = fmt.Sprintf("%q", strings.TrimPrefix(heading, "## "))
	}
	return []Problem{problem(where, "last H2 must be one of %s, not: %s", strings.Join(allowed, " | "), headings[len(headings)-1])}
}

func checkKind(where, text string) []Problem {
	if declaredKind(text) != "" {
		return nil
	}
	return []Problem{problem(where, "the Time line must name exactly one page kind: %s", strings.Join(pageKinds, ", "))}
}
