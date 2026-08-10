package conventions

import (
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Hugo honors both shortcode delimiters, arbitrary whitespace including newlines, either
// attribute order, and quoted, backtick-quoted, or bare values. Matching only the canonical
// spelling meant every other spelling the site renders for real passed unchecked, so match
// the call first and read its attributes separately. snippetIncludeOpener catches a call
// that merely starts on a line, which is what the fenced-block guard needs.
var (
	snippetIncludePattern = regexp.MustCompile(`(?s)\{\{[<%]\s*include(\s.*?|\s*)(?:/\s*)?[>%]\}\}`)
	snippetIncludeOpener  = regexp.MustCompile(`\{\{[<%]\s*include(?:\s|/|[>%]\}|$)`)
	shortcodeParamPattern = regexp.MustCompile("(?s)([A-Za-z][A-Za-z0-9_-]*)\\s*=\\s*(?:\"([^\"]*)\"|`([^`]*)`|([^\\s]+))")
)

// shortcodeParam returns the value Hugo would hand `.Get name`, and whether the call set it.
func shortcodeParam(arguments, name string) (string, bool) {
	for _, match := range shortcodeParamPattern.FindAllStringSubmatch(arguments, -1) {
		if match[1] != name {
			continue
		}
		// Exactly one of the quoted, backtick, or bare alternatives captures; an all-empty
		// match is a genuine `region=""`, which the caller must still see as set.
		for _, group := range match[2:] {
			if group != "" {
				return group, true
			}
		}
		return "", true
	}
	return "", false
}

// lineNumber converts a byte offset in text into the 1-based line the offset sits on.
func lineNumber(text string, offset int) int {
	return 1 + strings.Count(text[:offset], "\n")
}

func checkCollapsibles(where, text string) []Problem {
	var problems []Problem
	count := 0
	for _, line := range splitLines(text) {
		match := collapsiblePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		count++
		if !strings.HasPrefix(match[1], "Deeper: ") {
			problems = append(problems, problem(where, `collapsible summary must start with "Deeper: ": %s`, match[1]))
		}
	}
	if count > maxCollapsible {
		problems = append(problems, problem(where, "%d collapsibles; at most %d keep a page readable", count, maxCollapsible))
	}
	return problems
}

func checkLinkLabels(where, text string) []Problem {
	matches := bareNumberLabel.FindAllStringSubmatch(text, -1)
	problems := make([]Problem, 0, len(matches))
	for _, match := range matches {
		problems = append(problems, problem(where, "link label must be the page name, not a bare number: [%s]", match[1]))
	}
	return problems
}

func checkSnippets(where, text string) []Problem {
	var problems []Problem
	for _, line := range linesInsideFences(text) {
		if snippetIncludeOpener.MatchString(line.text) {
			problems = append(problems, problem(where, "line %d: include shortcode must not sit inside a fenced code block", line.number))
		}
	}
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(strings.TrimSpace(line.text), "--8<--") {
			problems = append(problems, problem(where, "line %d: Material snippet syntax is retired; use the include shortcode", line.number))
		}
	}
	return problems
}

func checkSnippetTargets(root, where, text string) []Problem {
	var problems []Problem
	// Scanned over the whole text rather than line by line: a shortcode call may legally
	// span several lines, which a per-line loop cannot see at all.
	for _, location := range snippetIncludePattern.FindAllStringSubmatchIndex(text, -1) {
		arguments := text[location[2]:location[3]]
		line := lineNumber(text, location[0])
		relative, hasPath := shortcodeParam(arguments, "path")
		region, hasRegion := shortcodeParam(arguments, "region")
		if !hasPath {
			problems = append(problems, problem(where, "line %d: include shortcode must name a source path", line))
			continue
		}
		cleaned := filepath.Clean(filepath.FromSlash(relative))
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			problems = append(problems, problem(where, "line %d: snippet source is outside the repository: %q", line, relative))
			continue
		}
		source, err := readFile(filepath.Join(root, cleaned))
		if err != nil {
			problems = append(problems, problem(where, "line %d: snippet source does not exist: %q", line, relative))
			continue
		}
		if !hasRegion || region == "" {
			problems = append(problems, problem(where, "line %d: trusted snippet must name a source region: %q", line, relative))
			continue
		}
		start := "--8<-- [start:" + region + "]"
		end := "--8<-- [end:" + region + "]"
		if strings.Count(source, start) != 1 || strings.Count(source, end) != 1 {
			problems = append(problems, problem(where, "line %d: snippet region %q needs exactly one start and end marker in %s", line, region, relative))
		} else if strings.Index(source, start) >= strings.Index(source, end) {
			problems = append(problems, problem(where, "line %d: snippet region %q ends before it starts", line, region))
		}
	}
	return problems
}

