package a2aserver_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/a2aserver"
)

// The trusted identity contract. Two things depend on the value reaching the
// run: the audit row's approved_by (through the run's user id) and the
// per-caller task ownership the listing path enforces. Both are asserted here
// end to end, because the Go seam is two pieces — an HTTP middleware and a call
// interceptor — and a test on either half alone would pass while the value
// never crossed.

const identityHeader = "X-Verified-Subject"

// identityRecorder captures the identity each invocation ran under.
type identityRecorder struct {
	users    []string
	sessions []string
	guard    sync.Mutex
}

// plugin builds the ADK plugin that does the recording.
func (r *identityRecorder) plugin(t *testing.T) *plugin.Plugin {
	t.Helper()

	built, err := plugin.New(plugin.Config{
		Name: "identity_recorder",
		BeforeModelCallback: func(ctx agent.Context, _ *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
			r.guard.Lock()
			defer r.guard.Unlock()
			r.users = append(r.users, ctx.UserID())
			r.sessions = append(r.sessions, ctx.SessionID())
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("plugin.New() error = %v, want nil", err)
	}
	return built
}

func (r *identityRecorder) observed() ([]string, []string) {
	r.guard.Lock()
	defer r.guard.Unlock()
	return append([]string(nil), r.users...), append([]string(nil), r.sessions...)
}

// newIdentityFixture serves an agent that records the identity of each run.
func newIdentityFixture(t *testing.T, header string) (*fixture, *identityRecorder) {
	t.Helper()

	recorder := &identityRecorder{}
	fixture := newFixture(t, func(opts *fixtureOptions) {
		opts.plugins = []*plugin.Plugin{recorder.plugin(t)}
		opts.options = func(options *a2aserver.Options) { options.TrustedIdentityHeader = header }
	})
	return fixture, recorder
}

func TestVerifiedIdentityBecomesTheRunUser(t *testing.T) {
	t.Parallel()

	fixture, recorder := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)

	envelope := rpc(t, server, a2aserver.RootPath, "message/send",
		textMessage("m1", "Hello."), [2]string{identityHeader, "  alice@example.test  "})
	if envelope["error"] != nil {
		t.Fatalf("message/send returned %v, want a result", envelope["error"])
	}

	users, sessions := recorder.observed()
	if len(users) != 1 {
		t.Fatalf("the model ran %d times, want 1", len(users))
	}
	// Trimmed, and it replaces the synthetic "A2A_USER_<contextId>" identity
	// that an unauthenticated caller would have run under.
	if users[0] != "alice@example.test" {
		t.Errorf("user id = %q, want the verified subject", users[0])
	}
	if strings.HasPrefix(users[0], "A2A_USER_") {
		t.Error("the synthetic identity survived a verified request")
	}
	// The session is still keyed on the A2A context id, which is what keeps one
	// conversation together across turns.
	if sessions[0] == "" || sessions[0] == users[0] {
		t.Errorf("session id = %q, want the A2A context id", sessions[0])
	}
}

func TestMissingVerifiedIdentityKeepsTheSyntheticUser(t *testing.T) {
	t.Parallel()

	fixture, recorder := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)

	if envelope := rpc(t, server, a2aserver.RootPath, "message/send",
		textMessage("m1", "Hello.")); envelope["error"] != nil {
		t.Fatalf("message/send returned %v, want a result", envelope["error"])
	}

	users, _ := recorder.observed()
	if len(users) != 1 {
		t.Fatalf("the model ran %d times, want 1", len(users))
	}
	if !strings.HasPrefix(users[0], "A2A_USER_") {
		t.Errorf("user id = %q, want the unauthenticated synthetic identity", users[0])
	}
}

func TestAnUnconfiguredHeaderIsNeverTrusted(t *testing.T) {
	t.Parallel()

	// No AGENT_TRUSTED_IDENTITY_HEADER means the middleware is not installed at
	// all, so there is no code path in which a client-supplied header is read.
	fixture, recorder := newIdentityFixture(t, "")
	server := fixture.serve(t)

	if envelope := rpc(t, server, a2aserver.RootPath, "message/send", textMessage("m1", "Hello."),
		[2]string{identityHeader, "mallory@evil.example"}); envelope["error"] != nil {
		t.Fatalf("message/send returned %v, want a result", envelope["error"])
	}

	users, _ := recorder.observed()
	if len(users) != 1 {
		t.Fatalf("the model ran %d times, want 1", len(users))
	}
	if users[0] == "mallory@evil.example" {
		t.Error("an unconfigured header was trusted")
	}
	if !strings.HasPrefix(users[0], "A2A_USER_") {
		t.Errorf("user id = %q, want the unauthenticated synthetic identity", users[0])
	}
}

