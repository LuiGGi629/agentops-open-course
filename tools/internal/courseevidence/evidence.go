package courseevidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/sourceidentity"
)

const (
	formatVersion = 2
	courseID      = "MLOps-Courses/agentops-open-course-go"
	claim         = "deterministic offline course gates at the recorded source revision"
)

var (
	DefaultGates     = [][]string{{"mise", "run", "check:core"}, {"mise", "run", "test"}}
	DefaultArtifacts = []string{
		"mise.lock",
		"agents/go/go.sum",
		"evals/go.sum",
		"tools/go.sum",
		"agents/data/incidents.db",
		"evals/ops.evalset.json",
	}
)

type GateResult struct {
	Command string `json:"command"`
	Result  string `json:"result"`
}

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Course      string       `json:"course"`
	Revision    string       `json:"revision"`
	TreeDigest  string       `json:"tree_digest"`
	GeneratedAt string       `json:"generated_at"`
	Claim       string       `json:"claim"`
	Gates       []GateResult `json:"gates"`
	Artifacts   []Artifact   `json:"artifacts"`
	Format      int          `json:"format"`
}

type Config struct {
	Now       func() time.Time
	Run       func(context.Context, string, []string, io.Writer, io.Writer) error
	Root      string
	Gates     [][]string
	Artifacts []string
}

func DefaultConfig(root string) Config {
	return Config{
		Root: root, Gates: cloneCommands(DefaultGates), Artifacts: append([]string(nil), DefaultArtifacts...),
		Now: time.Now, Run: runCommand,
	}
}

func Create(ctx context.Context, config Config, output string) error {
	identity, err := sourceidentity.Resolve(ctx, config.Root, sourceidentity.Release)
	if err != nil {
		return err
	}
	gates, err := runGates(ctx, config)
	if err != nil {
		return err
	}
	artifacts, err := artifactRecords(config.Root, config.Artifacts)
	if err != nil {
		return err
	}
	manifest := Manifest{
		Format: formatVersion, Course: courseID, Revision: identity.Revision, TreeDigest: identity.TreeDigest,
		GeneratedAt: config.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Claim:       claim, Gates: gates, Artifacts: artifacts,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding course evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return fmt.Errorf("creating the course-evidence directory: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(output, encoded, 0o600); err != nil {
		return fmt.Errorf("writing course evidence %s: %w", output, err)
	}
	return nil
}

func Verify(ctx context.Context, config Config, manifestPath string) (string, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fmt.Errorf("reading course evidence %s: %w", manifestPath, err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&manifest); decodeErr != nil {
		return "", fmt.Errorf("decoding course evidence: %w", decodeErr)
	}
	if manifest.Format != formatVersion || manifest.Course != courseID || manifest.Claim != claim {
		return "", errors.New("manifest format, course, or claim does not match this repository")
	}
	identity, err := sourceidentity.Resolve(ctx, config.Root, sourceidentity.Release)
	if err != nil {
		return "", err
	}
	if manifest.Revision != identity.Revision || manifest.TreeDigest != identity.TreeDigest {
		return "", fmt.Errorf(
			"manifest source identity %q (%s) does not match this checkout %s (%s)",
			manifest.Revision, manifest.TreeDigest, identity.Revision, identity.TreeDigest,
		)
	}
	expectedGates := gateInventory(config.Gates)
	if !equalGates(manifest.Gates, expectedGates) {
		return "", errors.New("manifest gate inventory does not match this revision's course contract")
	}
	expectedArtifacts, err := artifactRecords(config.Root, config.Artifacts)
	if err != nil {
		return "", err
	}
	if !equalArtifacts(manifest.Artifacts, expectedArtifacts) {
		return "", errors.New("manifest artifact hashes do not match this checkout")
	}
	if _, err := runGates(ctx, config); err != nil {
		return "", err
	}
	return identity.Revision, nil
}

func runGates(ctx context.Context, config Config) ([]GateResult, error) {
	results := make([]GateResult, 0, len(config.Gates))
	for _, command := range config.Gates {
		if len(command) == 0 {
			return nil, errors.New("course evidence contains an empty gate command")
		}
		if err := config.Run(ctx, config.Root, command, os.Stdout, os.Stderr); err != nil {
			return nil, fmt.Errorf("%s failed: %w", strings.Join(command, " "), err)
		}
		results = append(results, GateResult{Command: strings.Join(command, " "), Result: "passed"})
	}
	return results, nil
}

func artifactRecords(root string, paths []string) ([]Artifact, error) {
	records := make([]Artifact, 0, len(paths))
	for _, relative := range paths {
		file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return nil, fmt.Errorf("opening evidence artifact %s: %w", relative, err)
		}
		digest := sha256.New()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return nil, fmt.Errorf("hashing evidence artifact %s: %w", relative, err)
		}
		records = append(records, Artifact{Path: relative, SHA256: hex.EncodeToString(digest.Sum(nil))})
	}
	return records, nil
}

func runCommand(ctx context.Context, root string, command []string, stdout, stderr io.Writer) error {
	process := exec.CommandContext(ctx, command[0], command[1:]...)
	process.Dir, process.Stdout, process.Stderr = root, stdout, stderr
	return process.Run()
}

func gateInventory(commands [][]string) []GateResult {
	results := make([]GateResult, len(commands))
	for index, command := range commands {
		results[index] = GateResult{Command: strings.Join(command, " "), Result: "passed"}
	}
	return results
}

func equalGates(left, right []GateResult) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalArtifacts(left, right []Artifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneCommands(commands [][]string) [][]string {
	cloned := make([][]string, len(commands))
	for index, command := range commands {
		cloned[index] = append([]string(nil), command...)
	}
	return cloned
}
