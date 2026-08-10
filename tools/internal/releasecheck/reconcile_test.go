package releasecheck

import (
	"strings"
	"testing"
)

const (
	testVersion      = "v0.5.0"
	testSHA          = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSourceDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testIndexDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func packageVersion(tags ...string) map[string]any {
	rawTags := make([]any, len(tags))
	for index, tag := range tags {
		rawTags[index] = tag
	}
	if len(rawTags) == 0 {
		rawTags = []any{testVersion}
	}
	return map[string]any{
		"id": float64(12345), "name": testIndexDigest,
		"metadata": map[string]any{"container": map[string]any{"tags": rawTags}},
	}
}

func releaseIndex(mediaType string) map[string]any {
	index := map[string]any{
		"mediaType": mediaType,
		"manifests": []any{map[string]any{"digest": testSourceDigest}},
	}
	if mediaType == OCIIndexMediaType {
		index["annotations"] = map[string]any{
			"org.opencontainers.image.revision": testSHA,
			"org.opencontainers.image.version":  testVersion,
		}
	}
	return index
}

func sourceImage() map[string]any {
	return map[string]any{"config": map[string]any{"Labels": map[string]any{
		"org.opencontainers.image.revision": testSHA,
		"org.opencontainers.image.version":  testVersion,
	}}}
}

func validReconcileInput() ReconcileInput {
	return ReconcileInput{
		PackageVersions: []map[string]any{packageVersion()},
		Version:         testVersion, SHA: testSHA, SourceDigest: testSourceDigest,
		Index: releaseIndex(OCIIndexMediaType), SourceImage: sourceImage(), ResolvedDigest: testIndexDigest,
	}
}

func TestValidateReconcileTargetReturnsOnlyOwnedIdentity(t *testing.T) {
	result, err := ValidateReconcileTarget(validReconcileInput())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "owned" || result["version_id"] != int64(12345) || result["digest"] != testIndexDigest {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateReconcileTargetAcceptsProvenAbsence(t *testing.T) {
	input := validReconcileInput()
	input.PackageVersions = nil
	input.Index, input.SourceImage, input.ResolvedDigest = nil, nil, ""
	input.RegistryAbsent = true
	result, err := ValidateReconcileTarget(input)
	if err != nil || result["state"] != "absent" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	input.RegistryAbsent = false
	if _, err := ValidateReconcileTarget(input); err == nil || !strings.Contains(err.Error(), "absence was not proven") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateReconcileTargetFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ReconcileInput)
		want string
	}{
		{"duplicate package", func(input *ReconcileInput) {
			input.PackageVersions = append(input.PackageVersions, packageVersion())
		}, "more than one"},
		{"shared tag", func(input *ReconcileInput) {
			input.PackageVersions = []map[string]any{packageVersion(testVersion, "latest")}
		}, "another tag"},
		{"digest disagreement", func(input *ReconcileInput) {
			input.ResolvedDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		}, "disagree"},
		{"wrong child", func(input *ReconcileInput) {
			input.Index["manifests"] = []any{map[string]any{"digest": testIndexDigest}}
		}, "qualified source digest"},
		{"wrong labels", func(input *ReconcileInput) {
			input.SourceImage = map[string]any{}
		}, "source image labels"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validReconcileInput()
			test.edit(&input)
			_, err := ValidateReconcileTarget(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateReleaseIndexHandlesOCIAndDockerRules(t *testing.T) {
	for _, mediaType := range []string{OCIIndexMediaType, DockerIndexMediaType} {
		if _, err := ValidateReleaseIndex(releaseIndex(mediaType), sourceImage(), testVersion, testSHA, testSourceDigest); err != nil {
			t.Errorf("%s: %v", mediaType, err)
		}
	}
	docker := releaseIndex(DockerIndexMediaType)
	docker["annotations"] = map[string]any{"org.opencontainers.image.revision": testSHA}
	if _, err := ValidateReleaseIndex(docker, sourceImage(), testVersion, testSHA, testSourceDigest); err == nil ||
		!strings.Contains(err.Error(), "unsupported annotations") {
		t.Fatalf("error = %v", err)
	}
}
