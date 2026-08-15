package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MLOps-Courses/agentops-open-course/evals"
)

func TestCompareCommandRequiresExplicitArtifacts(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := compareCommand(nil, &output)
	if err == nil || !strings.Contains(err.Error(), "explicit --baseline and --candidate") {
		t.Fatalf("error = %v", err)
	}
}

func TestRetrievalCommandRejectsInvalidTimeoutBeforeStartingAgent(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := execute(t.Context(), []string{
		"retrieval", "--agent-binary", "unused", "--request-timeout", "-1s",
	}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "timeouts must not be negative") {
		t.Fatalf("error = %v", err)
	}
}

func TestContainerRuntimeFailureNeverFallsBackToHostProcess(t *testing.T) {
	t.Parallel()

	want := errors.New("container configuration rejected")
	processCalls := 0
	containerCalls := 0
	_, err := selectClientFactory(clientFactoryConfig{
		Runtime: "container",
		Container: evals.ContainerClientFactoryConfig{
			Image:     "agentops-agent:eval",
			Transport: "rest", Entrypoint: "agent",
		},
	}, clientFactoryConstructors{
		process: func(evals.ProcessClientFactoryConfig) (evals.ClientFactory, error) {
			processCalls++
			return nil, nil
		},
		container: func(evals.ContainerClientFactoryConfig) (evals.ClientFactory, error) {
			containerCalls++
			return nil, want
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want container failure", err)
	}
	if processCalls != 0 || containerCalls != 1 {
		t.Fatalf("constructor calls = process %d, container %d", processCalls, containerCalls)
	}
}

func TestClientFactoryRejectsUnknownRuntime(t *testing.T) {
	t.Parallel()

	_, err := selectClientFactory(clientFactoryConfig{Runtime: "automatic"}, clientFactoryConstructors{})
	if err == nil {
		t.Fatal("unknown runtime was accepted")
	}
}

func TestResolveCheckoutAlwaysExecutesRepositorySource(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Join(string(filepath.Separator), "checkout")
	var calls [][]string
	runner := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, arguments...))
		switch name {
		case "git":
			return []byte(repositoryRoot + "\n"), nil
		case "go":
			encoded, err := json.Marshal(checkoutIdentity{
				Revision:   strings.Repeat("a", 40),
				TreeDigest: "sha256:" + strings.Repeat("b", 64),
			})
			if err != nil {
				t.Fatal(err)
			}
			return encoded, nil
		default:
			t.Fatalf("unexpected command %q", name)
			return nil, nil
		}
	}

	source, err := resolveCheckoutWithCommand(t.Context(), runner)
	if err != nil {
		t.Fatal(err)
	}
	if source.Revision != strings.Repeat("a", 40) || source.Dirty {
		t.Fatalf("resolved checkout = %+v", source)
	}
	want := []string{
		"go", "-C", filepath.Join(repositoryRoot, "tools"), "run", "./cmd/source-identity",
		"--root", repositoryRoot,
	}
	if len(calls) != 2 || strings.Join(calls[1], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("source command calls = %q, want second call %q", calls, want)
	}
}

func TestResolveCheckoutRejectsAnInconsistentIdentity(t *testing.T) {
	t.Parallel()

	runner := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "git" {
			return []byte("/checkout\n"), nil
		}
		// A dirty tree that still claims HEAD would let a stale binary match.
		encoded, err := json.Marshal(checkoutIdentity{
			Revision: strings.Repeat("a", 40), TreeDigest: "sha256:" + strings.Repeat("b", 64), Dirty: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return encoded, nil
	}
	if _, err := resolveCheckoutWithCommand(t.Context(), runner); err == nil ||
		!strings.Contains(err.Error(), "validate source identity") {
		t.Fatalf("error = %v, want inconsistent identity rejection", err)
	}
}

func TestPreviousRunArtifactTreatsAMissingFileAsNoBaseline(t *testing.T) {
	t.Parallel()

	if _, found := previousRunArtifact(filepath.Join(t.TempDir(), "results.json")); found {
		t.Fatal("a missing results file was reported as a previous run")
	}
}

// The taught command carries four required cases, and `mise run eval -- ...` appends to
// it. A repeatable flag could therefore only grow, which is what made the README's
// other-evalset recipes impossible; a replaceable one lets the last value win.
func TestSplitCaseListLetsTheLastValueWin(t *testing.T) {
	for name, test := range map[string]struct {
		value string
		want  []string
	}{
		"the taught four": {"a,b,c,d", []string{"a", "b", "c", "d"}},
		"cleared":         {"", nil},
		"only separators": {" , ,", nil},
		"trimmed":         {" a , b ", []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := splitCaseList(test.value)
			if len(got) != len(test.want) {
				t.Fatalf("splitCaseList(%q) = %#v, want %#v", test.value, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("splitCaseList(%q) = %#v, want %#v", test.value, got, test.want)
				}
			}
		})
	}
}

// TestJudgeTimeoutFromEnvironment pins the deadline a CPU-only host has to be
// able to raise, and the two values it must refuse.
func TestJudgeTimeoutFromEnvironment(t *testing.T) {
	for name, test := range map[string]struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		"unset keeps the default": {value: "", want: 0},
		"raised for a slow host":  {value: "1200", want: 20 * time.Minute},
		"zero is not no deadline": {value: "0", wantErr: true},
		"negative is refused":     {value: "-5", wantErr: true},
		"non-numeric is refused":  {value: "20m", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("EVAL_JUDGE_TIMEOUT_S", test.value)
			got, err := judgeTimeoutFromEnvironment()
			if test.wantErr {
				if err == nil {
					t.Fatalf("judgeTimeoutFromEnvironment(%q) = %v, want an error", test.value, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("judgeTimeoutFromEnvironment(%q) = %v, %v; want %v", test.value, got, err, test.want)
			}
		})
	}
}
