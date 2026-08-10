package piiwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type recordingLLM struct {
	err      error
	request  *adkmodel.LLMRequest
	response string
}

func (m *recordingLLM) Name() string { return "recording" }

func (m *recordingLLM) GenerateContent(
	_ context.Context, request *adkmodel.LLMRequest, stream bool,
) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if stream {
			yield(nil, errors.New("unexpected streaming request"))
			return
		}
		m.request = request
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText(m.response, genai.RoleModel)}, nil)
	}
}

func TestModelDetectorUsesStrictStructuredOutputAndExactText(t *testing.T) {
	t.Parallel()

	model := &recordingLLM{response: `{
		"items":[
			{"index":0,"entities":[
				{"type":"PERSON","text":"Zoë"},
				{"type":"LOCATION","text":"Paris"}
			]},
			{"index":1,"entities":[
				{"type":"ORGANIZATION","text":"Acme"}
			]}
		]
	}`}
	detector, err := NewModelDetector(model)
	if err != nil {
		t.Fatalf("NewModelDetector() error = %v, want nil", err)
	}

	texts := []string{"Zoë left Paris; Zoë returned", "Acme paged Acme"}
	got, err := detector.Detect(t.Context(), texts)
	if err != nil {
		t.Fatalf("Detect() error = %v, want nil", err)
	}
	want := [][]Span{
		{
			{Entity: EntityPerson, Start: 0, End: 4},
			{Entity: EntityLocation, Start: 10, End: 15},
			{Entity: EntityPerson, Start: 17, End: 21},
		},
		{
			{Entity: EntityOrganization, Start: 0, End: 4},
			{Entity: EntityOrganization, Start: 11, End: 15},
		},
	}
	if !slices.EqualFunc(got, want, func(left, right []Span) bool { return slices.Equal(left, right) }) {
		t.Errorf("Detect() spans = %#v, want %#v", got, want)
	}

	request := model.request
	if request == nil || len(request.Contents) != 1 || request.Config == nil {
		t.Fatalf("model request = %#v, want one bounded structured request", request)
	}
	if request.Config.ResponseMIMEType != "application/json" || request.Config.ResponseSchema == nil {
		t.Errorf("response format = (%q, %#v), want application/json plus schema",
			request.Config.ResponseMIMEType, request.Config.ResponseSchema)
	}
	if request.Config.Temperature == nil || *request.Config.Temperature != 0 {
		t.Errorf("temperature = %v, want explicit zero", request.Config.Temperature)
	}
	if request.Config.MaxOutputTokens <= 0 || request.Config.MaxOutputTokens > 2048 {
		t.Errorf("max output tokens = %d, want a positive bounded value", request.Config.MaxOutputTokens)
	}
	if request.Config.SystemInstruction == nil || !strings.Contains(
		request.Config.SystemInstruction.Parts[0].Text, "exact substrings",
	) {
		t.Errorf("system instruction = %#v, want exact-substring constraint", request.Config.SystemInstruction)
	}
	if content := request.Contents[0].Parts[0].Text; !strings.Contains(content, `"index":0`) ||
		!strings.Contains(content, `"text":"Zoë left Paris; Zoë returned"`) {
		t.Errorf("model input = %q, want indexed JSON text", content)
	}
}

