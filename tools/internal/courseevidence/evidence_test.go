package courseevidence

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateAndVerifyBindEvidenceToCleanRevisionAndArtifacts(t *testing.T) {
	root := initializeRepository(t)
	artifact := filepath.Join(root, "input.txt")
	output := filepath.Join(root, ".evidence", "manifest.json")
	config := Config{
		Root:      root,
		Gates:     [][]string{{"gate", "one"}},
		Artifacts: []string{"input.txt"},
		Now:       func() time.Time { return time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC) },
		Run: func(_ context.Context, _ string, command []string, _, _ io.Writer) error {
			if strings.Join(command, " ") != "gate one" {
				t.Fatalf("command = %v", command)
			}
			return nil
		},
	}
	if err := Create(t.Context(), config, output); err != nil {
		t.Fatal(err)
	}
	if revision, err := Verify(t.Context(), config, output); err != nil || len(revision) != 40 {
		t.Fatalf("revision = %q, error = %v", revision, err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.TreeDigest, "sha256:") {
		t.Fatalf("tree digest = %q", manifest.TreeDigest)
	}
	if err := os.WriteFile(artifact, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(t.Context(), config, output); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateRejectsDirtyCheckoutBeforeRunningGates(t *testing.T) {
	root := initializeRepository(t)
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	config := Config{
		Root: root, Gates: [][]string{{"gate"}}, Artifacts: []string{"input.txt"}, Now: time.Now,
		Run: func(context.Context, string, []string, io.Writer, io.Writer) error { called = true; return nil },
	}
	if err := Create(t.Context(), config, filepath.Join(root, "manifest.json")); err == nil ||
		!strings.Contains(err.Error(), "dirty") || called {
		t.Fatalf("error = %v, called = %v", err, called)
	}
}

func initializeRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".evidence/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.name", "Course Evidence Test"},
		{"config", "user.email", "test@example.invalid"},
		{"add", "."},
		{"commit", "-m", "test: seed"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	return root
}