func TestDuplicateIdentityHeadersAreRefusedBeforeTheAgentRuns(t *testing.T) {
	t.Parallel()

	fixture, recorder := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)

	response := post(t, server, a2aserver.RootPath, "message/send", textMessage("m1", "Hello."),
		[2]string{identityHeader, "alice@example.test"},
		[2]string{strings.ToLower(identityHeader), "mallory@evil.example"})
	defer func() { _ = response.Body.Close() }()

	if got := response.StatusCode; got != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", got, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the refusal: %v", err)
	}
	if body["error"] != "duplicate trusted identity header" {
		t.Errorf("body = %v, want the duplicate-header refusal", body)
	}
	// Two values mean at least one was not set by the gateway, and there is no
	// safe way to choose. Nothing downstream may run.
	if users, _ := recorder.observed(); len(users) != 0 {
		t.Errorf("the agent ran %d times behind a refused request, want 0", len(users))
	}
	if fixture.model.callCount() != 0 {
		t.Errorf("the model was called %d times behind a refused request, want 0", fixture.model.callCount())
	}
}

func TestAnEmptyIdentityHeaderIsNotAnIdentity(t *testing.T) {
	t.Parallel()

	fixture, recorder := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)

	if envelope := rpc(t, server, a2aserver.RootPath, "message/send", textMessage("m1", "Hello."),
		[2]string{identityHeader, "   "}); envelope["error"] != nil {
		t.Fatalf("message/send returned %v, want a result", envelope["error"])
	}

	users, _ := recorder.observed()
	if len(users) != 1 {
		t.Fatalf("the model ran %d times, want 1", len(users))
	}
	if !strings.HasPrefix(users[0], "A2A_USER_") {
		t.Errorf("user id = %q, want the unauthenticated synthetic identity", users[0])
	}
}

func TestIdentityDoesNotLeakBetweenRequests(t *testing.T) {
	t.Parallel()

	fixture, recorder := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)
	fixture.model.turns = append(fixture.model.turns, textTurn("Second."))

	if envelope := rpc(t, server, a2aserver.RootPath, "message/send", textMessage("m1", "First."),
		[2]string{identityHeader, "alice@example.test"}); envelope["error"] != nil {
		t.Fatalf("the first message/send returned %v, want a result", envelope["error"])
	}
	if envelope := rpc(t, server, a2aserver.RootPath, "message/send",
		textMessage("m2", "Second.")); envelope["error"] != nil {
		t.Fatalf("the second message/send returned %v, want a result", envelope["error"])
	}

	users, _ := recorder.observed()
	if len(users) != 2 {
		t.Fatalf("the model ran %d times, want 2", len(users))
	}
	if users[0] != "alice@example.test" {
		t.Errorf("first user id = %q, want the verified subject", users[0])
	}
	if !strings.HasPrefix(users[1], "A2A_USER_") {
		t.Errorf("second user id = %q, want the synthetic identity", users[1])
	}
}

// TestVerifiedIdentityScopesTaskOwnership is the second half of the identity
// contract: the same subject is what the task store authenticates on, so
// listing tasks is scoped to the caller who created them. Without the call
// interceptor the store would see an empty user and refuse every listing.
func TestVerifiedIdentityScopesTaskOwnership(t *testing.T) {
	t.Parallel()

	fixture, _ := newIdentityFixture(t, identityHeader)
	server := fixture.serve(t)
	fixture.model.turns = append(fixture.model.turns, textTurn("Second."))

	alice := [2]string{identityHeader, "alice@example.test"}
	bob := [2]string{identityHeader, "bob@example.test"}
	if envelope := rpc(t, server, a2aserver.RootPath, "message/send",
		textMessage("m1", "Hello."), alice); envelope["error"] != nil {
		t.Fatalf("message/send returned %v, want a result", envelope["error"])
	}

	// ListTasks exists only on the protocol 1.0 binding.
	listed := rpc(t, server, a2aserver.InvokePath, "ListTasks", map[string]any{}, alice)
	result, ok := listed["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListTasks returned %v, want a result", listed)
	}
	tasks, _ := result["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("alice listed %d tasks, want 1", len(tasks))
	}

	other := rpc(t, server, a2aserver.InvokePath, "ListTasks", map[string]any{}, bob)
	otherResult, ok := other["result"].(map[string]any)
	if !ok {
		t.Fatalf("ListTasks returned %v, want a result", other)
	}
	otherTasks, _ := otherResult["tasks"].([]any)
	if len(otherTasks) != 0 {
		t.Errorf("bob listed %d of alice's tasks, want 0", len(otherTasks))
	}
}

// TestUnauthenticatedListingIsRefused pins the reference behavior: with no
// identity in front of the server, task listing is not available at all. It is
// the same answer the in-memory store gives, and it is why the deployment
// documents a gateway.
func TestUnauthenticatedListingIsRefused(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t)
	server := fixture.serve(t)

	envelope := rpc(t, server, a2aserver.InvokePath, "ListTasks", map[string]any{})
	if envelope["error"] == nil {
		t.Errorf("ListTasks without an identity returned %v, want an error", envelope)
	}
}
