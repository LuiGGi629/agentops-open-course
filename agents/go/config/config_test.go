package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// validBase is the smallest environment that loads cleanly: the shipped
// defaults. Tests copy it and change one thing, so a failure names exactly one
// cause.
func validBase() map[string]string { return map[string]string{} }

// withEnv copies validBase and applies overrides, so no test can leak a value
// into another.
func withEnv(overrides map[string]string) map[string]string {
	environ := validBase()
	for name, value := range overrides {
		environ[name] = value
	}
	return environ
}

// mustLoad fails the test when a configuration that should be valid is not.
func mustLoad(t *testing.T, environ map[string]string) Config {
	t.Helper()
	cfg, err := LoadFrom(environ)
	if err != nil {
		t.Fatalf("LoadFrom(%v) failed: %v", environ, err)
	}
	return cfg
}

// loadError asserts the configuration is rejected and returns the message.
func loadError(t *testing.T, environ map[string]string) string {
	t.Helper()
	cfg, err := LoadFrom(environ)
	if err == nil {
		t.Fatalf("LoadFrom(%v) unexpectedly succeeded: %+v", environ, cfg)
	}
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("LoadFrom(%v) returned %T, want *ValidationError", environ, err)
	}
	if len(invalid.Problems) == 0 {
		t.Fatalf("LoadFrom(%v) returned a ValidationError with no problems", environ)
	}
	// A rejected configuration must never leak a partially built value.
	if cfg != (Config{}) {
		t.Errorf("LoadFrom(%v) returned a non-zero Config alongside its error", environ)
	}
	return err.Error()
}

// assertContains fails with the full message, which is what an operator sees.
func assertContains(t *testing.T, message string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(message, want) {
			t.Errorf("message does not contain %q\nfull message:\n%s", want, message)
		}
	}
}

// cleanEnvironment removes every variable this package reads from the process
// environment, so an ambient developer setup cannot decide a test's outcome.
// The set is derived from the struct, so a new field is isolated automatically.
func cleanEnvironment(t *testing.T) {
	t.Helper()
	for _, spec := range specs() {
		previous, ok := os.LookupEnv(spec.variable)
		if !ok {
			continue
		}
		t.Cleanup(func() {
			if err := os.Setenv(spec.variable, previous); err != nil {
				t.Errorf("restoring %s: %v", spec.variable, err)
			}
		})
		if err := os.Unsetenv(spec.variable); err != nil {
			t.Fatalf("unsetting %s: %v", spec.variable, err)
		}
	}
}

func TestDefaultConfigurationIsValid(t *testing.T) {
	cfg := mustLoad(t, validBase())

	if cfg.Entrypoint != EntrypointAgent {
		t.Errorf("Entrypoint = %q, want %q", cfg.Entrypoint, EntrypointAgent)
	}
	if cfg.ModelProvider != ProviderOpenAICompatible {
		t.Errorf("ModelProvider = %q, want %q", cfg.ModelProvider, ProviderOpenAICompatible)
	}
	if cfg.Model != "qwen3:4b-instruct" {
		t.Errorf("Model = %q, want %q", cfg.Model, "qwen3:4b-instruct")
	}
	if cfg.ModelTemperature != nil {
		t.Errorf("ModelTemperature = %v, want unset", *cfg.ModelTemperature)
	}
	if cfg.OpenAIBaseURL != "http://127.0.0.1:11434/v1" {
		t.Errorf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
	if cfg.OpenAIAPIKey.Reveal() != "local-ollama" {
		t.Errorf("OpenAIAPIKey = %q, want the non-secret local marker", cfg.OpenAIAPIKey.Reveal())
	}
	if cfg.MCPURL != "" {
		t.Errorf("MCPURL = %q, want unset", cfg.MCPURL)
	}
	if cfg.PIIAnalyzerURL != "" {
		t.Errorf("PIIAnalyzerURL = %q, want unset (layer 2 is opt-in)", cfg.PIIAnalyzerURL)
	}
	if cfg.A2ABindHost != "127.0.0.1" {
		t.Errorf("A2ABindHost = %q, want the loopback-only default", cfg.A2ABindHost)
	}
	if cfg.A2AHost != "localhost" {
		t.Errorf("A2AHost = %q", cfg.A2AHost)
	}
	if cfg.EmbeddingTimeout != 120 {
		t.Errorf("EmbeddingTimeout = %v, want 120", cfg.EmbeddingTimeout)
	}
	if !cfg.SanitizeToolOutput {
		t.Error("SanitizeToolOutput = false, want the default-on injection hardening")
	}
	if cfg.DataDir != "../data" || cfg.StateDir != ".state" {
		t.Errorf("DataDir/StateDir = %q/%q", cfg.DataDir, cfg.StateDir)
	}
}

func TestEntrypointIsAValidatedChoice(t *testing.T) {
	for _, entrypoint := range Entrypoints() {
		cfg := mustLoad(t, withEnv(map[string]string{EnvEntrypoint: string(entrypoint)}))
		if cfg.Entrypoint != entrypoint {
			t.Errorf("Entrypoint = %q, want %q", cfg.Entrypoint, entrypoint)
		}
	}

	message := loadError(t, withEnv(map[string]string{EnvEntrypoint: "unknown"}))
	assertContains(t, message, EnvEntrypoint, "agent, workflow, coordinator", `"unknown"`)
}

func TestModelProviderIsAValidatedChoice(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{EnvModelProvider: "anthropic"}))
	assertContains(t, message, EnvModelProvider, "openai-compatible, gemini", `"anthropic"`)
}

