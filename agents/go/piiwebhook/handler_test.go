package piiwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type detectorFunc func(context.Context, []string) ([][]Span, error)

func (detect detectorFunc) Detect(ctx context.Context, texts []string) ([][]Span, error) {
	return detect(ctx, texts)
}

func newHandler(t *testing.T, detector Detector, timeout time.Duration) *Handler {
	t.Helper()
	handler, err := New(Config{Detector: detector, Timeout: timeout})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	return handler
}

func post(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func responseJSON(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response body = %q, want JSON: %v", recorder.Body.String(), err)
	}
	return decoded
}

func TestRequestWebhookMasksNamedEntitiesInPlace(t *testing.T) {
	t.Parallel()

	detector := detectorFunc(func(_ context.Context, texts []string) ([][]Span, error) {
		if len(texts) != 2 || texts[0] != "John paged Paris" || texts[1] != "No PII here" {
			t.Fatalf("Detect() texts = %q, want the gateway message contents", texts)
		}
		return [][]Span{
			{
				{Start: 0, End: 4, Entity: EntityPerson},
				{Start: 11, End: 16, Entity: EntityLocation},
			},
			nil,
		}, nil
	})
	handler := newHandler(t, detector, time.Second)

	recorder := post(t, handler, RequestPath, `{
		"body":{"messages":[
			{"role":"user","content":"John paged Paris"},
			{"role":"system","content":"No PII here"}
		]}
	}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want 200", recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	for _, want := range []string{"<PERSON> paged <LOCATION>", "No PII here", "named entities masked"} {
		if !strings.Contains(body, want) {
			t.Errorf("response body = %s, want it to contain %q", body, want)
		}
	}
	for _, gone := range []string{"John", "Paris"} {
		if strings.Contains(body, gone) {
			t.Errorf("response body = %s, still contains %q", body, gone)
		}
	}
}

func TestResponseWebhookPreservesTheGatewayChoiceShape(t *testing.T) {
	t.Parallel()

	detector := detectorFunc(func(_ context.Context, _ []string) ([][]Span, error) {
		return [][]Span{{{Start: 12, End: 21, Entity: EntityOrganization}}}, nil
	})
	handler := newHandler(t, detector, time.Second)

	recorder := post(t, handler, ResponsePath, `{
		"body":{"choices":[
			{"message":{"role":"assistant","content":"Escalate to Acme Corp"}}
		]}
	}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want 200", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{`"choices"`, `"role":"assistant"`, "Escalate to <ORGANIZATION>"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("response body = %s, want it to contain %q", recorder.Body.String(), want)
		}
	}
}

func TestModelFailureMasksEveryNonEmptyValue(t *testing.T) {
	t.Parallel()

	handler := newHandler(t, detectorFunc(func(context.Context, []string) ([][]Span, error) {
		return nil, errors.New("ollama unavailable")
	}), time.Second)
	recorder := post(t, handler, RequestPath,
		`{"body":{"messages":[{"role":"user","content":"Alice is in Paris"}]}}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want a mask action", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), RedactedMask) {
		t.Errorf("response body = %s, want conservative mask %q", recorder.Body.String(), RedactedMask)
	}
	for _, gone := range []string{"Alice", "Paris", "ollama unavailable"} {
		if strings.Contains(recorder.Body.String(), gone) {
			t.Errorf("response body = %s, leaks %q", recorder.Body.String(), gone)
		}
	}
}

func TestDetectionTimeoutMasksInsteadOfPassingThrough(t *testing.T) {
	t.Parallel()

	handler := newHandler(t, detectorFunc(func(ctx context.Context, _ []string) ([][]Span, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), 20*time.Millisecond)

	started := time.Now()
	recorder := post(t, handler, RequestPath,
		`{"body":{"messages":[{"role":"user","content":"Alice is in Paris"}]}}`)
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("webhook took %v, want the configured deadline", elapsed)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), RedactedMask) {
		t.Errorf("status = %d, body = %q, want a conservative mask action",
			recorder.Code, recorder.Body.String())
	}
}

func TestWebhookInputIsStrictAndBounded(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	handler := newHandler(t, detectorFunc(func(context.Context, []string) ([][]Span, error) {
		calls.Add(1)
		return nil, nil
	}), time.Second)

	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"body":{"messages":[]},"ignored":true}`},
		{name: "trailing JSON", body: `{"body":{"messages":[]}} {}`},
		{name: "oversized text", body: `{"body":{"messages":[{"role":"user","content":"` + strings.Repeat("x", DefaultMaxTextBytes+1) + `"}]}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := post(t, handler, RequestPath, testCase.body)
			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d, body = %q, want 400", recorder.Code, recorder.Body.String())
			}
			if got := responseJSON(t, recorder); got["error"] != InvalidRequestMessage {
				t.Errorf("response = %v, want opaque error %q", got, InvalidRequestMessage)
			}
		})
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("detector calls = %d, want 0 for invalid input", got)
	}
}

func TestNoNamedEntitiesReturnsPass(t *testing.T) {
	t.Parallel()

	handler := newHandler(t, detectorFunc(func(_ context.Context, texts []string) ([][]Span, error) {
		return make([][]Span, len(texts)), nil
	}), time.Second)
	recorder := post(t, handler, RequestPath,
		`{"body":{"messages":[{"role":"user","content":"Inspect job 002"}]}}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q, want 200", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"body"`) {
		t.Errorf("response body = %s, want a pass action without a replacement body", recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "no named entities detected") {
		t.Errorf("response body = %s, want a pass reason", recorder.Body.String())
	}
}