func checkGoBlocks(where, text string) []Problem {
	if !strings.HasPrefix(where, "content/2. ") && !strings.HasPrefix(where, "content/3. ") {
		return nil
	}
	var undeclared []string
	for _, block := range fencedBlocks(text, "go") {
		var first string
		for _, line := range block.body {
			if strings.TrimSpace(line) != "" {
				first = strings.TrimSpace(line)
				break
			}
		}
		if !strings.HasPrefix(first, "// simplified") && !strings.HasPrefix(first, "// illustrative") && !strings.HasPrefix(first, "// pseudocode") {
			undeclared = append(undeclared, strconv.Itoa(block.start))
		}
	}
	if len(undeclared) == 0 {
		return nil
	}
	return []Problem{problem(where, "%d unowned go blocks (lines %s): make it an include of the real source, or open the block with a `// simplified` comment", len(undeclared), strings.Join(undeclared, ", "))}
}

func checkAdmonitions(where, text string) []Problem {
	var problems []Problem
	for _, line := range linesOutsideFences(text) {
		match := admonitionPattern.FindStringSubmatch(line.text)
		if match != nil && !allowedAdmonitions[match[1]] {
			problems = append(problems, problem(where, "line %d: unsupported admonition type %q", line.number, match[1]))
		}
	}
	return problems
}

func checkOrderedLists(where, text string) []Problem {
	var problems []Problem
	for _, line := range linesOutsideFences(text) {
		match := orderedItemPattern.FindStringSubmatch(line.text)
		if match != nil && match[2] != "1" {
			problems = append(problems, problem(where, "line %d: ordered Markdown items must use `1.`, not `%s.`", line.number, match[2]))
		}
	}
	return problems
}

func checkPageLinkTargets(where, text string) []Problem {
	var problems []Problem
	for _, match := range fullPageLink.FindAllStringSubmatch(text, -1) {
		target, err := url.PathUnescape(match[2])
		if err != nil {
			target = match[2]
		}
		stem := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
		if match[1] != stem && !strings.HasPrefix(match[1], stem+" —") {
			problems = append(problems, problem(where, "link label %q targets page %q; use the target's full page name", match[1], stem))
		}
	}
	return problems
}

func checkHandsOnAction(where, text string) []Problem {
	if declaredKind(text) != "hands-on" {
		return nil
	}
	var headings []int
	for _, line := range linesOutsideFences(text) {
		if strings.HasPrefix(line.text, "## ") {
			headings = append(headings, line.number)
		}
	}
	limit := len(splitLines(text)) + 1
	if len(headings) > 2 {
		limit = headings[2]
	}
	for _, language := range []string{"bash", "console", "shell", "sh"} {
		for _, block := range fencedBlocks(text, language) {
			if block.start < limit {
				return nil
			}
		}
	}
	return []Problem{problem(where, "hands-on page must reach a bash/shell/console command within its first two H2 sections")}
}

func exerciseSections(text string) []fencedBlock {
	lines := splitLines(text)
	var result []fencedBlock
	for index, line := range lines {
		if !strings.HasPrefix(line, "## Your turn:") && !strings.HasPrefix(line, "Exercise:") && !strings.HasPrefix(line, "Optional exercise:") {
			continue
		}
		end := len(lines)
		for cursor := index + 1; cursor < len(lines); cursor++ {
			if strings.HasPrefix(lines[cursor], "## ") {
				end = cursor
				break
			}
		}
		result = append(result, fencedBlock{start: index + 1, end: end, body: lines[index:end]})
	}
	return result
}

