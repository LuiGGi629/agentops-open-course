package evals

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestFoldEventsExcludesStreamingPartials(t *testing.T) {
	t.Parallel()
	events := logicalEvents()
	turn := FoldEvents(events)
	if turn.Text != "inventory is down" {
		t.Fatalf("Text = %q", turn.Text)
	}
	if got := turn.ToolNames(); !reflect.DeepEqual(got, []string{"get_service_status"}) {
		t.Fatalf("ToolNames = %v", got)
	}
	if turn.Usage != (Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10, ModelCalls: 1}) {
		t.Fatalf("Usage = %+v", turn.Usage)
	}
	if len(turn.Events) != 2 || !turn.Events[0].Partial {
		t.Fatalf("raw Events did not retain partial capture: %+v", turn.Events)
	}
}

func TestFoldEventsTracksProviderFailureAndConfirmation(t *testing.T) {
	t.Parallel()
	confirmation := Event{Content: &Content{Parts: []Part{{FunctionCall: &FunctionCall{
		Name: ConfirmationTool, ID: "call-2", Args: map[string]any{"tool": "restart_service"},
	}}}}}
	turn := FoldEvents([]Event{confirmation, {ErrorCode: "MODEL_ERROR", ErrorMessage: "opaque"}})
	if !turn.Failed() || turn.ErrorCode != "MODEL_ERROR" || turn.AwaitingConfirmation == nil {
		t.Fatalf("unexpected folded failure/confirmation: %+v", turn)
	}
}

func TestFoldEventsRejectsInvalidUsageMetadata(t *testing.T) {
	t.Parallel()

	for name, events := range map[string][]Event{
		"negative": {{UsageMetadata: &UsageMetadata{PromptTokenCount: -1}}},
		"overflow": {
			{UsageMetadata: &UsageMetadata{TotalTokenCount: math.MaxInt64}},
			{UsageMetadata: &UsageMetadata{TotalTokenCount: 1}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			turn := FoldEvents(events)
			if turn.ErrorCode != "INVALID_USAGE" {
				t.Fatalf("ErrorCode = %q, want INVALID_USAGE", turn.ErrorCode)
			}
			if turn.Usage.InputTokens < 0 || turn.Usage.TotalTokens < 0 {
				t.Fatalf("invalid usage escaped folding: %+v", turn.Usage)
			}
		})
	}
}

func TestRESTAndA2ATransportsFoldEquivalentTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	restServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case strings.Contains(request.URL.Path, "/sessions"):
			writeTestJSON(t, writer, map[string]any{"id": "session-1"})
		case request.URL.Path == "/run":
			writeTestJSON(t, writer, logicalEvents())
		default:
			http.NotFound(writer, request)
		}
	}))
	defer restServer.Close()
	rest, err := NewRESTClient(RESTClientConfig{BaseURL: restServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	session, err := rest.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restTurn, err := rest.Send(ctx, session, "status")
	if err != nil {
		t.Fatal(err)
	}

	a2aServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var envelope struct {
			Method string `json:"method"`
			ID     string `json:"id"`
		}
		if decodeErr := json.NewDecoder(request.Body).Decode(&envelope); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if envelope.Method != "message/send" {
			t.Fatalf("method = %q", envelope.Method)
		}
		writeTestJSON(t, writer, map[string]any{
			"jsonrpc": "2.0", "id": envelope.ID, "result": logicalA2AResult(),
		})
	}))
	defer a2aServer.Close()
	a2a, err := NewA2AClient(A2AClientConfig{BaseURL: a2aServer.URL})
	if err != nil {
		t.Fatal(err)
	}
	a2aSession, err := a2a.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a2aTurn, err := a2a.Send(ctx, a2aSession, "status")
	if err != nil {
		t.Fatal(err)
	}
	restTurn.Events = nil
	a2aTurn.Events = nil
	if !reflect.DeepEqual(restTurn, a2aTurn) {
		t.Fatalf("normalized turns differ\nREST: %+v\nA2A: %+v", restTurn, a2aTurn)
	}
}

func TestA2AClientRejectsInvalidJSONRPCEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		jsonRPC    string
		responseID func(string) string
		wantError  string
	}{
		{
			name:       "protocol version",
			jsonRPC:    "1.0",
			responseID: func(requestID string) string { return requestID },
			wantError:  "invalid JSON-RPC version",
		},
		{
			name:       "response id",
			jsonRPC:    "2.0",
			responseID: func(string) string { return "unrelated-request" },
			wantError:  "JSON-RPC id mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var envelope struct {
					ID string `json:"id"`
				}
				if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
					t.Fatal(err)
				}
				writeTestJSON(t, writer, map[string]any{
					"jsonrpc": test.jsonRPC,
					"id":      test.responseID(envelope.ID),
					"result":  logicalA2AResult(),
				})
			}))
			defer server.Close()

			client, err := NewA2AClient(A2AClientConfig{BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Send(context.Background(), "session", "status")
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Send() error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestRESTStreamingUsesRunSSE(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/run_sse" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range logicalEvents() {
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = writer.Write([]byte("data: " + string(encoded) + "\n\n"))
		}
	}))
	defer server.Close()
	client, err := NewRESTClient(RESTClientConfig{BaseURL: server.URL, Streaming: true})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := client.Send(context.Background(), "session", "status")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Usage.TotalTokens != 10 || turn.Text != "inventory is down" {
		t.Fatalf("turn = %+v", turn)
	}
}

func TestClientsRejectCredentialURLs(t *testing.T) {
	t.Parallel()
	if _, err := NewRESTClient(RESTClientConfig{BaseURL: "http://secret@example.test"}); err == nil {
		t.Fatal("REST credential URL accepted")
	}
	if _, err := NewA2AClient(A2AClientConfig{BaseURL: "file:///tmp/socket"}); err == nil {
		t.Fatal("A2A non-HTTP URL accepted")
	}
}

func logicalEvents() []Event {
	return []Event{
		{
			Partial:       true,
			Content:       &Content{Role: "model", Parts: []Part{{Text: "inventory"}}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 2, CandidatesTokenCount: 1, TotalTokenCount: 3},
		},
		{
			Content: &Content{Role: "model", Parts: []Part{
				{FunctionCall: &FunctionCall{Name: "get_service_status", ID: "call-1", Args: map[string]any{"name": "inventory"}}},
				{FunctionResponse: &FunctionResponse{Name: "get_service_status", ID: "call-1", Response: map[string]any{"status": "down"}}},
				{Text: "inventory is down"},
			}},
			UsageMetadata: &UsageMetadata{PromptTokenCount: 7, CandidatesTokenCount: 3, TotalTokenCount: 10},
		},
	}
}

func logicalA2AResult() map[string]any {
	return map[string]any{
		"kind": "task", "id": "task-1", "contextId": "context-1",
		"artifacts": []any{
			map[string]any{
				"metadata": map[string]any{
					"adk_partial": true,
					"adk_usage_metadata": map[string]any{
						"promptTokenCount": 2, "candidatesTokenCount": 1, "totalTokenCount": 3,
					},
				},
				"parts": []any{map[string]any{"kind": "text", "text": "inventory"}},
			},
			map[string]any{
				"metadata": map[string]any{
					"adk_usage_metadata": map[string]any{
						"promptTokenCount": 7, "candidatesTokenCount": 3, "totalTokenCount": 10,
					},
				},
				"parts": []any{
					map[string]any{
						"kind": "data", "metadata": map[string]any{"adk_type": "function_call"},
						"data": map[string]any{"name": "get_service_status", "id": "call-1", "args": map[string]any{"name": "inventory"}},
					},
					map[string]any{
						"kind": "data", "metadata": map[string]any{"adk_type": "function_response"},
						"data": map[string]any{"name": "get_service_status", "id": "call-1", "response": map[string]any{"status": "down"}},
					},
					map[string]any{"kind": "text", "text": "inventory is down"},
				},
			},
		},
	}
}

func writeTestJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatal(err)
	}
}
