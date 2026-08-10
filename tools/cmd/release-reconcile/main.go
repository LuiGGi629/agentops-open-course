package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/releasecheck"
)

func main() {
	versionsPath := flag.String("versions", "", "slurped GitHub package-version pages")
	version := flag.String("version", "", "v-prefixed release version")
	sha := flag.String("sha", "", "full source commit")
	sourceDigest := flag.String("source-digest", "", "qualified child manifest digest")
	indexPath := flag.String("index", "", "resolved release index JSON")
	sourceImagePath := flag.String("source-image", "", "qualified source image inspect JSON")
	resolvedDigest := flag.String("resolved-digest", "", "resolved release index digest")
	registryAbsent := flag.Bool("registry-absent", false, "assert that registry absence was separately proven")
	validateOnly := flag.Bool("validate-index-only", false, "validate the release index without package reconciliation")
	flag.Parse()

	var result map[string]any
	var err error
	if *validateOnly {
		if *indexPath == "" || *sourceImagePath == "" || *versionsPath != "" || *resolvedDigest != "" || *registryAbsent {
			fail(errors.New("--validate-index-only requires only --index and --source-image evidence"))
		}
		index := mustObject(*indexPath)
		image := mustObject(*sourceImagePath)
		result, err = releasecheck.ValidateReleaseIndex(index, image, *version, *sha, *sourceDigest)
	} else {
		if *versionsPath == "" {
			fail(errors.New("reconciliation requires --versions"))
		}
		versionsDocument := mustJSON(*versionsPath)
		versions, flattenErr := releasecheck.FlattenPackageVersions(versionsDocument)
		if flattenErr != nil {
			fail(flattenErr)
		}
		var index, image map[string]any
		if *indexPath != "" {
			index = mustObject(*indexPath)
		}
		if *sourceImagePath != "" {
			image = mustObject(*sourceImagePath)
		}
		result, err = releasecheck.ValidateReconcileTarget(releasecheck.ReconcileInput{
			PackageVersions: versions, Version: *version, SHA: *sha, SourceDigest: *sourceDigest,
			Index: index, SourceImage: image, ResolvedDigest: *resolvedDigest, RegistryAbsent: *registryAbsent,
		})
	}
	if err != nil {
		fail(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		fail(err)
	}
	fmt.Println(string(encoded))
}

func mustJSON(path string) any {
	content, err := os.ReadFile(path)
	if err != nil {
		fail(fmt.Errorf("reading %s: %w", path, err))
	}
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		fail(fmt.Errorf("decoding %s: %w", path, err))
	}
	return document
}

func mustObject(path string) map[string]any {
	document := mustJSON(path)
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
