package releasecheck

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersionDigestClassifierFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		versions   any
		wantOutput string
		wantPass   bool
	}{
		{"absent", []any{[]any{}}, "", true},
		{"exclusive", []any{[]any{map[string]any{
			"name":     testIndexDigest,
			"metadata": map[string]any{"container": map[string]any{"tags": []any{"v1.0.0"}}},
		}}}, testIndexDigest, true},
		{"shared", []any{[]any{map[string]any{
			"name":     "sha256:bad",
			"metadata": map[string]any{"container": map[string]any{"tags": []any{"latest", "v1.0.0"}}},
		}}}, "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, passed := runVersionDigestClassifier(t, test.versions)
			if passed != test.wantPass || strings.TrimSpace(output) != test.wantOutput {
				t.Fatalf("output = %q, passed = %v", output, passed)
			}
		})
	}
}

func runVersionDigestClassifier(t *testing.T, versions any) (string, bool) {
	t.Helper()
	fixture, err := json.Marshal(versions)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	gh := filepath.Join(directory, "gh")
	if writeErr := os.WriteFile(gh, []byte("#!/usr/bin/env bash\nprintf '%s\\n' \"$GH_FIXTURE\"\n"), 0o700); writeErr != nil {
		t.Fatal(writeErr)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
	command := exec.Command("bash", "-c", "source scripts/release-promote.sh; version_digest_for_tag agent")
	command.Dir = repository
	command.Env = append(os.Environ(),
		"GH_FIXTURE="+string(fixture),
		"GITHUB_REPOSITORY=MLOps-Courses/agentops-open-course-go",
		"PATH="+directory+":"+os.Getenv("PATH"),
		"VERSION=v1.0.0",
	)
	output, err := command.Output()
	return string(output), err == nil
}