func TestLoadIgnoresALocalDotEnvFile(t *testing.T) {
	// Task runners inject the environment; the agent never reads a .env itself.
	// A file in the working directory must therefore change nothing.
	cleanEnvironment(t)
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("AGENT_MODEL=dotenv-must-not-load\n"), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Model != "qwen3:4b-instruct" {
		t.Errorf("Model = %q, want the committed default; a .env must not be loaded", cfg.Model)
	}
}

func TestRemovedGatewayFlagFailsWithMigrationGuidance(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv(EnvGatewayEnabled, "true")

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded with the removed AGENT_GATEWAY_ENABLED set")
	}
	assertContains(t, err.Error(),
		"AGENT_GATEWAY_ENABLED was removed",
		"AGENT_MODEL_PROVIDER=openai-compatible",
		"OPENAI_BASE_URL")
}

func TestRemovedGatewayFlagRejectsEveryValue(t *testing.T) {
	// The variable is not an alias of a live field: *any* value means the
	// operator is following stale instructions, so "false" and "" must fail too.
	for _, value := range []string{"true", "false", "1", "0", ""} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			message := loadError(t, withEnv(map[string]string{EnvGatewayEnabled: value}))
			assertContains(t, message, "AGENT_GATEWAY_ENABLED was removed")
		})
	}
}

func TestOpenAICompatibleProviderRequiresBaseURL(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{
		EnvModelProvider: string(ProviderOpenAICompatible),
		EnvOpenAIBaseURL: "",
	}))
	assertContains(t, message, "requires OPENAI_BASE_URL", "direct Ollama", "host agentgateway")
}

func TestOpenAICompatibleProviderRequiresAPIKey(t *testing.T) {
	for name, key := range map[string]string{"empty": "", "blank": "   "} {
		t.Run(name, func(t *testing.T) {
			message := loadError(t, withEnv(map[string]string{
				EnvModelProvider: string(ProviderOpenAICompatible),
				EnvOpenAIBaseURL: "http://127.0.0.1:4000/v1",
				EnvOpenAIAPIKey:  key,
			}))
			assertContains(t, message, "requires OPENAI_API_KEY", "local-ollama")
		})
	}
}

func TestValidOpenAICompatibleGatewayCombination(t *testing.T) {
	cfg := mustLoad(t, withEnv(map[string]string{
		EnvModelProvider: string(ProviderOpenAICompatible),
		EnvOpenAIBaseURL: "http://127.0.0.1:4000/v1",
		EnvOpenAIAPIKey:  "local-agentgateway",
	}))
	if cfg.OpenAIBaseURL != "http://127.0.0.1:4000/v1" {
		t.Errorf("OpenAIBaseURL = %q", cfg.OpenAIBaseURL)
	}
}

