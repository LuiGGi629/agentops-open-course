package fakemodel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestResponsesContract(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, ResponsesPath,
		strings.NewReader(`{"model":"qwen3:4b-instruct","stream":false}`))
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if document["status"] != "completed" || document["model"] != "qwen3:4b-instruct" {
		t.Fatalf("response = %#v", document)
	}
	outputs, ok := document["output"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("output = %#v", document["output"])
	}
	output, ok := outputs[0].(map[string]any)
	if !ok {
		t.Fatalf("output item = %#v", outputs[0])
	}
	contents, ok := output["content"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("content = %#v", output["content"])
	}
	content, ok := contents[0].(map[string]any)
	if !ok {
		t.Fatalf("content item = %#v", contents[0])
	}
	if content["text"] != ReplyText {
		t.Errorf("text = %q, want %q", content["text"], ReplyText)
	}
}

func TestRejectsStreamingAndUnknownPaths(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"streaming", ResponsesPath, `{"stream":true}`, http.StatusBadRequest},
		{"malformed", ResponsesPath, `{`, http.StatusBadRequest},
		{"wrong path", "/v1/chat/completions", `{}`, http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, HealthPath, nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok"`) {
		t.Fatalf("health = %d %s", response.Code, response.Body)
	}
}

func TestModelsListsTheCourseModel(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, ModelsPath, nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"qwen3:4b-instruct"`) {
		t.Fatalf("models = %d %s", response.Code, response.Body)
	}
}

func TestGuardrailProbeResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"unmasked request", `request-mask-probe request-mask@example.test`, RawRequestPIIReply},
		{"masked request", `request-mask-probe <EMAIL>`, MaskedRequestPIIReply},
		{"response mask", ResponseMaskProbe, ResponsePIIReply},
		{"response reject", ResponseRejectProbe, ResponseRejectPIIReply},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := strings.NewReader(`{"model":"qwen3:4b-instruct","input":` +
				strconv.Quote(test.input) + `,"stream":false}`)
			request := httptest.NewRequest(http.MethodPost, ResponsesPath, body)
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("response = %d %s, want %q", response.Code, response.Body, test.want)
			}
		})
	}
}

func TestNamedEntityProbeReturnsOneEmptyResultPerInput(t *testing.T) {
	t.Parallel()
	body := `{
		"model":"qwen3:4b-instruct",
		"input":[{"role":"user","content":[{"type":"input_text","text":"{\"items\":[{\"index\":0,\"text\":\"hello\"},{\"index\":1,\"text\":\"world\"}]}"}]}],
		"stream":false,
		"text":{"format":{"type":"json_schema"}}
	}`
	request := httptest.NewRequest(http.MethodPost, ResponsesPath, strings.NewReader(body))
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("response = %d %s", response.Code, response.Body)
	}
	var document struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Output) != 1 || len(document.Output[0].Content) != 1 {
		t.Fatalf("NER response = %#v", document)
	}
	var result struct {
		Items []struct {
			Entities []any `json:"entities"`
			Index    int   `json:"index"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(document.Output[0].Content[0].Text), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 || result.Items[0].Index != 0 || result.Items[1].Index != 1 ||
		len(result.Items[0].Entities) != 0 || len(result.Items[1].Entities) != 0 {
		t.Fatalf("NER result = %#v", result)
	}
}
