package evals

import (
	"strings"
	"testing"
)

func TestSourceEvidenceSeparatesReleaseAndDirtyDevelopmentIdentity(t *testing.T) {
	t.Parallel()
	release := testSourceEvidence()
	if err := release.Validate(); err != nil {
		t.Fatal(err)
	}
	dirty := release
	dirty.Mode = SourceDevelopment
	dirty.Identity = "unknown+dirty." + strings.Repeat("b", 12)
	dirty.Revision = ""
	dirty.Dirty = true
	if err := dirty.Validate(); err != nil {
		t.Fatal(err)
	}
	dirty.Revision = release.Revision
	if err := dirty.Validate(); err == nil {
		t.Fatal("dirty development source impersonated HEAD")
	}
}

func testSourceEvidence() SourceEvidence {
	revision := strings.Repeat("a", 40)
	return SourceEvidence{
		Mode: SourceRelease, Identity: revision, Revision: revision,
		TreeDigest: "sha256:" + strings.Repeat("b", 64),
	}
}