func TestModelTemperatureIsOptionalAndBounded(t *testing.T) {
	cfg := mustLoad(t, withEnv(map[string]string{EnvModelTemperature: "0"}))
	if cfg.ModelTemperature == nil || *cfg.ModelTemperature != 0 {
		t.Fatalf("ModelTemperature = %v, want an explicit 0 (greedy sampling)", cfg.ModelTemperature)
	}

	message := loadError(t, withEnv(map[string]string{EnvModelTemperature: "2.1"}))
	assertContains(t, message, EnvModelTemperature, "between 0 and 2")
}

func TestMCPURLMustBeHTTP(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{EnvMCPURL: "ftp://example.invalid/mcp"}))
	assertContains(t, message, "AGENT_MCP_URL must be an http", "ftp://example.invalid/mcp")

	cfg := mustLoad(t, withEnv(map[string]string{EnvMCPURL: "http://127.0.0.1:3000/mcp"}))
	if cfg.MCPURL != "http://127.0.0.1:3000/mcp" {
		t.Errorf("MCPURL = %q", cfg.MCPURL)
	}
}

func TestPIIAnalyzerURLMustBeHTTP(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{EnvPIIAnalyzerURL: "127.0.0.1:3000"}))
	assertContains(t, message, "AGENT_PII_ANALYZER_URL must be an http", "127.0.0.1:3000")

	cfg := mustLoad(t, withEnv(map[string]string{EnvPIIAnalyzerURL: "http://presidio:3000"}))
	if cfg.PIIAnalyzerURL != "http://presidio:3000" {
		t.Errorf("PIIAnalyzerURL = %q", cfg.PIIAnalyzerURL)
	}
}

func TestPromptURIMustBeARegistryURI(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{EnvPromptURI: "agentops-agent-instruction/2"}))
	assertContains(t, message, "AGENT_PROMPT_URI", "prompts:/agentops-agent-instruction/2")

	cfg := mustLoad(t, withEnv(map[string]string{EnvPromptURI: "prompts:/agentops-agent-instruction/2"}))
	if cfg.PromptURI != "prompts:/agentops-agent-instruction/2" {
		t.Errorf("PromptURI = %q", cfg.PromptURI)
	}
}

func TestModelFallbackMustDifferFromThePrimaryModel(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{
		EnvModel:         "qwen3:4b-instruct",
		EnvModelFallback: "qwen3:4b-instruct",
	}))
	assertContains(t, message, "AGENT_MODEL_FALLBACK must differ from AGENT_MODEL", "qwen3:4b-instruct")

	cfg := mustLoad(t, withEnv(map[string]string{EnvModelFallback: "qwen3:1.7b"}))
	if cfg.ModelFallback == nil || *cfg.ModelFallback != "qwen3:1.7b" {
		t.Errorf("ModelFallback = %v", cfg.ModelFallback)
	}
}

func TestA2ABindAndAdvertisedHostsAreDistinct(t *testing.T) {
	cfg := mustLoad(t, withEnv(map[string]string{
		// Kubernetes explicitly opts into a container-wide bind; the advertised
		// host stays a name a caller can dial.
		EnvA2ABindHost: "0.0.0.0",
		EnvA2AHost:     "agentops-agent.localhost",
	}))
	if cfg.A2ABindHost != "0.0.0.0" || cfg.A2AHost != "agentops-agent.localhost" {
		t.Errorf("bind/advertise = %q/%q", cfg.A2ABindHost, cfg.A2AHost)
	}

	message := loadError(t, withEnv(map[string]string{EnvA2ABindHost: ""}))
	assertContains(t, message, EnvA2ABindHost)
}

func TestGeminiAPIKeyProviderDoesNotRequireOpenAIConfiguration(t *testing.T) {
	// The OpenAI checks are provider-gated: selecting Gemini must not demand
	// credentials for an adapter that is not in use.
	cfg := mustLoad(t, withEnv(map[string]string{
		EnvModelProvider: string(ProviderGemini),
		EnvGoogleAPIKey:  "gemini-secret",
		EnvOpenAIBaseURL: "",
		EnvOpenAIAPIKey:  "",
	}))
	if cfg.ModelProvider != ProviderGemini {
		t.Errorf("ModelProvider = %q", cfg.ModelProvider)
	}
	if cfg.GoogleAPIKey.Reveal() != "gemini-secret" {
		t.Errorf("GoogleAPIKey = %q", cfg.GoogleAPIKey.Reveal())
	}
}

