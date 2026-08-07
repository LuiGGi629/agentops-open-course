package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// This file is layer 2 of the PII defense (Chapter 4.5): an optional Presidio
// analyzer reached over HTTP, off unless AGENT_PII_ANALYZER_URL is set.
//
// It exists because person, location and nationality detection is named-entity
// recognition, which needs a model. Approximating that with a Go name list
// would be worse than not doing it: it would look like coverage while missing
// every name the list does not contain. So layer 1 says plainly that it does
// not detect names, and this is where the capability plugs in when a deployment
// wants it.
//
// The rule that makes it safe to depend on is fail-closed: a configured
// analyzer that does not answer withholds the text rather than passing it
// through. See [AnalyzerUnavailableMask].

// ErrAnalyzerUnavailable marks every failure to obtain an answer from the
// analyzer — a refused connection, a timeout, a non-OK status, a body that does
// not parse. Callers match on it rather than on the transport error, because
// the policy response is the same in every case.
var ErrAnalyzerUnavailable = errors.New("pii analyzer unavailable")

// analyzePath is the Presidio analyzer's detection endpoint. The image listens
// on port 3000 and serves POST /analyze.
const analyzePath = "/analyze"

// defaultAnalyzerTimeout bounds one detection call.
//
// It is short on purpose. The analyzer sits on the redaction path of every
// model request, every model response and every tool result, so a slow analyzer
// is indistinguishable from a broken agent — and because the failure is
// fail-closed, waiting longer only delays the same outcome.
const defaultAnalyzerTimeout = 2 * time.Second

// defaultAnalyzerLanguage matches the Python track's engine, which loaded the
// small English spaCy model and nothing else.
const defaultAnalyzerLanguage = "en"

// AnalyzerConfig configures the optional layer-2 analyzer.
type AnalyzerConfig struct {
	// HTTPClient overrides the client used for detection calls. Nil builds one
	// bounded by Timeout. A supplied client is used as given, so its own
	// timeout — or lack of one — is the caller's choice.
	HTTPClient *http.Client

	// Endpoint is the analyzer's base URL, for example http://127.0.0.1:3000.
	// Required; an empty endpoint is a configuration error rather than a silent
	// disable, because "off" is expressed by not building an Analyzer at all.
	Endpoint string

	// Language is the analyzer's model language. Empty means English.
	Language string

	// Timeout bounds one detection call when HTTPClient is nil. Zero means
	// [defaultAnalyzerTimeout].
	Timeout time.Duration
}

// Analyzer is a client for a Presidio analyzer service.
type Analyzer struct {
	client   *http.Client
	endpoint string
	language string
}

// NewAnalyzer builds a layer-2 analyzer client.
func NewAnalyzer(cfg AnalyzerConfig) (*Analyzer, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("%w: Analyzer Endpoint is required", ErrIncompleteConfig)
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: Analyzer Endpoint %q is not a URL: %w", ErrIncompleteConfig, cfg.Endpoint, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf(
			"%w: Analyzer Endpoint %q must be an http or https URL", ErrIncompleteConfig, cfg.Endpoint,
		)
	}

	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultAnalyzerTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	language := cfg.Language
	if language == "" {
		language = defaultAnalyzerLanguage
	}
	return &Analyzer{client: client, endpoint: endpoint, language: language}, nil
}

// analyzeRequest is Presidio's POST /analyze body.
type analyzeRequest struct {
	Text     string   `json:"text"`
	Language string   `json:"language"`
	Entities []string `json:"entities,omitempty"`
}

// analyzeResult is one element of Presidio's POST /analyze response. The
// offsets are Python string indices, which count code points, not bytes.
type analyzeResult struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// Analyze asks the service which of the requested classes appear in text.
//
// Every failure is reported as an error wrapping [ErrAnalyzerUnavailable]; the
// method never returns a partial result, because a partial answer from a
// service that is meant to be authoritative for names is the one outcome that
// would make the whole layer misleading.
func (a *Analyzer) Analyze(ctx context.Context, text string, entities []entity) ([]span, error) {
	wanted := make([]string, len(entities))
	for index, name := range entities {
		wanted[index] = string(name)
	}
	body, err := json.Marshal(analyzeRequest{Text: text, Language: a.language, Entities: wanted})
	if err != nil {
		return nil, fmt.Errorf("%w: encoding the request: %w", ErrAnalyzerUnavailable, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+analyzePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: building the request: %w", ErrAnalyzerUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: calling %s: %w", ErrAnalyzerUnavailable, a.endpoint+analyzePath, err)
	}
	defer func() {
		// The body is drained and closed even on the error paths below: an
		// undrained body keeps the connection out of the pool, which turns one
		// slow analyzer into a growing pile of sockets.
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s answered %s", ErrAnalyzerUnavailable, a.endpoint+analyzePath, response.Status)
	}

	var results []analyzeResult
	if err := json.NewDecoder(response.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("%w: decoding the response: %w", ErrAnalyzerUnavailable, err)
	}

	offsets := runeOffsets(text)
	found := make([]span, 0, len(results))
	for _, result := range results {
		start, end, ok := byteRange(offsets, result.Start, result.End)
		if !ok {
			// A span outside the text it was asked about means the two sides
			// disagree about the text. Refusing is the fail-closed reading; the
			// alternative is masking arbitrary characters.
			return nil, fmt.Errorf(
				"%w: %s reported %s at [%d,%d), which is outside the analyzed text",
				ErrAnalyzerUnavailable, a.endpoint+analyzePath, result.EntityType, result.Start, result.End,
			)
		}
		found = append(found, span{entity: entity(result.EntityType), start: start, end: end})
	}
	return found, nil
}

// runeOffsets maps each code-point index of text to its byte offset, with a
// final entry for the end of the string.
//
// Presidio counts characters the way Python does; Go counts bytes. Every span
// this client returns must be in Go's units or the mask lands in the wrong
// place — and silently so, for any text holding a non-ASCII character.
func runeOffsets(text string) []int {
	offsets := make([]int, 0, utf8.RuneCountInString(text)+1)
	for index := range text {
		offsets = append(offsets, index)
	}
	return append(offsets, len(text))
}

// byteRange converts a code-point range to a byte range, reporting whether it
// is within the text.
func byteRange(offsets []int, start, end int) (int, int, bool) {
	if start < 0 || end < start || end >= len(offsets) {
		return 0, 0, false
	}
	return offsets[start], offsets[end], true
}
