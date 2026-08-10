package releasecheck

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func envelope(t *testing.T, predicate any) map[string]any {
	t.Helper()
	statement, err := json.Marshal(map[string]any{"predicate": predicate})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"payload": base64.StdEncoding.EncodeToString(statement)}
}

func TestVerifyAttestationsAcceptsOneMatchingEnvelope(t *testing.T) {
	sbom := map[string]any{"name": "release", "packages": []any{}}
	count, err := VerifyAttestations([]any{envelope(t, map[string]any{"name": "other"}), envelope(t, sbom)}, sbom)
	if err != nil || count != 2 {
		t.Fatalf("VerifyAttestations() = %d, %v", count, err)
	}
}

func TestVerifyAttestationsFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		document any
		want     string
	}{
		{"mismatch", envelope(t, map[string]any{"name": "other"}), "matches the release SBOM"},
		{"missing payload", map[string]any{}, "base64 payload"},
		{"bad base64", map[string]any{"payload": "%%%"}, "decoding attestation payload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := VerifyAttestations(test.document, map[string]any{"name": "release"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