func TestGeminiProviderRequiresAnExplicitAuthPath(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{EnvModelProvider: string(ProviderGemini)}))
	assertContains(t, message, "requires either GOOGLE_API_KEY",
		"GOOGLE_GENAI_USE_ENTERPRISE=true", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION")
}

func TestGeminiEnterpriseProviderAcceptsTheADCCoursePath(t *testing.T) {
	cfg := mustLoad(t, withEnv(map[string]string{
		EnvModelProvider:            string(ProviderGemini),
		EnvGoogleGenAIUseEnterprise: "true",
		EnvGoogleCloudProject:       "agentops-open-course",
		EnvGoogleCloudLocation:      "global",
	}))
	if !cfg.GoogleGenAIUseEnterprise {
		t.Error("GoogleGenAIUseEnterprise = false, want true")
	}
	if cfg.GoogleCloudProject != "agentops-open-course" || cfg.GoogleCloudLocation != "global" {
		t.Errorf("project/location = %q/%q", cfg.GoogleCloudProject, cfg.GoogleCloudLocation)
	}
}

func TestGeminiEnterpriseProviderRequiresProjectAndLocation(t *testing.T) {
	tests := map[string]struct {
		project  string
		location string
		want     []string
	}{
		"missing project":  {project: "", location: "global", want: []string{EnvGoogleCloudProject}},
		"missing location": {project: "agentops-open-course", location: "", want: []string{EnvGoogleCloudLocation}},
		"blank project":    {project: "   ", location: "global", want: []string{EnvGoogleCloudProject}},
		"missing both": {
			project:  "",
			location: "",
			want:     []string{EnvGoogleCloudProject + " and " + EnvGoogleCloudLocation},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			message := loadError(t, withEnv(map[string]string{
				EnvModelProvider:            string(ProviderGemini),
				EnvGoogleGenAIUseEnterprise: "true",
				EnvGoogleCloudProject:       test.project,
				EnvGoogleCloudLocation:      test.location,
			}))
			assertContains(t, message, test.want...)
		})
	}
}

func TestGeminiProviderRejectsAmbiguousAPIKeyAndEnterpriseAuth(t *testing.T) {
	message := loadError(t, withEnv(map[string]string{
		EnvModelProvider:            string(ProviderGemini),
		EnvGoogleAPIKey:             "gemini-secret",
		EnvGoogleGenAIUseEnterprise: "true",
		EnvGoogleCloudProject:       "agentops-open-course",
		EnvGoogleCloudLocation:      "global",
	}))
	assertContains(t, message, "cannot combine GOOGLE_API_KEY", "GOOGLE_GENAI_USE_ENTERPRISE=true")
}

func TestProviderEnvironmentVariablesAreReadFromTheProcess(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv(EnvModelProvider, string(ProviderOpenAICompatible))
	t.Setenv(EnvOpenAIBaseURL, "http://127.0.0.1:4000/v1")
	t.Setenv(EnvOpenAIAPIKey, "super-sensitive")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ModelProvider != ProviderOpenAICompatible {
		t.Errorf("ModelProvider = %q", cfg.ModelProvider)
	}
	if cfg.OpenAIAPIKey.Reveal() != "super-sensitive" {
		t.Errorf("OpenAIAPIKey did not round-trip through the environment")
	}
	assertSecretNeverRendered(t, cfg, "super-sensitive")
}

func TestGeminiEnvironmentVariablesAreReadFromTheProcess(t *testing.T) {
	cleanEnvironment(t)
	t.Setenv(EnvModelProvider, string(ProviderGemini))
	t.Setenv(EnvGoogleAPIKey, "gemini-sensitive")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ModelProvider != ProviderGemini {
		t.Errorf("ModelProvider = %q", cfg.ModelProvider)
	}
	assertSecretNeverRendered(t, cfg, "gemini-sensitive")
}

