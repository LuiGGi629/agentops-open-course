package evals

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestProcessRuntimeIdentityRejectsStaleBinary(t *testing.T) {
	t.Parallel()

	source := testSourceEvidence()
	stale := source
	stale.TreeDigest = "sha256:" + strings.Repeat("c", 64)
	runner := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		if name != "/tmp/agent" || len(arguments) != 1 || arguments[0] != "version" {
			t.Fatalf("command = %q %q, want exact process binary version", name, arguments)
		}
		return runtimeVersionJSON(t, stale), nil
	}

	err := verifyProcessRuntimeIdentity(t.Context(), "/tmp/agent", source, runner)
	if err == nil || !strings.Contains(err.Error(), "tree_digest") {
		t.Fatalf("error = %v, want stale tree digest rejection", err)
	}
}

func TestProcessRuntimeIdentityAcceptsMatchingBinary(t *testing.T) {
	t.Parallel()

	source := testSourceEvidence()
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return runtimeVersionJSON(t, source), nil
	}
	if err := verifyProcessRuntimeIdentity(t.Context(), "/tmp/agent", source, runner); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRuntimeIdentityRejectsMalformedVersion(t *testing.T) {
	t.Parallel()

	runner := func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"mode":"release","dirty":false}`), nil
	}
	err := verifyProcessRuntimeIdentity(t.Context(), "/tmp/agent", testSourceEvidence(), runner)
	if err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v, want malformed version rejection", err)
	}
}

func TestContainerRuntimeIdentityRejectsStaleImageLabels(t *testing.T) {
	t.Parallel()

	source := testSourceEvidence()
	labels := runtimeSourceLabels(source)
	labels[containerTreeDigestLabel] = "sha256:" + strings.Repeat("c", 64)
	runner := fakeContainerIdentityRunner(t, source, labels)

	err := verifyContainerRuntimeIdentity(t.Context(), "docker", "agent:candidate", source, runner)
	if err == nil || !strings.Contains(err.Error(), containerTreeDigestLabel) {
		t.Fatalf("error = %v, want stale image-label rejection", err)
	}
}

func TestContainerRuntimeIdentityAcceptsMatchingBinaryAndLabels(t *testing.T) {
	t.Parallel()

	source := testSourceEvidence()
	if err := verifyContainerRuntimeIdentity(
		t.Context(), "docker", "agent:candidate", source,
		fakeContainerIdentityRunner(t, source, runtimeSourceLabels(source)),
	); err != nil {
		t.Fatal(err)
	}
}

func TestContainerRuntimeIdentityPinsMutableTagToInspectedImage(t *testing.T) {
	t.Parallel()

	source := testSourceEvidence()
	imageID, err := resolveContainerRuntimeIdentity(
		t.Context(), "docker", "agent:candidate", source,
		fakeContainerIdentityRunner(t, source, runtimeSourceLabels(source)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if imageID != "sha256:image-id" {
		t.Fatalf("resolved image = %q", imageID)
	}
}

func TestRuntimeVersionAllowsEmptyDirtyDevelopmentRevision(t *testing.T) {
	t.Parallel()
	source := SourceEvidence{
		Mode: SourceDevelopment, Identity: "unknown+dirty." + strings.Repeat("b", 12),
		TreeDigest: "sha256:" + strings.Repeat("b", 64), Dirty: true,
	}
	encoded, err := json.Marshal(map[string]any{
		"mode": source.Mode, "source_identity": source.Identity,
		"tree_digest": source.TreeDigest, "dirty": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRuntimeVersion(encoded)
	if err != nil || decoded != source {
		t.Fatalf("decoded/error = %#v/%v", decoded, err)
	}
}

func runtimeVersionJSON(t *testing.T, source SourceEvidence) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"build_timestamp": "2026-08-09T08:00:00Z",
		"mode":            source.Mode,
		"version":         "v1.2.3",
		"source_identity": source.Identity,
		"revision":        source.Revision,
		"tree_digest":     source.TreeDigest,
		"dirty":           source.Dirty,
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func runtimeSourceLabels(source SourceEvidence) map[string]string {
	return map[string]string{
		containerModeLabel:           string(source.Mode),
		containerSourceIdentityLabel: source.Identity,
		containerRevisionLabel:       source.Revision,
		containerTreeDigestLabel:     source.TreeDigest,
		containerDirtyLabel:          "false",
	}
}

func fakeContainerIdentityRunner(
	t *testing.T, source SourceEvidence, labels map[string]string,
) runtimeCommand {
	t.Helper()
	return func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		if name != "docker" {
			t.Fatalf("engine = %q, want docker", name)
		}
		switch {
		case len(arguments) == 3 && arguments[0] == "image" && arguments[1] == "inspect" && arguments[2] == "agent:candidate":
			encoded, err := json.Marshal([]map[string]any{{"Id": "sha256:image-id", "Config": map[string]any{"Labels": labels}}})
			if err != nil {
				t.Fatal(err)
			}
			return encoded, nil
		case len(arguments) == 7 && arguments[0] == "run" && arguments[1] == "--rm" &&
			arguments[2] == "--network" && arguments[3] == "none" && arguments[4] == "--read-only" &&
			arguments[5] == "sha256:image-id" && arguments[6] == "version":
			return runtimeVersionJSON(t, source), nil
		default:
			t.Fatalf("unexpected container identity command: %q", arguments)
			return nil, nil
		}
	}
}
