package evals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayJudgeUsesGatewayAndStrictVerdict(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer marker" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "judge" || !strings.Contains(payload.Messages[0].Content, "untrusted data") {
			t.Fatalf("unsafe judge request: %+v", payload)
		}
		writeTestJSON(t, writer, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": `{"passed":true,"rationale":"grounded"}`}}},
		})
	}))
	defer server.Close()
	judge, err := NewGatewayJudge(GatewayJudgeConfig{
		BaseURL: server.URL + "/v1", Model: "judge", APIKey: "marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	verdict, err := judge.Judge(context.Background(), JudgeInput{
		Questions: []string{"q"}, Answers: []string{"a"}, ReferenceAnswers: []string{"r"},
	})
	if err != nil || !verdict.Passed || verdict.Rationale != "grounded" {
		t.Fatalf("verdict/error = %+v/%v", verdict, err)
	}
}

func TestJudgeVerdictRejectsUnknownOrMissingFields(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		`{"passed":true,"rationale":"ok","score":1}`,
		`{"rationale":"ok"}`,
		`{"passed":true,"rationale":""}`,
		`{"passed":true,"rationale":"ok"} {}`,
	} {
		if _, err := parseJudgeVerdict(content); err == nil {
			t.Fatalf("invalid verdict accepted: %s", content)
		}
	}
}

func TestJudgeVerdictErrorsOmitProviderControlledFields(t *testing.T) {
	t.Parallel()

	const marker = "SYNTHETIC_PROVIDER_FIELD_DO_NOT_LOG"
	for name, content := range map[string]string{
		"unknown field":  `{"passed":true,"rationale":"ok","` + marker + `":true}`,
		"malformed tail": `{"passed":true,"rationale":"ok"} @` + marker,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseJudgeVerdict(content)
			if err == nil {
				t.Fatal("parseJudgeVerdict() error = nil, want invalid provider JSON to fail")
			}
			if got, want := err.Error(), "decode judge verdict"; got != want {
				t.Fatalf("parseJudgeVerdict() error = %q, want fixed local class %q", got, want)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("parseJudgeVerdict() error retained a provider-controlled field")
			}
		})
	}
}

func TestCalibrationArtifactIsSanitizedAndEnforcesFloor(t *testing.T) {
	t.Parallel()
	set, err := LoadCalibrationSet("judge-calibration.json")
	if err != nil {
		t.Fatal(err)
	}
	result, err := Calibrate(
		context.Background(), set, fixedJudge{pass: true},
		ModelEvidence{Provider: "openai-compatible", Name: "judge", Digest: strings.Repeat("a", 64)},
		testSourceEvidence(), "test-judge", 0.75,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed() || result.Matches != 4 || result.Total != 12 {
		t.Fatalf("result = %+v", result)
	}
	if result.SchemaVersion != 3 || result.JudgeModel.Provider != "openai-compatible" ||
		result.JudgeModel.Digest != strings.Repeat("a", 64) {
		t.Fatalf("judge identity = %#v", result.JudgeModel)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"question", "reference_answer", "answer", "rationale"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("calibration artifact leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestCalibrationRejectsMutableJudgeIdentity(t *testing.T) {
	t.Parallel()
	set, err := LoadCalibrationSet("judge-calibration.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Calibrate(
		context.Background(), set, fixedJudge{pass: true},
		ModelEvidence{Provider: "openai-compatible", Name: "judge"},
		testSourceEvidence(), "test-judge", 0.75,
	)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("mutable judge error = %v", err)
	}
}

type fixedJudge struct{ pass bool }

func (judge fixedJudge) Judge(context.Context, JudgeInput) (JudgeVerdict, error) {
	return JudgeVerdict{Passed: judge.pass, Rationale: "not serialized"}, nil
}