// assertSecretNeverRendered proves the Secret type, not caller discipline, is
// what keeps a credential out of logs and errors.
func assertSecretNeverRendered(t *testing.T, cfg Config, plaintext string) {
	t.Helper()
	openAIText, err := cfg.OpenAIAPIKey.MarshalText()
	if err != nil {
		t.Fatalf("marshaling the OpenAI key: %v", err)
	}
	googleText, err := cfg.GoogleAPIKey.MarshalText()
	if err != nil {
		t.Fatalf("marshaling the Google key: %v", err)
	}
	renderings := map[string]string{
		"%v on the struct":       fmt.Sprintf("%v", cfg),
		"%+v on the struct":      fmt.Sprintf("%+v", cfg),
		"%q on the OpenAI key":   fmt.Sprintf("%q", cfg.OpenAIAPIKey),
		"%q on the Google key":   fmt.Sprintf("%q", cfg.GoogleAPIKey),
		"String on the keys":     cfg.OpenAIAPIKey.String() + cfg.GoogleAPIKey.String(),
		"MarshalText (JSON etc)": string(openAIText) + string(googleText),
	}
	for rendering, rendered := range renderings {
		if strings.Contains(rendered, plaintext) {
			t.Errorf("%s leaked the secret: %s", rendering, rendered)
		}
	}
	for _, setting := range cfg.Describe() {
		if strings.Contains(setting.Value, plaintext) {
			t.Errorf("Describe leaked the secret in %s", setting.Variable)
		}
	}
}

func TestFieldBoundsMatrix(t *testing.T) {
	// One row per bound the Python track expresses as a pydantic Field
	// constraint. `ok` values must load; `bad` values must be rejected naming
	// their own variable.
	tests := map[string]struct {
		variable string
		ok       []string
		bad      []string
	}{
		"model":                   {EnvModel, []string{"qwen3:1.7b"}, []string{""}},
		"a2a port":                {EnvA2APort, []string{"1", "8080", "65535"}, []string{"0", "65536", "-1", "http"}},
		"a2a protocol":            {EnvA2AProtocol, []string{"http", "https"}, []string{"ftp", "HTTP", ""}},
		"a2a max llm calls":       {EnvA2AMaxLLMCalls, []string{"1", "12", "100"}, []string{"0", "101"}},
		"drain timeout":           {EnvDrainTimeout, []string{"0.5", "10", "300"}, []string{"0", "300.1", "-1"}},
		"model timeout":           {EnvModelTimeout, []string{"0.1", "60", "600"}, []string{"0", "601"}},
		"tool timeout":            {EnvToolTimeout, []string{"30", "600"}, []string{"0", "601"}},
		"max retries":             {EnvMaxRetries, []string{"0", "2", "10"}, []string{"-1", "11"}},
		"retry backoff":           {EnvRetryBackoff, []string{"0.5", "30"}, []string{"0", "30.1"}},
		"circuit threshold":       {EnvCircuitFailureThreshold, []string{"1", "5", "100"}, []string{"0", "101"}},
		"circuit reset":           {EnvCircuitResetTimeout, []string{"30", "600"}, []string{"0", "601"}},
		"embedding timeout":       {EnvEmbeddingTimeout, []string{"120", "600"}, []string{"0", "601"}},
		"embeddings url":          {EnvEmbeddingsURL, []string{"http://ollama:11434"}, []string{""}},
		"embedding model":         {EnvEmbeddingModel, []string{"nomic-embed-text"}, []string{""}},
		"max history messages":    {EnvMaxHistoryMessages, []string{"2", "40"}, []string{"1", "0", "-1"}},
		"max tokens per session":  {EnvMaxTokensPerSession, []string{"1", "50000"}, []string{"0", "-1"}},
		"input price":             {EnvInputPricePer1K, []string{"0", "0.15"}, []string{"-0.1"}},
		"output price":            {EnvOutputPricePer1K, []string{"0", "0.6"}, []string{"-0.1"}},
		"trusted identity header": {EnvTrustedIdentityHeader, []string{"x-verified-subject"}, []string{""}},
		"model fallback":          {EnvModelFallback, []string{"qwen3:1.7b"}, []string{""}},
		"a2a bind host":           {EnvA2ABindHost, []string{"0.0.0.0"}, []string{""}},
		"a2a advertised host":     {EnvA2AHost, []string{"agentops-agent.localhost"}, []string{""}},
		"a2a streaming":           {EnvA2AStreaming, []string{"true", "false"}, []string{"yes", ""}},
		"circuit breaker enabled": {EnvCircuitBreakerEnabled, []string{"true", "false"}, []string{"on", ""}},
		"model temperature":       {EnvModelTemperature, []string{"0", "1", "2"}, []string{"-0.1", "2.1", "warm"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			for _, value := range test.ok {
				mustLoad(t, withEnv(map[string]string{test.variable: value}))
			}
			for _, value := range test.bad {
				message := loadError(t, withEnv(map[string]string{test.variable: value}))
				assertContains(t, message, test.variable)
			}
		})
	}
}

