package policy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// This file holds the layer-2 tests: the named-entity cases test_pii.py covered
// with Presidio, plus the fail-closed rule that makes an optional analyzer safe
// to depend on.
//
// The cases carried over from Python are the two <PERSON> parametrizations and
// the "Paris" assertion inside
// test_redacts_personal_data_without_corrupting_domain_identifiers. They are
// not deleted and not weakened: they run here against a stub analyzer, and they
// state exactly which layer earns them.

// analyzerAnswer builds a Presidio-shaped response marking every occurrence of
// needle in text, in Python's units — code points, not bytes. Getting those
// units wrong is the failure mode the stub exists to catch.
func analyzerAnswer(text, needle string, class entity) string {
	var results []string
	for offset := 0; ; {
		index := strings.Index(text[offset:], needle)
		if index < 0 {
			break
		}
		start := utf8.RuneCountInString(text[:offset+index])
		end := start + utf8.RuneCountInString(needle)
		results = append(results, `{"entity_type":"`+string(class)+
			`","start":`+strconv.Itoa(start)+`,"end":`+strconv.Itoa(end)+`,"score":0.85}`)
		offset += index + len(needle)
	}
	return "[" + strings.Join(results, ",") + "]"
}

// withAnalyzer builds a policy whose layer 2 is the supplied stub.
func withAnalyzer(t *testing.T, stub *analyzerStub) *Policy {
	t.Helper()
	return newPolicy(t, Config{
		SanitizeToolOutput: true,
		Analyzer:           stub.start(t, AnalyzerConfig{}),
	})
}

// TestLayerTwoRedactsPersonNextToProtectedIncidentIdentifier is the Go home of
// test_redacts_person_next_to_protected_incident_identifier.
//
// The stub also asserts what the analyzer was given: the incident identifier
// must already be masked out of the text, because that protection is what stops
// an NER model from swallowing the identifier into the name span beside it.
func TestLayerTwoRedactsPersonNextToProtectedIncidentIdentifier(t *testing.T) {
	t.Parallel()

	for _, layout := range []struct {
		name string
		text string
		want string
	}{
		{"name first", "John " + authIncident, entityPerson.mask() + " " + authIncident},
		{"name last", authIncident + " John", authIncident + " " + entityPerson.mask()},
	} {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()

			stub := &analyzerStub{handler: func(t *testing.T, request analyzeRequest) (int, string) {
				if strings.Contains(request.Text, authIncident) {
					t.Errorf("the analyzer saw %q, want the incident identifier protected first", request.Text)
				}
				return http.StatusOK, analyzerAnswer(request.Text, "John", entityPerson)
			}}

			redacted := withAnalyzer(t, stub).RedactBoundaryText(t.Context(), layout.text)
			if redacted != layout.want {
				t.Errorf("RedactBoundaryText(%q) = %q, want %q", layout.text, redacted, layout.want)
			}
		})
	}
}

// TestLayerTwoRedactsLocations is the "Paris" half of
// test_redacts_personal_data_without_corrupting_domain_identifiers.
func TestLayerTwoRedactsLocations(t *testing.T) {
	t.Parallel()

	stub := &analyzerStub{handler: func(_ *testing.T, request analyzeRequest) (int, string) {
		return http.StatusOK, analyzerAnswer(request.Text, "Paris", entityLocation)
	}}

	redacted := withAnalyzer(t, stub).RedactBoundaryText(t.Context(),
		"Email "+operatorEmail+" from Paris about "+authIncident+".")

	for _, gone := range []string{operatorEmail, "Paris"} {
		if strings.Contains(redacted, gone) {
			t.Errorf("RedactBoundaryText() = %q, still contains %q", redacted, gone)
		}
	}
	if !strings.Contains(redacted, authIncident) {
		t.Errorf("RedactBoundaryText() = %q, want the incident identifier preserved", redacted)
	}
	for _, want := range []string{entityEmail.mask(), entityLocation.mask()} {
		if !strings.Contains(redacted, want) {
			t.Errorf("RedactBoundaryText() = %q, want it to contain %s", redacted, want)
		}
	}
}

