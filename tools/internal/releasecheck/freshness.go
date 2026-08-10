package releasecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	freshnessTitlePrefix = "docs: freshness audit"
	minimumWaiverLength  = 24
	maximumWaiverLength  = 500
	maximumFreshnessAge  = 120 * 24 * time.Hour
)

var (
	issueEvidencePattern = regexp.MustCompile(`^issue:([1-9][0-9]*)$`)
	repositoryPattern    = regexp.MustCompile(`^[^/]+/[^/]+$`)
	spacePattern         = regexp.MustCompile(`\s+`)
)

type FreshnessInput struct {
	Evidence              string
	Actor                 string
	Repository            string
	Now                   time.Time
	Issue                 map[string]any
	ChecklistTemplateHTML string
}

type renderedTask struct {
	label   string
	checked bool
}

func ValidateFreshnessHandoff(input FreshnessInput) (map[string]any, error) {
	if strings.TrimSpace(input.Actor) == "" || !repositoryPattern.MatchString(input.Repository) {
		return nil, errors.New("release actor and owner/repository are required")
	}
	now := input.Now.UTC()

	if match := issueEvidencePattern.FindStringSubmatch(input.Evidence); match != nil {
		return validateFreshnessIssue(input, match[1], now)
	}
	if strings.HasPrefix(input.Evidence, "waiver:") {
		reason := strings.TrimSpace(strings.TrimPrefix(input.Evidence, "waiver:"))
		if strings.Contains(reason, "\n") || len(reason) < minimumWaiverLength || len(reason) > maximumWaiverLength {
			return nil, errors.New("freshness waiver needs a single-line reason between 24 and 500 characters")
		}
		return map[string]any{
			"schema_version": 2,
			"kind":           "waiver",
			"reason":         reason,
			"selected_by":    input.Actor,
			"review_gate":    "protected release environment",
		}, nil
	}
	return nil, errors.New("freshness evidence must be issue:<number> or waiver:<reviewed reason>")
}

func validateFreshnessIssue(input FreshnessInput, rawNumber string, now time.Time) (map[string]any, error) {
	number := 0
	if _, err := fmt.Sscan(rawNumber, &number); err != nil {
		return nil, fmt.Errorf("parsing freshness issue number: %w", err)
	}
	issueNumber, ok := positiveInteger(input.Issue["number"])
	if input.Issue == nil || !ok || issueNumber != int64(number) {
		return nil, errors.New("freshness handoff does not match the fetched issue")
	}
	if input.Issue["state"] != "closed" || input.Issue["pull_request"] != nil {
		return nil, errors.New("freshness evidence must be a closed issue, not a pull request")
	}
	title, _ := input.Issue["title"].(string)
	if !strings.HasPrefix(title, freshnessTitlePrefix) {
		return nil, fmt.Errorf("freshness issue title must start with %q", freshnessTitlePrefix)
	}
	if !slices.Contains(labelNames(input.Issue), "documentation") {
		return nil, errors.New("freshness issue must carry the documentation label")
	}
	body, _ := input.Issue["body"].(string)
	if strings.TrimSpace(body) == "" {
		return nil, errors.New("freshness issue must contain its reviewed checklist")
	}
	bodyHTML, _ := input.Issue["body_html"].(string)
	if strings.TrimSpace(bodyHTML) == "" {
		return nil, errors.New("freshness issue must include GitHub-rendered HTML")
	}
	if strings.TrimSpace(input.ChecklistTemplateHTML) == "" {
		return nil, errors.New("rendered freshness checklist template is required for issue evidence")
	}
	templateItems, err := renderedTaskItems(input.ChecklistTemplateHTML)
	if err != nil {
		return nil, err
	}
	expected := make([]string, len(templateItems))
	for index, item := range templateItems {
		if item.checked {
			return nil, errors.New("freshness checklist template must contain only unchecked task items")
		}
		expected[index] = item.label
	}
	if len(expected) == 0 {
		return nil, errors.New("freshness checklist template contains no task items")
	}
	reviewed, err := renderedTaskItems(bodyHTML)
	if err != nil {
		return nil, err
	}
	if len(reviewed) == 0 {
		return nil, errors.New("freshness issue must contain GitHub-rendered task items")
	}
	reviewedLabels := make([]string, len(reviewed))
	for index, item := range reviewed {
		if !item.checked {
			return nil, errors.New("freshness issue still contains unchecked checklist items")
		}
		reviewedLabels[index] = item.label
	}
	if !sameStringInventory(expected, reviewedLabels) {
		return nil, errors.New("freshness issue does not match the exact current checklist inventory")
	}
	closedAtRaw, _ := input.Issue["closed_at"].(string)
	closedAt, err := time.Parse(time.RFC3339, closedAtRaw)
	if err != nil {
		return nil, errors.New("freshness issue close timestamp is invalid")
	}
	age := now.Sub(closedAt)
	if age < 0 || age > maximumFreshnessAge {
		return nil, errors.New("freshness issue must have been reviewed and closed within 120 days")
	}
	url, _ := input.Issue["html_url"].(string)
	if !strings.HasPrefix(url, "https://github.com/"+input.Repository+"/issues/") {
		return nil, errors.New("freshness issue URL does not belong to this repository")
	}
	closedBy, _ := input.Issue["closed_by"].(map[string]any)
	reviewer, _ := closedBy["login"].(string)
	if reviewer == "" {
		return nil, errors.New("freshness issue does not record who closed the review")
	}
	return map[string]any{
		"schema_version":   2,
		"kind":             "issue",
		"issue_number":     number,
		"checklist_items":  len(expected),
		"checklist_sha256": checklistDigest(expected),
		"closed_at":        closedAt.UTC().Format(time.RFC3339),
		"reviewed_by":      reviewer,
		"selected_by":      input.Actor,
	}, nil
}

