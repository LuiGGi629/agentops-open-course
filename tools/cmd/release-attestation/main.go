package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/releasecheck"
)

func main() {
	attestationsPath := flag.String("attestations", "", "verified cosign attestation JSON")
	sbomPath := flag.String("sbom", "", "release SPDX SBOM JSON")
	flag.Parse()
	if *attestationsPath == "" || *sbomPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	attestations, err := readJSON(*attestationsPath)
	if err != nil {
		fail(err)
	}
	sbom, err := readJSON(*sbomPath)
	if err != nil {
		fail(err)
	}
	count, err := releasecheck.VerifyAttestations(attestations, sbom)
	if err != nil {
		fail(err)
	}
	fmt.Printf("verified %d policy-valid SPDX attestation envelope(s)\n", count)
}

func readJSON(path string) (any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var document any
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return document, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
