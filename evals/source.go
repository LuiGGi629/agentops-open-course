package evals

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type SourceMode string

const (
	SourceDevelopment SourceMode = "development"
	SourceRelease     SourceMode = "release"
)

var (
	sourceRevisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	dirtyIdentityPattern  = regexp.MustCompile(`^unknown\+dirty\.[0-9a-f]{12}$`)
	treeDigestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// SourceEvidence keeps the checkout revision and content digest separate. A
// dirty development tree has a deterministic identity but never claims HEAD.
type SourceEvidence struct {
	Mode       SourceMode `json:"mode"`
	Identity   string     `json:"identity"`
	Revision   string     `json:"revision,omitempty"`
	TreeDigest string     `json:"tree_digest"`
	Dirty      bool       `json:"dirty"`
	Shallow    bool       `json:"shallow"`
}

func (s SourceEvidence) Validate() error {
	var problems []error
	if s.Mode != SourceDevelopment && s.Mode != SourceRelease {
		problems = append(problems, fmt.Errorf("invalid source mode %q", s.Mode))
	}
	if !treeDigestPattern.MatchString(s.TreeDigest) {
		problems = append(problems, errors.New("source tree digest must be a lowercase sha256 digest"))
	}
	switch {
	case s.Mode == SourceRelease:
		if s.Dirty || !sourceRevisionPattern.MatchString(s.Revision) || s.Identity != s.Revision {
			problems = append(problems, errors.New("release source must be clean and identify one full revision"))
		}
	case s.Dirty:
		if s.Revision != "" || !dirtyIdentityPattern.MatchString(s.Identity) {
			problems = append(problems, errors.New("dirty development source must not claim a revision"))
		}
	default:
		if !sourceRevisionPattern.MatchString(s.Revision) || s.Identity != s.Revision {
			problems = append(problems, errors.New("clean development source must identify one full revision"))
		}
	}
	return errors.Join(problems...)
}

func validPlatformIdentity(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}
