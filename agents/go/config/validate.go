package config

import (
	"cmp"
	"fmt"
	"strings"
)

// Problem is one actionable configuration defect: which variable is at fault
// and what to do about it.
//
// Variable is empty for problems that no single variable owns — a provider
// selected without its credentials, a fallback model equal to the primary.
type Problem struct {
	Variable string
	Message  string
}

// String renders one problem the way an operator reads it.
func (p Problem) String() string {
	if p.Variable == "" {
		return p.Message
	}
	return p.Variable + ": " + p.Message
}

// ValidationError reports every problem found in one phase of Load.
//
// Problems accumulate within a phase and are reported whole: a learner with
// three bad values sees all three, not the first one three times over three
// edit-and-rerun cycles.
type ValidationError struct{ Problems []Problem }

// Error renders every problem, one per line.
func (e *ValidationError) Error() string {
	rendered := make([]string, 0, len(e.Problems)+1)
	rendered = append(rendered, fmt.Sprintf("agent configuration is invalid (%d problems)", len(e.Problems)))
	for _, problem := range e.Problems {
		rendered = append(rendered, "  - "+problem.String())
	}
	return strings.Join(rendered, "\n")
}

// problems accumulates defects within one validation phase.
type problems []Problem

func (p *problems) add(variable, format string, args ...any) {
	*p = append(*p, Problem{Variable: variable, Message: fmt.Sprintf(format, args...)})
}

// addCrossField records a defect that no single variable owns.
func (p *problems) addCrossField(format string, args ...any) {
	p.add("", format, args...)
}

// closedRange records a problem when value falls outside the inclusive bounds,
// mirroring pydantic's ge/le.
func closedRange[T cmp.Ordered](found *problems, variable string, value, low, high T) {
	if value < low || value > high {
		found.add(variable, "must be between %v and %v, got %v", low, high, value)
	}
}

// positiveAtMost records a problem when value is not strictly above zero or
// exceeds high, mirroring pydantic's gt=0/le. Every deadline uses this rather
// than closedRange: a zero-second deadline can never be met.
func positiveAtMost(found *problems, variable string, value, high Seconds) {
	if value <= 0 || value > high {
		found.add(variable, "must be greater than 0 and at most %v, got %v", high, value)
	}
}

// notEmpty records a problem when a required string is blank.
func notEmpty(found *problems, variable, value string) {
	if value == "" {
		found.add(variable, "must not be empty")
	}
}

// fieldProblems checks every per-field constraint the Python track expresses as
// a pydantic Field bound. These run before the cross-field phases, so a bad
// bound never reaches logic that assumes a sane value.
func (c Config) fieldProblems() []Problem {
	var found problems

	// The enum types reject unknown values while parsing, so these two only fire
	// for a Config built in code or for a variable set to the empty string —
	// neither of which may be allowed to select a silently wrong adapter.
	if !c.Entrypoint.Valid() {
		found.add(EnvEntrypoint, "must be one of %s; got %q", joinValues(Entrypoints()), c.Entrypoint)
	}
	if !c.ModelProvider.Valid() {
		found.add(EnvModelProvider, "must be one of %s; got %q", joinValues(ModelProviders()), c.ModelProvider)
	}

	notEmpty(&found, EnvModel, c.Model)
	// Python resolves an empty path to the working directory; Go rejects it.
	// An empty seed or state directory is never what an operator meant, and
	// failing here beats a confusing "seed database is missing: /incidents.db"
	// from the data layer three calls later.
	notEmpty(&found, EnvDataDir, c.DataDir)
	notEmpty(&found, EnvStateDir, c.StateDir)
	notEmpty(&found, EnvA2ABindHost, c.A2ABindHost)
	notEmpty(&found, EnvA2AHost, c.A2AHost)
	notEmpty(&found, EnvEmbeddingsURL, c.EmbeddingsURL)
	notEmpty(&found, EnvEmbeddingModel, c.EmbeddingModel)

	closedRange(&found, EnvA2APort, c.A2APort, 1, 65535)
	closedRange(&found, EnvA2AMaxLLMCalls, c.A2AMaxLLMCalls, 1, 100)
	closedRange(&found, EnvMaxRetries, c.MaxRetries, 0, 10)
	closedRange(&found, EnvCircuitFailureThreshold, c.CircuitFailureThreshold, 1, 100)

	positiveAtMost(&found, EnvDrainTimeout, c.DrainTimeout, 300)
	positiveAtMost(&found, EnvModelTimeout, c.ModelTimeout, 600)
	positiveAtMost(&found, EnvToolTimeout, c.ToolTimeout, 600)
	positiveAtMost(&found, EnvRetryBackoff, c.RetryBackoff, 30)
	positiveAtMost(&found, EnvCircuitResetTimeout, c.CircuitResetTimeout, 600)
	positiveAtMost(&found, EnvEmbeddingTimeout, c.EmbeddingTimeout, 600)

	if c.A2AProtocol != "http" && c.A2AProtocol != "https" {
		found.add(EnvA2AProtocol, "must be http or https, got %q", c.A2AProtocol)
	}
	if c.InputPricePer1K < 0 {
		found.add(EnvInputPricePer1K, "must not be negative, got %v", c.InputPricePer1K)
	}
	if c.OutputPricePer1K < 0 {
		found.add(EnvOutputPricePer1K, "must not be negative, got %v", c.OutputPricePer1K)
	}

	// Optional settings are only checked when they are actually set; unset is a
	// valid, documented state for every pointer field.
	if c.ModelTemperature != nil {
		closedRange(&found, EnvModelTemperature, *c.ModelTemperature, 0, 2)
	}
	if c.TrustedIdentityHeader != nil {
		notEmpty(&found, EnvTrustedIdentityHeader, *c.TrustedIdentityHeader)
	}
	if c.ModelFallback != nil {
		notEmpty(&found, EnvModelFallback, *c.ModelFallback)
	}
	if c.MaxHistoryMessages != nil && *c.MaxHistoryMessages < 2 {
		found.add(EnvMaxHistoryMessages, "must be at least 2, got %d", *c.MaxHistoryMessages)
	}
	if c.MaxTokensPerSession != nil && *c.MaxTokensPerSession < 1 {
		found.add(EnvMaxTokensPerSession, "must be at least 1, got %d", *c.MaxTokensPerSession)
	}
	return found
}