// TestAnalyzerIsAskedOnlyForNamedEntities pins the division of labor: layer 1
// already owns every deterministic class, so sending them over the wire would
// pay a network round trip for an answer the process already has.
func TestAnalyzerIsAskedOnlyForNamedEntities(t *testing.T) {
	t.Parallel()

	requested := make(chan []string, 4)
	stub := &analyzerStub{handler: func(_ *testing.T, request analyzeRequest) (int, string) {
		requested <- request.Entities
		return http.StatusOK, "[]"
	}}
	policy := withAnalyzer(t, stub)

	for _, call := range []struct {
		name string
		run  func()
		want []entity
	}{
		{
			name: "boundary",
			run:  func() { policy.RedactBoundaryText(t.Context(), "some text") },
			want: []entity{entityLocation, entityPerson, entityNRP},
		},
		{
			name: "persisted",
			run:  func() { policy.RedactPersistedText(t.Context(), "some text") },
			want: []entity{entityLocation, entityPerson},
		},
	} {
		t.Run(call.name, func(t *testing.T) {
			call.run()
			got := <-requested
			if len(got) != len(call.want) {
				t.Fatalf("requested entities = %v, want %d of them", got, len(call.want))
			}
			for _, name := range call.want {
				if !strings.Contains(strings.Join(got, ","), string(name)) {
					t.Errorf("requested entities = %v, want %s among them", got, name)
				}
			}
		})
	}
}

// TestBlankTextNeverReachesTheAnalyzer keeps the optional service off the path
// where it has nothing to decide.
func TestBlankTextNeverReachesTheAnalyzer(t *testing.T) {
	t.Parallel()

	stub := &analyzerStub{handler: func(_ *testing.T, _ analyzeRequest) (int, string) {
		return http.StatusOK, "[]"
	}}
	policy := withAnalyzer(t, stub)

	if redacted := policy.RedactPersistedText(t.Context(), "   "); redacted != "   " {
		t.Errorf("RedactPersistedText() = %q, want it unchanged", redacted)
	}
	if calls := stub.calls.Load(); calls != 0 {
		t.Errorf("the analyzer was called %d times for blank text, want 0", calls)
	}
}

// TestAnalyzerFailuresRedactConservatively is the fail-closed rule.
//
// A configured analyzer that does not answer means the process cannot tell
// whether the text holds a name it promised to remove, so the text is withheld
// rather than passed through. Every transport-level failure resolves the same
// way, which is why they share one sentinel.
func TestAnalyzerFailuresRedactConservatively(t *testing.T) {
	t.Parallel()

	for _, failure := range []struct {
		handler func(t *testing.T, request analyzeRequest) (int, string)
		name    string
	}{
		{
			name: "server error",
			handler: func(_ *testing.T, _ analyzeRequest) (int, string) {
				return http.StatusInternalServerError, `{"error":"model not loaded"}`
			},
		},
		{
			name: "unparseable body",
			handler: func(_ *testing.T, _ analyzeRequest) (int, string) {
				return http.StatusOK, `not json at all`
			},
		},
		{
			name: "span outside the analyzed text",
			handler: func(_ *testing.T, request analyzeRequest) (int, string) {
				beyond := strconv.Itoa(utf8.RuneCountInString(request.Text) + 5)
				return http.StatusOK, `[{"entity_type":"PERSON","start":0,"end":` + beyond + `,"score":0.9}]`
			},
		},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()

			stub := &analyzerStub{handler: failure.handler}
			policy := withAnalyzer(t, stub)

			const text = "Ping the on-call about the deploy."
			if redacted := policy.RedactBoundaryText(t.Context(), text); redacted != AnalyzerUnavailableMask {
				t.Errorf("RedactBoundaryText() = %q, want %q", redacted, AnalyzerUnavailableMask)
			}
			// Structured values fail closed one string at a time, so the
			// surrounding structure survives and the failure is legible.
			redacted := renderValue(policy.RedactBoundaryValue(t.Context(),
				map[string]any{"summary": text, "count": 7}))
			if !strings.Contains(redacted, AnalyzerUnavailableMask) {
				t.Errorf("RedactBoundaryValue() = %s, want it to contain %s", redacted, AnalyzerUnavailableMask)
			}
			if strings.Contains(redacted, text) {
				t.Errorf("RedactBoundaryValue() = %s, still contains the uncertified text", redacted)
			}
		})
	}
}

