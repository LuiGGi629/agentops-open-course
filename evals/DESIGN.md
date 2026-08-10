# Evaluation design evidence

This note preserves the verified design facts from the abandoned Python scaffold that preceded the Go harness. The implementation was discarded; these facts remain the compatibility contract.

## Verified wire contract

- ADK Go 2.2.0 exposes the runtime routes `POST /apps/{app}/users/{user}/sessions`, `POST /run`, `POST /run_sse`, and `GET /list-apps`. Its eval routes are unimplemented.
- The REST event shape carries `content.parts`, `usageMetadata`, `partial`, `errorCode`, and `errorMessage`.
- The deployed A2A surface uses JSON-RPC `message/send`. Text, function calls, and function responses arrive in task artifacts or status messages. ADK metadata uses `adk_` prefixes, including `adk_type`, `adk_usage_metadata`, `adk_partial`, and `adk_error_code`.
- `adk_request_confirmation` is the confirmation wrapper emitted for a guarded tool. A confirmation reply is a function response with the original call id and a `{confirmed, payload: {rationale}}` response.

These facts were checked against the local source of `google.golang.org/adk/v2@v2.2.0`, notably `server/adkrest/internal/{models,routers}` and `server/adka2a/v2/{metadata,parts}.go`. They are covered by fixed-wire HTTP tests in this module so an upstream shape change fails offline.

## Folding contract

Both transports normalize captured events and call the same `FoldEvents` function. A scorer sees one `Turn` type containing final text, ordered tool calls, tool evidence, usage, provider failure, and an optional pending confirmation. Streaming partials are retained as raw evidence but excluded from text, tool, and token totals because the final event repeats their content.

Transport equivalence is a load-bearing test: equivalent REST and A2A captures must fold to an equivalent `Turn`. This is the mechanical reason one scorer can grade either deployment surface.

## Scoring contract

- Trajectory matching is binary, required-subset, and in order. Extra calls and optional actual arguments are allowed. An expected empty string accepts an omitted argument because the tool's default supplies it; a non-empty actual value does not satisfy that explicit empty-string expectation.
- Cost compares positive per-case `total_tokens` and `model_calls` against a reviewed, fully attributed baseline. Case-set or runtime-identity drift refuses comparison.
- Groundedness recognizes incident ids, severities, dataset service names, and dataset runbook slugs. Every entity in the answer must appear in that turn's question or tool-response evidence.
- Structured-report scoring validates the strict report object rather than accepting prose or a second JSON value.
- The gateway judge returns strict JSON `{passed, rationale}`. Governed runs use it only after its typed provider/name/digest clears the balanced 12-case calibration set at the repository policy's exact agreement floor.
- Approval, write authority, injection, PII, and refusal controls have distinct deterministic score names. The confirmation scorer accepts only ADK's exact confirmation-required placeholder plus its pending wrapper; a completed write response fails.

## Evidence boundary

An evaluation run is its own OpenTelemetry trace. Cases are child spans and scores are metrics. The run carries a typed source identity, revision, tree digest, dirty state, platform identity, model, and evalset instead of overloading one commit string. The exporter is configured only through `EVAL_OTEL_EXPORTER_OTLP_ENDPOINT`; it never inherits the agent's OTLP destination.

Offline validation proves the parsers, scorers, process/client behavior, import boundary, assets, and in-memory OTel evidence. A fresh cost baseline, a real model verdict, and visibility in a live Tempo/Grafana stack require an explicitly run model-backed evaluation and are not implied by the offline suite.