func checkExercises(where, text string) []Problem {
	fields := [][]string{
		{"**Mode**:"},
		{"**Goal**:"},
		{"**Files to touch**:"},
		{"**Preflight**:"},
		{"**Gate that proves completion**:", "**Proof of completion**:"},
		{"**Final state**:"},
	}
	var problems []Problem
	for _, block := range exerciseSections(text) {
		section := strings.Join(block.body, "\n")
		for _, alternatives := range fields {
			found := false
			for _, field := range alternatives {
				found = found || strings.Contains(section, field)
			}
			if !found {
				problems = append(problems, problem(where, "line %d: exercise is missing %s", block.start, strings.Join(alternatives, " or ")))
			}
		}
		mode := exerciseMode.FindStringSubmatch(section)
		if mode != nil && mode[1] == "temporary experiment" {
			if !strings.Contains(section, "git diff --quiet --") && !strings.Contains(section, "test ! -e ") {
				problems = append(problems, problem(where, "line %d: temporary experiment needs a target-specific dirty preflight", block.start))
			}
			if !strings.Contains(section, "git restore --") && !strings.Contains(section, "rm --") {
				problems = append(problems, problem(where, "line %d: temporary experiment needs target-specific cleanup", block.start))
			}
		}
		if strings.Contains(section, "git checkout --") {
			problems = append(problems, problem(where, "line %d: use scoped `git restore -- <files>` instead of deprecated checkout cleanup", block.start))
		}
		if directoryRestore.MatchString(section) {
			problems = append(problems, problem(where, "line %d: cleanup restores a directory and can discard unrelated learner work", block.start))
		}
		if probabilistic.MatchString(section) && mandatoryRed.MatchString(section) {
			problems = append(problems, problem(where, "line %d: probabilistic evidence cannot satisfy a mandatory red-state claim", block.start))
		}
	}
	return problems
}

func checkDiagramAlternatives(where, text string, legacy map[string]bool) ([]Problem, map[string]bool) {
	lines := splitLines(text)
	used := make(map[string]bool)
	var problems []Problem
	for _, block := range fencedBlocks(text, "mermaid") {
		from := max(0, block.start-5)
		to := min(len(lines), block.end+5)
		digest := digestLines(block.body)
		if legacy[digest] {
			used[digest] = true
		}
		if strings.Contains(strings.Join(lines[from:to], "\n"), "**Diagram in words:**") || legacy[digest] {
			continue
		}
		problems = append(problems, problem(where, "line %d: Mermaid diagram needs adjacent `**Diagram in words:**` prose", block.start))
	}
	return problems, used
}

func checkMachinePaths(where, text string) []Problem {
	var problems []Problem
	for index, line := range splitLines(text) {
		if machinePathPattern.MatchString(line) {
			problems = append(problems, problem(where, "line %d: found a machine-specific path or obsolete registry hostname", index+1))
		}
	}
	return problems
}

func checkExactCountClaims(where, text string) []Problem {
	pattern := regexp.MustCompile(`(?i)\bexactly\s+(?:\d+|one|two|three|four|five|six|seven|eight|nine|ten)\s+(?:lines?|tests?|modules?)\b`)
	var problems []Problem
	for _, line := range linesOutsideFences(text) {
		if pattern.MatchString(line.text) {
			problems = append(problems, problem(where, "line %d: replace brittle exact line/test/module count with derived or count-free evidence", line.number))
		}
	}
	return problems
}

func mergeUsed(target, source map[string]bool) {
	for value := range source {
		target[value] = true
	}
}

func sortedDifference(left, right map[string]bool) []string {
	var values []string
	for value := range left {
		if !right[value] {
			values = append(values, value)
		}
	}
	slices.Sort(values)
	return values
}