func renderedTaskItems(document string) ([]renderedTask, error) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return nil, fmt.Errorf("parsing rendered freshness checklist: %w", err)
	}
	items := make([]renderedTask, 0)
	var walk func(*html.Node) error
	walk = func(node *html.Node) error {
		if node.Type == html.ElementNode && node.Data == "li" && hasClass(node, "task-list-item") {
			item, parseErr := parseTaskItem(node)
			if parseErr != nil {
				return parseErr
			}
			items = append(items, item)
			return nil
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return items, nil
}

func parseTaskItem(node *html.Node) (renderedTask, error) {
	inputs := make([]*html.Node, 0, 1)
	var findInputs func(*html.Node)
	findInputs = func(candidate *html.Node) {
		if candidate.Type == html.ElementNode && candidate.Data == "input" {
			inputs = append(inputs, candidate)
		}
		for child := candidate.FirstChild; child != nil; child = child.NextSibling {
			findInputs(child)
		}
	}
	findInputs(node)
	if len(inputs) != 1 || attr(inputs[0], "type") != "checkbox" || !hasAttr(inputs[0], "disabled") ||
		!hasClass(inputs[0], "task-list-item-checkbox") {
		return renderedTask{}, errors.New("rendered freshness checklist contains a malformed task item")
	}
	var text strings.Builder
	var collectText func(*html.Node)
	collectText = func(candidate *html.Node) {
		if candidate == inputs[0] {
			return
		}
		if candidate.Type == html.TextNode {
			text.WriteString(candidate.Data)
		}
		for child := candidate.FirstChild; child != nil; child = child.NextSibling {
			collectText(child)
		}
	}
	collectText(node)
	label := strings.TrimSpace(spacePattern.ReplaceAllString(text.String(), " "))
	if label == "" {
		return renderedTask{}, errors.New("rendered freshness checklist contains a malformed task item")
	}
	return renderedTask{checked: hasAttr(inputs[0], "checked"), label: label}, nil
}

func hasClass(node *html.Node, wanted string) bool {
	return slices.Contains(strings.Fields(attr(node, "class")), wanted)
}

func attr(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func hasAttr(node *html.Node, key string) bool {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return true
		}
	}
	return false
}

func labelNames(issue map[string]any) []string {
	labels, _ := issue["labels"].([]any)
	names := make([]string, 0, len(labels))
	for _, raw := range labels {
		label, _ := raw.(map[string]any)
		if name, ok := label["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func sameStringInventory(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func checklistDigest(labels []string) string {
	payload, _ := json.Marshal(labels)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func positiveInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case float64:
		return int64(typed), typed > 0 && typed == float64(int64(typed))
	default:
		return 0, false
	}
}