func TestFieldProblemsAccumulateSoEveryMistakeIsReportedAtOnce(t *testing.T) {
	// The whole point of the accumulating shape: a learner with three bad values
	// fixes three values, not one per run.
	_, err := LoadFrom(withEnv(map[string]string{
		EnvA2APort:          "70000",
		EnvMaxRetries:       "99",
		EnvModelTemperature: "3",
	}))
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if len(invalid.Problems) != 3 {
		t.Fatalf("reported %d problems, want 3: %v", len(invalid.Problems), invalid.Problems)
	}
	assertContains(t, err.Error(), EnvA2APort, EnvMaxRetries, EnvModelTemperature)
}

func TestProviderProblemsAccumulateAndShortCircuitLaterPhases(t *testing.T) {
	// Both provider credentials are missing *and* the MCP URL is wrong. The
	// provider phase is reported whole and on its own, exactly as the Python
	// validator raises its provider list before it looks at anything else.
	_, err := LoadFrom(withEnv(map[string]string{
		EnvOpenAIBaseURL: "",
		EnvOpenAIAPIKey:  "",
		EnvMCPURL:        "ftp://example.invalid/mcp",
	}))
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if len(invalid.Problems) != 2 {
		t.Fatalf("reported %d problems, want the 2 provider problems: %v", len(invalid.Problems), invalid.Problems)
	}
	assertContains(t, err.Error(), "requires OPENAI_BASE_URL", "requires OPENAI_API_KEY")
	if strings.Contains(err.Error(), "AGENT_MCP_URL") {
		t.Error("provider problems must short-circuit the cross-field phase")
	}
}

func TestOneVariableIsReportedOnce(t *testing.T) {
	// A value that fails to parse also fails the bound check on the resulting
	// zero value; the operator must be told about one mistake once.
	_, err := LoadFrom(withEnv(map[string]string{EnvA2APort: "not-a-number"}))
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	if len(invalid.Problems) != 1 {
		t.Fatalf("reported %d problems, want 1: %v", len(invalid.Problems), invalid.Problems)
	}
	assertContains(t, invalid.Problems[0].String(), EnvA2APort, "not-a-number")
}

func TestExplicitlyEmptyVariablesAreNeverSilentDefaults(t *testing.T) {
	// caarlos0/env resolves an explicitly empty variable to its envDefault. Left
	// alone, `OPENAI_API_KEY=` in a .env would silently become "local-ollama"
	// and `AGENT_MODEL=` would become the shipped model. Every one of these must
	// be rejected instead.
	for _, variable := range []string{
		EnvEntrypoint, EnvModelProvider, EnvModel, EnvOpenAIBaseURL, EnvOpenAIAPIKey,
		EnvA2ABindHost, EnvA2APort, EnvDrainTimeout, EnvEmbeddingModel,
		EnvSanitizeToolOutput, EnvMaxHistoryMessages,
	} {
		t.Run(variable, func(t *testing.T) {
			message := loadError(t, withEnv(map[string]string{variable: ""}))
			assertContains(t, message, variable)
		})
	}

	// An optional credential is the exception: empty means "not configured",
	// which is exactly what unset means, and the Python track accepts it.
	for _, variable := range []string{EnvGoogleAPIKey, EnvMCPToken, EnvMCPURL, EnvPromptURI} {
		t.Run(variable+" tolerates empty", func(t *testing.T) {
			mustLoad(t, withEnv(map[string]string{variable: ""}))
		})
	}
}

