package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MLOps-Courses/agentops-open-course-go/evals"
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
		"retrieval", "--source-commit", "commit", "--agent-binary", "unused", "--request-timeout", "-1s",
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

func TestPolicyCalibrationCollectsFailedThresholdEvidence(t *testing.T) {
	t.Parallel()
	failed := evals.RunArtifact{Summary: evals.RunSummary{
		PassRate: 0.25, MinimumPassRate: 0.33, RequiredCasesPassed: false,
	}}
	if err := runThresholdError(failed, true); err != nil {
		t.Fatalf("calibration trial error = %v", err)
	}
	if err := runThresholdError(failed, false); err == nil {
		t.Fatal("ordinary run accepted failed thresholds")
	}
}

func TestCalibrateCommandRejectsCallerFloorWithReleasePolicy(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	err := calibrateCommand(t.Context(), []string{
		"--release-policy", "release-policy.json", "--source-mode", "release", "--floor", "0.5",
	}, &output)
	if err == nil || !strings.Contains(err.Error(), "policy owns") {
		t.Fatalf("caller-selected release floor error = %v", err)
	}
}

func TestCostIdentityRejectsNonFiniteTemperature(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AGENT_MODEL_TEMPERATURE", value)
			_, err := currentCostIdentity("provider", "model", "", evals.SourceEvidence{
				Mode:       evals.SourceRelease,
				Identity:   strings.Repeat("a", 40),
				Revision:   strings.Repeat("a", 40),
				TreeDigest: "sha256:" + strings.Repeat("b", 64),
			}, strings.Repeat("c", 64))
			if err == nil || !strings.Contains(err.Error(), "finite") {
				t.Fatalf("temperature %s error = %v", value, err)
			}
		})
	}
}

func TestReadSourceIdentityAlwaysExecutesRepositorySource(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Join(string(filepath.Separator), "checkout")
	var calls [][]string
	runner := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, arguments...))
		switch name {
		case "git":
			return []byte(repositoryRoot + "\n"), nil
		case "go":
			encoded, err := json.Marshal(sourceIdentity{
				Mode: "development", Display: strings.Repeat("a", 40), Revision: strings.Repeat("a", 40),
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

	if _, err := readSourceIdentityWithCommand(t.Context(), "development", runner); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"go", "-C", filepath.Join(repositoryRoot, "tools"), "run", "./cmd/source-identity",
		"--root", repositoryRoot, "--mode", "development",
	}
	if len(calls) != 2 || strings.Join(calls[1], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("source command calls = %q, want second call %q", calls, want)
	}
}
