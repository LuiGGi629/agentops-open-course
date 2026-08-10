package evals

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeContainerCommand struct {
	done   chan error
	killed bool
	mu     sync.Mutex
}

func (c *fakeContainerCommand) Wait() error { return <-c.done }

func (c *fakeContainerCommand) Kill() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killed = true
	return nil
}

func (c *fakeContainerCommand) wasKilled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killed
}

func TestAgentContainerOwnsDockerLifecycleAndRuntimeIdentity(t *testing.T) {
	t.Parallel()

	command := &fakeContainerCommand{done: make(chan error, 1)}
	var started commandSpec
	var stopped commandSpec
	var readyURL string
	var readyPath string
	dependencies := containerDependencies{
		start: func(spec commandSpec) (runningCommand, error) {
			started = spec
			return command, nil
		},
		run: func(_ context.Context, spec commandSpec) error {
			stopped = spec
			command.done <- nil
			return nil
		},
		waitReady: func(_ context.Context, _ <-chan error, baseURL, path string, _ time.Duration) error {
			readyURL = baseURL
			readyPath = path
			return nil
		},
	}

	container, err := startAgentContainer(context.Background(), AgentContainerConfig{
		Engine: "docker", Image: "agentops-agent:eval", SourceIdentity: "unknown+dirty.0123456789ab",
		Transport: "rest", Entrypoint: "workflow", Port: 43123, Output: io.Discard,
		Environment: map[string]string{
			"OPENAI_API_KEY":     "should-not-appear-in-arguments",
			"OPENAI_BASE_URL":    "http://127.0.0.1:11434/v1",
			"EVAL_PRIVATE_VALUE": "must-not-enter-the-agent",
		},
	}, dependencies)
	if err != nil {
		t.Fatal(err)
	}

	if started.name != "docker" {
		t.Fatalf("engine = %q, want docker", started.name)
	}
	assertArgumentsContainInOrder(t, started.arguments, []string{
		"run", "--rm", "--name", "agentops-eval-43123", "--network", "host",
	})
	for _, expected := range []string{
		"--tmpfs", "/app/state:rw,nosuid,nodev,noexec,uid=10001,gid=10001,mode=0700",
		"--label", "agentops.eval.source_identity=unknown+dirty.0123456789ab",
		"agentops-agent:eval", "web", "-port", "43123", "api",
	} {
		if !slices.Contains(started.arguments, expected) {
			t.Errorf("docker arguments do not contain %q: %q", expected, started.arguments)
		}
	}
	arguments := strings.Join(started.arguments, " ")
	if strings.Contains(arguments, "should-not-appear-in-arguments") || strings.Contains(arguments, "EVAL_PRIVATE_VALUE") {
		t.Fatalf("docker arguments expose or forward private evaluator data: %s", arguments)
	}
	for _, key := range []string{
		"AGENT_ENTRYPOINT", "AGENT_STATE_DIR", "AGENT_DATA_DIR", "AGENT_A2A_BIND_HOST",
		"AGENT_A2A_PORT", "OPENAI_API_KEY", "OPENAI_BASE_URL", "OTEL_SDK_DISABLED",
	} {
		assertDockerEnvironmentKey(t, started.arguments, key)
	}
	joinedEnvironment := strings.Join(started.environment, "\n")
	for _, expected := range []string{
		"AGENT_ENTRYPOINT=workflow", "AGENT_STATE_DIR=/app/state", "AGENT_DATA_DIR=/app/data",
		"AGENT_A2A_BIND_HOST=127.0.0.1", "AGENT_A2A_PORT=43123",
		"OTEL_SDK_DISABLED=true",
	} {
		if !strings.Contains(joinedEnvironment, expected) {
			t.Errorf("container command environment is missing %q", expected)
		}
	}
	if readyURL != "http://127.0.0.1:43123" || readyPath != "/list-apps" {
		t.Fatalf("readiness = %q %q, want REST contract", readyURL, readyPath)
	}

	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
	if stopped.name != "docker" {
		t.Fatalf("stop engine = %q, want docker", stopped.name)
	}
	if diff := strings.Join(stopped.arguments, " "); diff != "stop --timeout=30 agentops-eval-43123" {
		t.Fatalf("stop arguments = %q", diff)
	}
	if command.wasKilled() {
		t.Fatal("normal container shutdown killed the docker client")
	}
}