func TestModelDetectorRejectsUntrustedOutput(t *testing.T) {
	t.Parallel()

	for name, output := range map[string]string{
		"malformed JSON":      `{"items":[`,
		"unknown field":       `{"items":[{"index":0,"entities":[],"extra":true}]}`,
		"missing index":       `{"items":[]}`,
		"duplicate index":     `{"items":[{"index":0,"entities":[]},{"index":0,"entities":[]}]}`,
		"unknown entity":      `{"items":[{"index":0,"entities":[{"type":"MACHINE","text":"job-001"}]}]}`,
		"hallucinated text":   `{"items":[{"index":0,"entities":[{"type":"PERSON","text":"Alice"}]}]}`,
		"placeholder entity":  `{"items":[{"index":0,"entities":[{"type":"PERSON","text":"<REDACTED>"}]}]}`,
		"trailing JSON value": `{"items":[{"index":0,"entities":[]}]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			detector, err := NewModelDetector(&recordingLLM{response: output})
			if err != nil {
				t.Fatalf("NewModelDetector() error = %v, want nil", err)
			}
			if _, err := detector.Detect(t.Context(), []string{"No name here"}); err == nil {
				t.Fatal("Detect() error = nil, want strict output rejection")
			}
		})
	}
}

func TestOpenAICompatibleDetectorUsesResponsesStructuredOutput(t *testing.T) {
	t.Parallel()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer local-marker" {
			t.Errorf("Authorization = %q, want local marker", authorization)
		}
		if request.URL.Path == "/v1/models" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"object":"list","data":[{"id":"qwen-test","object":"model"}]}`)
			return
		}
		if request.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q, want /v1/responses", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{
			"id":"resp_ner","model":"qwen-test",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"items\":[{\"index\":0,\"entities\":[{\"type\":\"PERSON\",\"text\":\"Alice\"}]}]}"}]}],
			"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}
		}`)
	}))
	defer server.Close()

	detector, err := NewOpenAICompatibleDetector(t.Context(), OpenAIConfig{
		BaseURL: server.URL + "/v1",
		APIKey:  "local-marker",
		Model:   "qwen-test",
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleDetector() error = %v, want nil", err)
	}
	if readyErr := detector.Ready(t.Context()); readyErr != nil {
		t.Fatalf("Ready() error = %v, want the configured model in /v1/models", readyErr)
	}
	got, err := detector.Detect(t.Context(), []string{"Alice responded"})
	if err != nil {
		t.Fatalf("Detect() error = %v, want nil", err)
	}
	if len(got) != 1 || !slices.Equal(got[0], []Span{{Entity: EntityPerson, Start: 0, End: 5}}) {
		t.Errorf("Detect() = %#v, want Alice PERSON span", got)
	}

	format, _ := requestBody["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true {
		t.Errorf("structured format = %#v, want strict json_schema", format)
	}
	if requestBody["model"] != "qwen-test" {
		t.Errorf("model = %#v, want qwen-test", requestBody["model"])
	}
}

func TestOpenAICompatibleDetectorBoundsTheResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, strings.Repeat("x", 2048))
	}))
	defer server.Close()

	detector, err := NewOpenAICompatibleDetector(t.Context(), OpenAIConfig{
		BaseURL:          server.URL + "/v1",
		APIKey:           "local-marker",
		Model:            "qwen-test",
		Timeout:          time.Second,
		MaxResponseBytes: 256,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleDetector() error = %v, want nil", err)
	}
	if _, err := detector.Detect(t.Context(), []string{"Alice responded"}); err == nil {
		t.Fatal("Detect() error = nil, want oversized response rejection")
	}
}

func TestOpenAICompatibleDetectorRefusesCrossOriginRedirects(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var captured atomic.Int64
			target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				captured.Add(1)
				http.Error(writer, "redirect target", http.StatusBadGateway)
			}))
			t.Cleanup(target.Close)
			source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Location", target.URL+"/v1/responses")
				writer.WriteHeader(status)
			}))
			t.Cleanup(source.Close)

			detector, err := NewOpenAICompatibleDetector(t.Context(), OpenAIConfig{
				BaseURL: source.URL + "/v1",
				APIKey:  "redirect-sensitive-pii-token",
				Model:   "qwen-test",
				Timeout: time.Second,
			})
			if err != nil {
				t.Fatalf("NewOpenAICompatibleDetector() error = %v, want nil", err)
			}
			if _, err := detector.Detect(t.Context(), []string{"Alice is private"}); err == nil {
				t.Fatal("Detect() error = nil, want redirect refusal")
			}
			if got := captured.Load(); got != 0 {
				t.Fatalf("redirect target received %d request(s), want zero credential or PII replays", got)
			}
		})
	}
}
