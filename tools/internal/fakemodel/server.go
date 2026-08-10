package fakemodel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	ResponsesPath = "/v1/responses"
	ModelsPath    = "/v1/models"
	HealthPath    = "/healthz"
	ReplyText     = "Fake model response for platform latency measurement."

	RawRequestPIIReply     = "FAKE_MODEL_SAW_UNMASKED_REQUEST_PII"
	MaskedRequestPIIReply  = "FAKE_MODEL_SAW_MASKED_REQUEST_PII"
	ResponseMaskProbe      = "response-mask-probe"
	ResponsePIIReply       = "Contact response-mask@example.test"
	ResponseRejectProbe    = "response-reject-probe"
	ResponseRejectPIIReply = "Contact reject-probe@example.invalid"

	requestMaskProbe = "request-mask-probe"
	requestMaskPII   = "request-mask@example.test"
	defaultModel     = "agentops-fake"
)

type request struct {
	Model string `json:"model"`
	Text  struct {
		Format struct {
			Type string `json:"type"`
		} `json:"format"`
	} `json:"text"`
	Input  json.RawMessage `json:"input"`
	Stream bool            `json:"stream"`
}

type errorEnvelope struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET "+ModelsPath, func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{
			"object": "list",
			"data":   []any{map[string]string{"id": "qwen3:4b-instruct", "object": "model"}},
		})
	})
	mux.HandleFunc("POST "+ResponsesPath, func(writer http.ResponseWriter, incoming *http.Request) {
		defer func() { _ = incoming.Body.Close() }()
		decoder := json.NewDecoder(http.MaxBytesReader(writer, incoming.Body, 1<<20))
		var parsed request
		if err := decoder.Decode(&parsed); err != nil {
			writeError(writer, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
			return
		}
		if decoder.Decode(&struct{}{}) == nil {
			writeError(writer, http.StatusBadRequest, "request body must contain one JSON object")
			return
		}
		if parsed.Stream {
			writeError(writer, http.StatusBadRequest,
				"streaming is intentionally unsupported; keep AGENT_A2A_STREAMING=false")
			return
		}
		if parsed.Model == "" {
			parsed.Model = defaultModel
		}
		writeJSON(writer, http.StatusOK, response(parsed.Model, responseText(parsed)))
	})
	return mux
}

func responseText(parsed request) string {
	if parsed.Text.Format.Type == "json_schema" {
		indexes, ok := namedEntityIndexes(parsed.Input)
		if !ok {
			return `{"items":[]}`
		}
		items := make([]map[string]any, len(indexes))
		for index, itemIndex := range indexes {
			items[index] = map[string]any{"index": itemIndex, "entities": []any{}}
		}
		encoded, err := json.Marshal(map[string]any{"items": items})
		if err != nil {
			return `{"items":[]}`
		}
		return string(encoded)
	}
	input := parsed.Input
	switch {
	case bytes.Contains(input, []byte(requestMaskProbe)):
		if bytes.Contains(input, []byte(requestMaskPII)) {
			return RawRequestPIIReply
		}
		return MaskedRequestPIIReply
	case bytes.Contains(input, []byte(ResponseMaskProbe)):
		return ResponsePIIReply
	case bytes.Contains(input, []byte(ResponseRejectProbe)):
		return ResponseRejectPIIReply
	default:
		return ReplyText
	}
}

func namedEntityIndexes(input json.RawMessage) ([]int, bool) {
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		return nil, false
	}
	var visit func(any) ([]int, bool)
	visit = func(candidate any) ([]int, bool) {
		switch typed := candidate.(type) {
		case string:
			var payload struct {
				Items []struct {
					Index int `json:"index"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(typed), &payload); err == nil && payload.Items != nil {
				indexes := make([]int, len(payload.Items))
				for index, item := range payload.Items {
					indexes[index] = item.Index
				}
				return indexes, true
			}
		case []any:
			for _, item := range typed {
				if indexes, ok := visit(item); ok {
					return indexes, true
				}
			}
		case map[string]any:
			for _, item := range typed {
				if indexes, ok := visit(item); ok {
					return indexes, true
				}
			}
		}
		return nil, false
	}
	return visit(value)
}

func response(model, text string) map[string]any {
	return map[string]any{
		"id":                  "resp-agentops-fake",
		"object":              "response",
		"created_at":          0,
		"completed_at":        0,
		"status":              "completed",
		"model":               model,
		"error":               nil,
		"incomplete_details":  nil,
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"tools":               []any{},
		"output": []any{map[string]any{
			"id": "msg-agentops-fake", "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{
				"type": "output_text", "text": text, "annotations": []any{},
			}},
		}},
		"usage": map[string]int{"input_tokens": 10, "output_tokens": 8, "total_tokens": 18},
	}
}

func writeError(writer http.ResponseWriter, status int, message string) {
	payload := errorEnvelope{}
	payload.Error.Message = message
	writeJSON(writer, status, payload)
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		http.Error(writer, "could not encode response", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(encoded)
}

func Server(address string, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}
