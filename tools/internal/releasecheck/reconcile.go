package releasecheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

const (
	DockerIndexMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"
	OCIIndexMediaType    = "application/vnd.oci.image.index.v1+json"
)

var (
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	shaPattern     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

type ReconcileInput struct {
	Index           map[string]any
	SourceImage     map[string]any
	Version         string
	SHA             string
	SourceDigest    string
	ResolvedDigest  string
	PackageVersions []map[string]any
	RegistryAbsent  bool
}

func ValidateReleaseIndex(index, sourceImage map[string]any, version, sha, sourceDigest string) (map[string]any, error) {
	if err := validateAuthority(version, sha, sourceDigest); err != nil {
		return nil, err
	}
	mediaType, _ := index["mediaType"].(string)
	if mediaType != DockerIndexMediaType && mediaType != OCIIndexMediaType {
		return nil, errors.New("release tag does not resolve to an OCI or Docker index")
	}
	manifests, ok := index["manifests"].([]any)
	if !ok || len(manifests) != 1 {
		return nil, errors.New("release index must contain exactly one source manifest")
	}
	manifest, ok := manifests[0].(map[string]any)
	if !ok {
		return nil, errors.New("release index must contain exactly one source manifest")
	}
	if manifest["digest"] != sourceDigest {
		return nil, errors.New("release index does not contain the qualified source digest")
	}
	config, _ := sourceImage["config"].(map[string]any)
	labels, _ := config["Labels"].(map[string]any)
	if labels["org.opencontainers.image.revision"] != sha || labels["org.opencontainers.image.version"] != version {
		return nil, errors.New("source image labels do not identify the qualified release")
	}
	annotations, annotationsExist := index["annotations"].(map[string]any)
	if mediaType == OCIIndexMediaType {
		if !annotationsExist || annotations["org.opencontainers.image.revision"] != sha ||
			annotations["org.opencontainers.image.version"] != version {
			return nil, errors.New("OCI release index annotations do not identify the qualified source")
		}
	} else if raw, exists := index["annotations"]; exists && raw != nil {
		if annotations == nil || len(annotations) != 0 {
			return nil, errors.New("docker release index contains unsupported annotations")
		}
	}
	return map[string]any{"state": "valid", "media_type": mediaType}, nil
}

func ValidateReconcileTarget(input ReconcileInput) (map[string]any, error) {
	if err := validateAuthority(input.Version, input.SHA, input.SourceDigest); err != nil {
		return nil, err
	}
	indexPresent := input.Index != nil
	digestPresent := input.ResolvedDigest != ""
	imagePresent := input.SourceImage != nil
	if indexPresent != digestPresent {
		return nil, errors.New("registry index and resolved digest must be supplied together")
	}
	if indexPresent != imagePresent {
		return nil, errors.New("registry index and source image must be supplied together")
	}
	if indexPresent && input.RegistryAbsent {
		return nil, errors.New("registry tag cannot be both present and absent")
	}

	matching := make([]map[string]any, 0, 1)
	for _, record := range input.PackageVersions {
		tags, err := packageTags(record)
		if err != nil {
			return nil, err
		}
		for _, tag := range tags {
			if tag == input.Version {
				matching = append(matching, record)
				break
			}
		}
	}
	if len(matching) == 0 {
		if indexPresent {
			return nil, errors.New("registry tag has no uniquely owned package version")
		}
		if !input.RegistryAbsent {
			return nil, errors.New("registry tag absence was not proven")
		}
		return map[string]any{"state": "absent"}, nil
	}
	if len(matching) != 1 {
		return nil, errors.New("more than one package version carries the release tag")
	}
	record := matching[0]
	tags, err := packageTags(record)
	if err != nil {
		return nil, err
	}
	if len(tags) != 1 || tags[0] != input.Version {
		return nil, errors.New("release package version carries another tag")
	}
	versionID, ok := record["id"].(float64)
	if !ok || versionID < 1 || versionID != float64(int64(versionID)) {
		return nil, errors.New("release package version has no positive numeric id")
	}
	packageDigest, ok := record["name"].(string)
	if !ok || !digestPattern.MatchString(packageDigest) {
		return nil, errors.New("release package version has no immutable index digest")
	}
	if !indexPresent {
		return nil, errors.New("owned package version does not resolve to complete registry evidence")
	}
	if !digestPattern.MatchString(input.ResolvedDigest) || input.ResolvedDigest != packageDigest {
		return nil, errors.New("registry and package API disagree on the release index digest")
	}
	if _, err := ValidateReleaseIndex(input.Index, input.SourceImage,
		input.Version, input.SHA, input.SourceDigest); err != nil {
		return nil, err
	}
	return map[string]any{"state": "owned", "version_id": int64(versionID), "digest": packageDigest}, nil
}

func validateAuthority(version, sha, sourceDigest string) error {
	if !versionPattern.MatchString(version) {
		return errors.New("release version must be a v-prefixed three-part version")
	}
	if !shaPattern.MatchString(sha) {
		return errors.New("release source must be a full lowercase commit SHA")
	}
	if !digestPattern.MatchString(sourceDigest) {
		return errors.New("release source digest must be an immutable SHA-256")
	}
	return nil
}

func packageTags(record map[string]any) ([]string, error) {
	metadata, _ := record["metadata"].(map[string]any)
	container, _ := metadata["container"].(map[string]any)
	rawTags, ok := container["tags"].([]any)
	if !ok {
		return nil, errors.New("package version has an invalid container tag inventory")
	}
	tags := make([]string, len(rawTags))
	for index, raw := range rawTags {
		tag, ok := raw.(string)
		if !ok {
			return nil, errors.New("package version has an invalid container tag inventory")
		}
		tags[index] = tag
	}
	return tags, nil
}

func FlattenPackageVersions(document any) ([]map[string]any, error) {
	pages, ok := document.([]any)
	if !ok {
		return nil, errors.New("package versions must contain a JSON array of package pages")
	}
	records := make([]map[string]any, 0)
	for _, rawPage := range pages {
		page, ok := rawPage.([]any)
		if !ok {
			return nil, errors.New("package versions contain a non-array package page")
		}
		for _, rawRecord := range page {
			record, ok := rawRecord.(map[string]any)
			if !ok {
				return nil, errors.New("package versions contain a non-object package version")
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func DecodeObject(content []byte, label string) (map[string]any, error) {
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", label, err)
	}
	object, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must contain one JSON object", label)
	}
	return object, nil
}
