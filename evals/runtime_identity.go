package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	containerModeLabel           = "dev.fmind.agentops.build-mode"
	containerSourceIdentityLabel = "dev.fmind.agentops.source-identity"
	containerRevisionLabel       = "dev.fmind.agentops.source-revision"
	containerTreeDigestLabel     = "dev.fmind.agentops.source-tree-digest"
	containerDirtyLabel          = "dev.fmind.agentops.source-dirty"
)

type runtimeCommand func(context.Context, string, ...string) ([]byte, error)

type runtimeVersion struct {
	Mode           *SourceMode `json:"mode"`
	SourceIdentity *string     `json:"source_identity"`
	Revision       *string     `json:"revision"`
	TreeDigest     *string     `json:"tree_digest"`
	Dirty          *bool       `json:"dirty"`
}

type containerImageInspect struct {
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	ID string `json:"Id"`
}

func verifyProcessRuntimeIdentity(
	ctx context.Context, binary string, expected SourceEvidence, run runtimeCommand,
) error {
	if strings.TrimSpace(binary) == "" {
		return errors.New("process runtime identity needs an agent binary")
	}
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("validate expected process source: %w", err)
	}
	if run == nil {
		return errors.New("process runtime identity command is unavailable")
	}
	output, err := run(ctx, binary, "version")
	if err != nil {
		return fmt.Errorf("query process runtime identity: %w", err)
	}
	actual, err := decodeRuntimeVersion(output)
	if err != nil {
		return fmt.Errorf("decode process runtime identity: %w", err)
	}
	if err := compareRuntimeSource(expected, actual); err != nil {
		return fmt.Errorf("process binary source does not match evaluated checkout: %w", err)
	}
	return nil
}

func verifyContainerRuntimeIdentity(
	ctx context.Context, engine, image string, expected SourceEvidence, run runtimeCommand,
) error {
	_, err := resolveContainerRuntimeIdentity(ctx, engine, image, expected, run)
	return err
}

func resolveContainerRuntimeIdentity(
	ctx context.Context, engine, image string, expected SourceEvidence, run runtimeCommand,
) (string, error) {
	if strings.TrimSpace(engine) == "" || strings.TrimSpace(image) == "" {
		return "", errors.New("container runtime identity needs an engine and image")
	}
	if err := expected.Validate(); err != nil {
		return "", fmt.Errorf("validate expected container source: %w", err)
	}
	if run == nil {
		return "", errors.New("container runtime identity command is unavailable")
	}

	inspectOutput, err := run(ctx, engine, "image", "inspect", image)
	if err != nil {
		return "", fmt.Errorf("inspect container image identity: %w", err)
	}
	inspected, err := decodeContainerImageInspect(inspectOutput)
	if err != nil {
		return "", err
	}
	// Resolve a mutable tag once, then execute the immutable image ID whose
	// labels were inspected so the two provenance surfaces cannot cross images.
	versionOutput, versionErr := run(
		ctx, engine, "run", "--rm", "--network", "none", "--read-only", inspected.ID, "version",
	)
	if versionErr != nil {
		return "", fmt.Errorf("query container binary identity: %w", versionErr)
	}
	actual, decodeErr := decodeRuntimeVersion(versionOutput)
	if decodeErr != nil {
		return "", fmt.Errorf("decode container binary identity: %w", decodeErr)
	}
	if err := errors.Join(
		compareContainerLabels(expected, inspected.Config.Labels),
		compareRuntimeSource(expected, actual),
	); err != nil {
		return "", err
	}
	return inspected.ID, nil
}