// providerProblems rejects model-provider combinations that cannot authenticate.
//
// This phase is reported on its own and short-circuits the rest: with no usable
// credentials there is nothing worth saying about MCP routes or fallback models.
func (c Config) providerProblems() []Problem {
	var found problems

	// Any value at all — including "false" and the empty string — is a migration
	// signal. Someone who wrote this variable is following stale instructions and
	// must be told so, never handed a silent default.
	if c.DeprecatedGatewayEnabled != nil {
		found.addCrossField(
			"AGENT_GATEWAY_ENABLED was removed. Keep AGENT_MODEL_PROVIDER=openai-compatible " +
				"and select direct Ollama or agentgateway with OPENAI_BASE_URL " +
				"(http://127.0.0.1:11434/v1 or http://127.0.0.1:4000/v1).",
		)
	}
	if c.ModelProvider == ProviderOpenAICompatible && c.OpenAIBaseURL == "" {
		found.addCrossField(
			"AGENT_MODEL_PROVIDER=openai-compatible requires OPENAI_BASE_URL. Use " +
				"http://127.0.0.1:11434/v1 for direct Ollama or http://127.0.0.1:4000/v1 " +
				"for the host agentgateway model route.",
		)
	}
	if c.ModelProvider == ProviderOpenAICompatible && strings.TrimSpace(c.OpenAIAPIKey.Reveal()) == "" {
		found.addCrossField(
			"AGENT_MODEL_PROVIDER=openai-compatible requires OPENAI_API_KEY. Ollama and the open " +
				"local gateway accept a non-secret marker such as local-ollama.",
		)
	}

	googleAPIKey := strings.TrimSpace(c.GoogleAPIKey.Reveal())
	if c.ModelProvider == ProviderGemini {
		switch {
		case googleAPIKey != "" && c.GoogleGenAIUseEnterprise:
			found.addCrossField(
				"AGENT_MODEL_PROVIDER=gemini cannot combine GOOGLE_API_KEY with " +
					"GOOGLE_GENAI_USE_ENTERPRISE=true in this course. Choose AI Studio API-key auth " +
					"or the ADC-backed enterprise path.",
			)
		case c.GoogleGenAIUseEnterprise:
			var missing []string
			for _, required := range []struct{ variable, value string }{
				{EnvGoogleCloudProject, c.GoogleCloudProject},
				{EnvGoogleCloudLocation, c.GoogleCloudLocation},
			} {
				if strings.TrimSpace(required.value) == "" {
					missing = append(missing, required.variable)
				}
			}
			if len(missing) > 0 {
				found.addCrossField(
					"AGENT_MODEL_PROVIDER=gemini with GOOGLE_GENAI_USE_ENTERPRISE=true requires %s "+
						"for the ADC-backed course path.", strings.Join(missing, " and "),
				)
			}
		case googleAPIKey == "":
			found.addCrossField(
				"AGENT_MODEL_PROVIDER=gemini requires either GOOGLE_API_KEY for AI Studio, or " +
					"GOOGLE_GENAI_USE_ENTERPRISE=true with GOOGLE_CLOUD_PROJECT and " +
					"GOOGLE_CLOUD_LOCATION for ADC.",
			)
		}
	}
	return found
}

// crossFieldProblems rejects combinations that parse but cannot work.
func (c Config) crossFieldProblems() []Problem {
	var found problems

	if c.PromptURI != "" && !strings.HasPrefix(c.PromptURI, "prompts:/") {
		found.addCrossField(
			"AGENT_PROMPT_URI must look like prompts:/agentops-agent-instruction/2, got %q. "+
				"Unset it to use the committed instruction.", c.PromptURI,
		)
	}
	if c.MCPURL != "" && !isHTTPURL(c.MCPURL) {
		found.addCrossField(
			"AGENT_MCP_URL must be an http(s) URL such as http://127.0.0.1:3000/mcp, got %q. "+
				"Unset it to call the six read tools directly, in process.", c.MCPURL,
		)
	}
	// The Go track adds this variable (there is no Presidio in the pure-Go
	// binary), so it also owns its rule. Same shape as AGENT_MCP_URL: a
	// misconfigured analyzer must fail at startup, not on the first redaction.
	if c.PIIAnalyzerURL != "" && !isHTTPURL(c.PIIAnalyzerURL) {
		found.addCrossField(
			"AGENT_PII_ANALYZER_URL must be an http(s) URL such as http://127.0.0.1:3000, got %q. "+
				"Unset it to rely on in-process redaction alone.", c.PIIAnalyzerURL,
		)
	}
	if c.ModelFallback != nil && *c.ModelFallback == c.Model {
		found.addCrossField(
			"AGENT_MODEL_FALLBACK must differ from AGENT_MODEL (both are %q); "+
				"a fallback identical to the primary adds no resilience. "+
				"Unset it or pick a distinct model.", c.Model,
		)
	}
	return found
}

// isHTTPURL reports whether value carries an http or https scheme.
func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
