package releasecheck

import (
	"fmt"
	"html"
	"strings"
	"testing"
	"time"
)

var freshnessNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

const freshnessRepository = "MLOps-Courses/agentops-open-course-go"

func taskHTML(label string, checked bool) string {
	state := "Incomplete"
	checkedAttribute := ""
	if checked {
		state = "Completed"
		checkedAttribute = ` checked=""`
	}
	return fmt.Sprintf(`<li class="task-list-item"><input type="checkbox" disabled="" `+
		`class="task-list-item-checkbox" aria-label="%s task"%s> %s</li>`,
		state, checkedAttribute, html.EscapeString(label))
}

func taskList(checked bool) string {
	return "<ul>" + taskHTML("Provider names", checked) + taskHTML("Prices and cost inputs", checked) + "</ul>"
}

func freshnessIssue() map[string]any {
	return map[string]any{
		"number":    float64(113),
		"state":     "closed",
		"title":     "docs: freshness audit for 2026-Q3",
		"body":      "## Reviewed\n\n- [x] Provider names\n- [x] Prices and cost inputs\n",
		"body_html": taskList(true),
		"closed_at": "2026-07-30T10:00:00Z",
		"closed_by": map[string]any{"login": "maintainer"},
		"html_url":  "https://github.com/" + freshnessRepository + "/issues/113",
		"labels":    []any{map[string]any{"name": "documentation"}},
	}
}

func validFreshnessInput() FreshnessInput {
	return FreshnessInput{
		Evidence: "issue:113", Actor: "release-operator", Repository: freshnessRepository,
		Now: freshnessNow, Issue: freshnessIssue(), ChecklistTemplateHTML: taskList(false),
	}
}

func TestValidateFreshnessHandoffSanitizesRecentIssue(t *testing.T) {
	result, err := ValidateFreshnessHandoff(validFreshnessInput())
	if err != nil {
		t.Fatal(err)
	}
	if result["kind"] != "issue" || result["issue_number"] != 113 || result["checklist_items"] != 2 ||
		result["reviewed_by"] != "maintainer" || result["selected_by"] != "release-operator" {
		t.Fatalf("result = %#v", result)
	}
	if _, containsURL := result["url"]; containsURL {
		t.Fatalf("sanitized freshness evidence contains a URL: %#v", result)
	}
}

func TestValidateFreshnessHandoffRejectsStaleIncompleteOrMismatchedIssue(t *testing.T) {
	tests := []struct {
		edit func(*FreshnessInput)
		name string
	}{
		{name: "open", edit: func(input *FreshnessInput) { input.Issue["state"] = "open" }},
		{name: "stale", edit: func(input *FreshnessInput) { input.Issue["closed_at"] = "2026-01-01T00:00:00Z" }},
		{name: "unlabelled", edit: func(input *FreshnessInput) { input.Issue["labels"] = []any{} }},
		{name: "no body", edit: func(input *FreshnessInput) { input.Issue["body"] = nil }},
		{name: "no rendered tasks", edit: func(input *FreshnessInput) { input.Issue["body_html"] = "<p>reviewed</p>" }},
		{name: "unchecked", edit: func(input *FreshnessInput) { input.Issue["body_html"] = taskList(false) }},
		{name: "extra", edit: func(input *FreshnessInput) {
			input.Issue["body_html"] = taskList(true) + taskHTML("Unreviewed extra claim", true)
		}},
		{name: "wrong issue", edit: func(input *FreshnessInput) { input.Issue["number"] = float64(114) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validFreshnessInput()
			test.edit(&input)
			if _, err := ValidateFreshnessHandoff(input); err == nil {
				t.Fatal("error = nil")
			}
		})
	}
}

func TestRenderedTaskItemsPreserveVisibleNestedText(t *testing.T) {
	document := `<ul><li class="task-list-item"><input type="checkbox" disabled="" ` +
		`class="task-list-item-checkbox" checked=""> Review <code>qwen3</code> &amp; ` +
		`<a href="https://example.com">source</a></li></ul>`
	items, err := renderedTaskItems(document)
	if err != nil || len(items) != 1 || !items[0].checked || items[0].label != "Review qwen3 & source" {
		t.Fatalf("items = %#v, error = %v", items, err)
	}
}

func TestRenderedTaskItemsFailClosedOnMalformedSignature(t *testing.T) {
	for _, document := range []string{
		`<li class="task-list-item">Provider names</li>`,
		`<li class="task-list-item"><input type="checkbox"> Provider names</li>`,
	} {
		if _, err := renderedTaskItems(document); err == nil {
			t.Fatalf("renderedTaskItems(%q) = nil error", document)
		}
	}
}

func TestValidateFreshnessHandoffAcceptsOnlyReviewedWaiver(t *testing.T) {
	input := validFreshnessInput()
	input.Issue = nil
	input.ChecklistTemplateHTML = ""
	input.Evidence = "waiver:Provider source was unavailable after two reviewed attempts."
	result, err := ValidateFreshnessHandoff(input)
	if err != nil || result["kind"] != "waiver" || result["review_gate"] != "protected release environment" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	for _, evidence := range []string{"113", "issue:0", "waiver:too short", "waiver:line one\nline two is long enough"} {
		input.Evidence = evidence
		if _, err := ValidateFreshnessHandoff(input); err == nil || strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("evidence %q: error = %v", evidence, err)
		}
	}
}