// TestAnalyzerTimeoutRedactsConservatively is the same rule for the failure
// mode that matters most in production: the analyzer is up, but slow.
func TestAnalyzerTimeoutRedactsConservatively(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		<-release
		writer.WriteHeader(http.StatusOK)
		if _, err := writer.Write([]byte("[]")); err != nil {
			t.Errorf("writing the analyzer response: %v", err)
		}
	}))
	// Registered after the server exists, so it runs before Close: httptest
	// waits for outstanding requests, and a blocked handler would deadlock the
	// cleanup rather than fail the test.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	analyzer, err := NewAnalyzer(AnalyzerConfig{Endpoint: server.URL, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v, want nil", err)
	}
	policy := newPolicy(t, Config{Analyzer: analyzer})

	if redacted := policy.RedactBoundaryText(t.Context(), "Paged the on-call."); redacted != AnalyzerUnavailableMask {
		t.Errorf("RedactBoundaryText() = %q, want %q", redacted, AnalyzerUnavailableMask)
	}
}

// TestAnalyzerUnreachableRedactsConservatively covers the analyzer that is not
// running at all — the state a misconfigured deployment is actually in.
func TestAnalyzerUnreachableRedactsConservatively(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()

	analyzer, err := NewAnalyzer(AnalyzerConfig{Endpoint: endpoint, Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v, want nil", err)
	}

	if _, err := analyzer.Analyze(t.Context(), "text", []entity{entityPerson}); !errors.Is(err, ErrAnalyzerUnavailable) {
		t.Errorf("Analyze() error = %v, want it to wrap %v", err, ErrAnalyzerUnavailable)
	}
	policy := newPolicy(t, Config{Analyzer: analyzer})
	if redacted := policy.RedactPersistedText(t.Context(), "text"); redacted != AnalyzerUnavailableMask {
		t.Errorf("RedactPersistedText() = %q, want %q", redacted, AnalyzerUnavailableMask)
	}
}

// TestAnalyzerSpansAreConvertedFromCodePointsToBytes is the bug this client
// would otherwise ship silently: Presidio counts characters the way Python
// does, Go counts bytes, and any non-ASCII character ahead of a span moves the
// mask if the units are not converted.
func TestAnalyzerSpansAreConvertedFromCodePointsToBytes(t *testing.T) {
	t.Parallel()

	const text = "Café escalation owner John reported it"
	stub := &analyzerStub{handler: func(_ *testing.T, request analyzeRequest) (int, string) {
		return http.StatusOK, analyzerAnswer(request.Text, "John", entityPerson)
	}}

	redacted := withAnalyzer(t, stub).RedactBoundaryText(t.Context(), text)
	want := "Café escalation owner " + entityPerson.mask() + " reported it"
	if redacted != want {
		t.Errorf("RedactBoundaryText() = %q, want %q", redacted, want)
	}
}

// TestNewAnalyzerRejectsAnUnusableEndpoint keeps a misconfiguration a startup
// failure rather than a silent layer that never fires.
func TestNewAnalyzerRejectsAnUnusableEndpoint(t *testing.T) {
	t.Parallel()

	for _, invalid := range []struct {
		name     string
		endpoint string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"not a URL", "://"},
		{"wrong scheme", "ftp://analyzer:3000"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewAnalyzer(AnalyzerConfig{Endpoint: invalid.endpoint}); !errors.Is(err, ErrIncompleteConfig) {
				t.Errorf("NewAnalyzer(%q) error = %v, want it to wrap %v", invalid.endpoint, err, ErrIncompleteConfig)
			}
		})
	}
}

// TestAnalyzerPostsToTheDetectionEndpoint pins the wire contract the Presidio
// image serves, so a change here is a deliberate one.
func TestAnalyzerPostsToTheDetectionEndpoint(t *testing.T) {
	t.Parallel()

	type call struct {
		method   string
		path     string
		mimeType string
	}
	seen := make(chan call, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen <- call{
			method:   request.Method,
			path:     request.URL.Path,
			mimeType: request.Header.Get("Content-Type"),
		}
		if _, err := writer.Write([]byte("[]")); err != nil {
			t.Errorf("writing the analyzer response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	// A trailing slash on the configured endpoint must not produce a double
	// slash in the path: a reverse proxy in front of the analyzer may not
	// normalize it.
	analyzer, err := NewAnalyzer(AnalyzerConfig{Endpoint: server.URL + "/"})
	if err != nil {
		t.Fatalf("NewAnalyzer() error = %v, want nil", err)
	}
	if _, err := analyzer.Analyze(t.Context(), "text", []entity{entityPerson}); err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}

	got := <-seen
	if got.method != http.MethodPost || got.path != analyzePath || got.mimeType != "application/json" {
		t.Errorf("analyzer call = %+v, want POST %s as application/json", got, analyzePath)
	}
}