func decodeRuntimeVersion(output []byte) (SourceEvidence, error) {
	var wire runtimeVersion
	if err := json.Unmarshal(output, &wire); err != nil {
		return SourceEvidence{}, fmt.Errorf("version output is not JSON: %w", err)
	}
	if wire.Mode == nil || wire.SourceIdentity == nil || wire.TreeDigest == nil || wire.Dirty == nil {
		return SourceEvidence{}, errors.New("version output has an incomplete source tuple")
	}
	revision := ""
	if wire.Revision != nil {
		revision = *wire.Revision
	}
	evidence := SourceEvidence{
		Mode: *wire.Mode, Identity: *wire.SourceIdentity, Revision: revision,
		TreeDigest: *wire.TreeDigest, Dirty: *wire.Dirty,
	}
	if err := evidence.Validate(); err != nil {
		return SourceEvidence{}, fmt.Errorf("version output has an invalid source tuple: %w", err)
	}
	return evidence, nil
}

func decodeContainerImageInspect(output []byte) (containerImageInspect, error) {
	var images []containerImageInspect
	if err := json.Unmarshal(output, &images); err != nil {
		return containerImageInspect{}, fmt.Errorf("decode container image identity: %w", err)
	}
	if len(images) != 1 || strings.TrimSpace(images[0].ID) == "" {
		return containerImageInspect{}, fmt.Errorf(
			"container image inspection returned %d images or an empty image ID", len(images),
		)
	}
	return images[0], nil
}

func compareRuntimeSource(expected, actual SourceEvidence) error {
	var problems []error
	if actual.Mode != expected.Mode {
		problems = append(problems, fmt.Errorf("mode=%q, want %q", actual.Mode, expected.Mode))
	}
	if actual.Identity != expected.Identity {
		problems = append(problems, fmt.Errorf("source_identity=%q, want %q", actual.Identity, expected.Identity))
	}
	if actual.Revision != expected.Revision {
		problems = append(problems, fmt.Errorf("revision=%q, want %q", actual.Revision, expected.Revision))
	}
	if actual.TreeDigest != expected.TreeDigest {
		problems = append(problems, fmt.Errorf("tree_digest=%q, want %q", actual.TreeDigest, expected.TreeDigest))
	}
	if actual.Dirty != expected.Dirty {
		problems = append(problems, fmt.Errorf("dirty=%t, want %t", actual.Dirty, expected.Dirty))
	}
	return errors.Join(problems...)
}

func compareContainerLabels(expected SourceEvidence, labels map[string]string) error {
	want := map[string]string{
		containerModeLabel:           string(expected.Mode),
		containerSourceIdentityLabel: expected.Identity,
		containerRevisionLabel:       expected.Revision,
		containerTreeDigestLabel:     expected.TreeDigest,
		containerDirtyLabel:          strconv.FormatBool(expected.Dirty),
	}
	var problems []error
	for _, key := range []string{
		containerModeLabel, containerSourceIdentityLabel, containerRevisionLabel,
		containerTreeDigestLabel, containerDirtyLabel,
	} {
		value, found := labels[key]
		if !found {
			problems = append(problems, fmt.Errorf("container label %s is missing", key))
			continue
		}
		if value != want[key] {
			problems = append(problems, fmt.Errorf("container label %s=%q, want %q", key, value, want[key]))
		}
	}
	return errors.Join(problems...)
}

func executeRuntimeCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		stderr := strings.TrimSpace(string(exitError.Stderr))
		if stderr != "" {
			return nil, fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return nil, err
}

func snapshotRuntimeBinary(source, directory string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open runtime binary: %w", err)
	}
	defer func() { _ = input.Close() }()
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return "", errors.New("runtime binary must be a regular file")
	}
	destination := filepath.Join(directory, "agent-evaluated")
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return "", fmt.Errorf("create runtime binary snapshot: %w", err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	after, statErr := input.Stat()
	if err := errors.Join(copyErr, closeErr, statErr); err != nil {
		_ = os.Remove(destination)
		return "", fmt.Errorf("copy runtime binary snapshot: %w", err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		_ = os.Remove(destination)
		return "", errors.New("runtime binary changed while it was snapshotted")
	}
	return destination, nil
}