func TestSecondsConvertToDurations(t *testing.T) {
	cfg := mustLoad(t, withEnv(map[string]string{
		EnvModelTimeout: "1.5",
		EnvRetryBackoff: "0.25",
	}))
	if got := cfg.ModelTimeout.Duration(); got != 1500*time.Millisecond {
		t.Errorf("ModelTimeout.Duration() = %v, want 1.5s", got)
	}
	if got := cfg.RetryBackoff.Duration(); got != 250*time.Millisecond {
		t.Errorf("RetryBackoff.Duration() = %v, want 250ms", got)
	}
}

func TestVariableConstantsMatchStructTags(t *testing.T) {
	// Go struct tags cannot reference constants, so this is what keeps the
	// exported Env* names and the `env:"..."` tags from drifting apart.
	fromTags := make([]string, 0, len(specs()))
	for _, spec := range specs() {
		fromTags = append(fromTags, spec.variable)
	}
	fromConstants := []string{
		EnvEntrypoint, EnvModelProvider, EnvModel, EnvOpenAIBaseURL, EnvOpenAIAPIKey,
		EnvGoogleAPIKey, EnvGoogleGenAIUseEnterprise, EnvGoogleCloudProject, EnvGoogleCloudLocation,
		EnvGatewayEnabled, EnvMCPURL, EnvMCPToken, EnvPromptURI, EnvDataDir, EnvStateDir,
		EnvA2ABindHost, EnvA2AHost, EnvA2APort, EnvA2AProtocol, EnvA2AMaxLLMCalls,
		EnvA2AStreaming, EnvDrainTimeout, EnvTrustedIdentityHeader, EnvModelTemperature,
		EnvModelTimeout, EnvToolTimeout, EnvMaxRetries, EnvRetryBackoff,
		EnvCircuitBreakerEnabled, EnvCircuitFailureThreshold, EnvCircuitResetTimeout,
		EnvModelFallback, EnvWritesDisabled, EnvSanitizeToolOutput, EnvPIIAnalyzerURL,
		EnvSemanticRetrieval, EnvEmbeddingsURL, EnvEmbeddingModel, EnvEmbeddingTimeout,
		EnvMaxHistoryMessages, EnvMaxTokensPerSession, EnvInputPricePer1K, EnvOutputPricePer1K,
	}
	slices.Sort(fromTags)
	slices.Sort(fromConstants)
	if !slices.Equal(fromTags, fromConstants) {
		t.Errorf("struct tags and Env* constants disagree:\ntags:      %v\nconstants: %v", fromTags, fromConstants)
	}
}

func TestEveryFieldCarriesAnEnvironmentTag(t *testing.T) {
	// A field without a tag is unreachable configuration: it can never be set,
	// never appears in config:check, and never reaches .env.example.
	configType := reflect.TypeFor[Config]()
	if len(specs()) != configType.NumField() {
		for i := range configType.NumField() {
			if _, ok := configType.Field(i).Tag.Lookup("env"); !ok {
				t.Errorf("Config.%s has no `env` tag", configType.Field(i).Name)
			}
		}
	}
}

func TestEnvExampleDocumentsEveryActiveVariable(t *testing.T) {
	// The Go module ships its own .env.example because its defaults (the data and
	// state directories) resolve against agents/go, not agents/python. Gate it so
	// a new setting cannot land undocumented.
	example, err := os.ReadFile(filepath.Join("..", ".env.example"))
	if err != nil {
		t.Fatalf("reading .env.example: %v", err)
	}
	documented := make(map[string]bool)
	// Commented-out lines count: they document a variable and its default
	// without turning it on.
	for _, match := range regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]+)=`).FindAllStringSubmatch(string(example), -1) {
		documented[match[1]] = true
	}
	for _, spec := range specs() {
		// The deprecated alias is deliberately absent: documenting a removed
		// variable would invite someone to set it.
		if spec.variable == EnvGatewayEnabled {
			if documented[spec.variable] {
				t.Errorf("%s is removed and must not appear in .env.example", spec.variable)
			}
			continue
		}
		if !documented[spec.variable] {
			t.Errorf("%s is not documented in .env.example", spec.variable)
		}
	}
}
