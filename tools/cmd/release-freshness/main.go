package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/releasecheck"
)

func main() {
	evidence := flag.String("evidence", "", "issue:<number> or waiver:<reviewed reason>")
	actor := flag.String("actor", "", "release operator")
	repository := flag.String("repository", "", "owner/repository")
	issuePath := flag.String("issue-json", "", "GitHub issue response JSON")
	templatePath := flag.String("checklist-template-html", "", "GitHub-rendered issue-template HTML")
	flag.Parse()

	var issue map[string]any
	var template string
	if *issuePath != "" {
		issue = mustObject(*issuePath)
		if *templatePath == "" {
			fail(errors.New("rendered freshness checklist template is required with issue evidence"))
		}
		content, err := os.ReadFile(*templatePath)
		if err != nil {
			fail(fmt.Errorf("reading %s: %w", *templatePath, err))
		}
		template = string(content)
	}
	result, err := releasecheck.ValidateFreshnessHandoff(releasecheck.FreshnessInput{
		Evidence: *evidence, Actor: *actor, Repository: *repository, Now: time.Now(),
		Issue: issue, ChecklistTemplateHTML: template,
	})
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(encoded))
}

func mustObject(path string) map[string]any {
	content, err := os.ReadFile(path)
	if err != nil {
		fail(fmt.Errorf("reading %s: %w", path, err))
	}
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		fail(fmt.Errorf("decoding %s: %w", path, err))
	}
	object, ok := document.(map[string]any)
	if !ok {
		fail(fmt.Errorf("%s must contain one JSON object", path))
	}
	return object
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