func TestAgentContainerA2AUsesLoopbackAndHealthReadiness(t *testing.T) {
	t.Parallel()

	command := &fakeContainerCommand{done: make(chan error, 1)}
	var started commandSpec
	var readyPath string
	dependencies := containerDependencies{
		start: func(spec commandSpec) (runningCommand, error) {
			started = spec
			return command, nil
		},
		run: func(_ context.Context, _ commandSpec) error {
			command.done <- nil
			return nil
		},
		waitReady: func(_ context.Context, _ <-chan error, _, path string, _ time.Duration) error {
			readyPath = path
			return nil
		},
	}
	container, err := startAgentContainer(context.Background(), AgentContainerConfig{
		Engine: "docker", Image: "agentops-agent:eval", SourceIdentity: "abcdef",
		Transport: "a2a", Entrypoint: "agent", Port: 43124,
	}, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if got := started.arguments[len(started.arguments)-2:]; !slices.Equal(got, []string{"agentops-agent:eval", "a2a"}) {
		t.Fatalf("container command tail = %q", got)
	}
	if readyPath != "/healthz" {
		t.Fatalf("readiness path = %q, want /healthz", readyPath)
	}
	if err := container.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentContainerStartFailureDoesNotLaunchAnythingElse(t *testing.T) {
	t.Parallel()

	want := errors.New("docker unavailable")
	startCalls := 0
	dependencies := containerDependencies{
		start: func(commandSpec) (runningCommand, error) {
			startCalls++
			return nil, want
		},
		run: func(context.Context, commandSpec) error {
			t.Fatal("cleanup must not run for a command that never started")
			return nil
		},
		waitReady: func(context.Context, <-chan error, string, string, time.Duration) error {
			t.Fatal("readiness must not run for a command that never started")
			return nil
		},
	}
	_, err := startAgentContainer(context.Background(), AgentContainerConfig{
		Engine: "docker", Image: "agentops-agent:eval", SourceIdentity: "abcdef",
		Transport: "rest", Entrypoint: "agent", Port: 43125,
	}, dependencies)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want docker start failure", err)
	}
	if startCalls != 1 {
		t.Fatalf("start calls = %d, want exactly one container attempt", startCalls)
	}
}

func TestAgentContainerReadinessFailureStopsOwnedContainer(t *testing.T) {
	t.Parallel()

	command := &fakeContainerCommand{done: make(chan error, 1)}
	want := errors.New("readiness failed")
	stopCalls := 0
	dependencies := containerDependencies{
		start: func(commandSpec) (runningCommand, error) { return command, nil },
		run: func(_ context.Context, spec commandSpec) error {
			stopCalls++
			if !slices.Equal(spec.arguments, []string{"stop", "--timeout=30", "agentops-eval-43126"}) {
				t.Fatalf("cleanup command = %q", spec.arguments)
			}
			command.done <- nil
			return nil
		},
		waitReady: func(context.Context, <-chan error, string, string, time.Duration) error { return want },
	}
	_, err := startAgentContainer(context.Background(), AgentContainerConfig{
		Image: "agentops-agent:eval", SourceIdentity: "abcdef",
		Transport: "rest", Entrypoint: "agent", Port: 43126,
	}, dependencies)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want readiness failure", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls = %d, want one", stopCalls)
	}
}

func TestAgentContainerForceRemoveUsesIndependentContextAfterStopFailure(t *testing.T) {
	t.Parallel()

	command := &fakeContainerCommand{done: make(chan error, 1)}
	want := errors.New("docker stop timed out")
	var stopContext context.Context
	removeCalls := 0
	reusedContext := false
	dependencies := containerDependencies{
		start: func(commandSpec) (runningCommand, error) { return command, nil },
		run: func(ctx context.Context, spec commandSpec) error {
			switch spec.arguments[0] {
			case "stop":
				stopContext = ctx
				return want
			case "rm":
				removeCalls++
				reusedContext = ctx == stopContext
				command.done <- nil
				return nil
			default:
				t.Fatalf("unexpected cleanup command %q", spec.arguments)
				return nil
			}
		},
		waitReady: func(context.Context, <-chan error, string, string, time.Duration) error { return nil },
	}
	container, err := startAgentContainer(context.Background(), AgentContainerConfig{
		Image: "agentops-agent:eval", SourceIdentity: "abcdef",
		Transport: "rest", Entrypoint: "agent", Port: 43127,
	}, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := container.Close(); !errors.Is(err, want) {
		t.Fatalf("Close() error = %v, want stop failure", err)
	}
	if removeCalls != 1 {
		t.Fatalf("force-remove calls = %d, want one", removeCalls)
	}
	if reusedContext {
		t.Fatal("force-remove reused the failed stop context")
	}
}

func TestAgentContainerRequiresImageAndSourceIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		config AgentContainerConfig
	}{
		{name: "image", config: AgentContainerConfig{SourceIdentity: "abc", Transport: "rest", Entrypoint: "agent", Port: 1}},
		{name: "source", config: AgentContainerConfig{Image: "agent:dev", Transport: "rest", Entrypoint: "agent", Port: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := startAgentContainer(context.Background(), test.config, containerDependencies{}); err == nil {
				t.Fatal("missing container identity was accepted")
			}
		})
	}
}

func assertArgumentsContainInOrder(t *testing.T, arguments, expected []string) {
	t.Helper()
	position := 0
	for _, argument := range arguments {
		if position < len(expected) && argument == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Fatalf("arguments %q do not contain ordered subset %q", arguments, expected)
	}
}

func assertDockerEnvironmentKey(t *testing.T, arguments []string, key string) {
	t.Helper()
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == "--env" && arguments[index+1] == key {
			return
		}
	}
	t.Errorf("docker arguments do not pass environment key %q: %q", key, arguments)
}
