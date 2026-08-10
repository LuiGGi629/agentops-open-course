package releasecheck

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

func VerifyAttestations(document, sbom any) (int, error) {
	envelopes, ok := document.([]any)
	if !ok {
		envelopes = []any{document}
	}
	if len(envelopes) == 0 {
		return 0, errors.New("no policy-valid SPDX attestations were returned")
	}
	matched := false
	for _, rawEnvelope := range envelopes {
		envelope, ok := rawEnvelope.(map[string]any)
		if !ok {
			return 0, errors.New("every verified attestation needs one base64 payload")
		}
		payload, ok := envelope["payload"].(string)
		if !ok {
			return 0, errors.New("every verified attestation needs one base64 payload")
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(payload)
		if err != nil {
			return 0, fmt.Errorf("decoding attestation payload: %w", err)
		}
		var statement map[string]any
		if err := json.Unmarshal(decoded, &statement); err != nil {
			return 0, fmt.Errorf("decoding attestation statement: %w", err)
		}
		predicate, exists := statement["predicate"]
		if !exists {
			return 0, errors.New("every verified attestation payload needs a predicate")
		}
		matched = matched || reflect.DeepEqual(predicate, sbom)
	}
	if !matched {
		return 0, errors.New("no policy-valid SPDX attestation matches the release SBOM asset")
	}
	return len(envelopes), nil
}
