# Changelog

All notable changes to the AgentOps Open Course are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Removed the redundant standalone Kustomize binary and render overlays through the pinned kubectl, avoiding an executable-format failure on GitHub's Ubuntu runner.

## [0.2.0] - 2026-07-30

### Added

- Added a bounded read-only `plan → investigate → evidence_review → recommend` workflow, a least-privilege coordinator path, and model-backed evaluation for the planning and reflection loop.
- Added explicit learner, platform, and maintainer install tiers plus stable aggregate CI and security-scan jobs that import-smoke every built image.
- Added explicit accessibility and single-maintainer governance contracts for diagrams, keyboard/contrast expectations, review authority, and the path to maintainership.
- Added a repository-wide `TODO.md` that defines the OSS, course, runtime, security, accessibility, maintenance, and release evidence required before v1.0.0.
- Added project-neutral GKE render and deployment helpers, a balanced persistent-disk storage class, and output-driven Workload Identity manifests for the optional GCP lab.

### Changed

- Reworked the learning path so setup stays read-only, the first model interaction happens in Chapter 2, Kubernetes deployment precedes inspection, and the capstone is the primary finish before optional project maintenance.
- Unified ADK discovery behind one validated `AGENT_ENTRYPOINT=agent|workflow|coordinator` package boundary while constructing only the selected composition.
- Made promotion a truthful offline preflight by default, with model-backed evidence required before it prints deploy and rollback commands.
- Bound scheduled model evidence to one provider, immutable prompt, model digest, evaluation contract, source revision, serving context, and sampling configuration while reusing the exact MLflow transcript for required cost and groundedness verdicts.
- Made skill loading and both guarded-action confirmation trajectories strict named Qwen gates, so an aggregate pass rate cannot hide a failed safety contract.
- Made the optional GCP module and GKE delivery path variable-driven, quota-aware, and cheaper by default while preserving explicit plan, verification, and teardown boundaries.

### Fixed

- Repaired the locked ADK 2.4 terminal entrypoint, which was shadowed by `agent.py`, and added real CLI, wheel, and container discovery coverage.
- Wrapped `adk eval` so metric failures cannot exit successfully; each trajectory case is strict and the measured local-model baseline uses an explicit aggregate case-pass floor.
- Strengthened host and load smoke tests to require successful A2A completion, made OpenTelemetry provider setup idempotent, preserved evidence across workflow nodes, and required fresh reads after approved actions.
- Made approved-write replays idempotent in SQLite, bounded model-controlled runbook retrieval, serialized per-session token accounting, and protected the optional circuit-breaker registry and generation-bound transitions across worker threads.

## [0.1.1] - 2026-07-24

### Changed

- Renamed the reference agent from "Ops Copilot" to "AgentOps Agent", aligning the application identity (`OTEL_SERVICE_NAME`, MLflow experiment and prompt registry, ADK `app_name`, MCP server, audit actor, gateway backend) with the `agentops-agent` name the container image, Kubernetes workload, and Python distribution already used.
- Promoted the default local model to `qwen3:4b-instruct` (Qwen3 4B Instruct 2507) for stronger tool calling at the same 2.5 GB footprint, and documented `gemma4:e4b` as an optional, heavier Apache-2.0 alternative.
- Workflow sub-agents now enforce the same per-session token budget as every other agent (`enforce_token_budget`/`record_token_usage`).

### Fixed

- Corrected the front matter on the Overview, Quality, and Observability chapter indexes, where an unquoted colon made the YAML invalid and published the page description as a heading. `scripts/check-docs.sh` now parses front matter instead of pattern-matching it.
- Made semantic runbook indexing atomic — a `BEGIN IMMEDIATE` rebuild — so two concurrent first-use turns can no longer race a parallel index drop/create.
- Long-term memory now surfaces the same `DataAccessError` boundary as the primary data layer instead of leaking a raw SQLite driver error.
- The prompt A/B evaluator reads a marked child-output line and re-raises the child's stderr on failure, instead of assuming its scores are the last line printed and swallowing the cause.
- `mise run config:check` now names the offending field on a validation error, and `eval:cost` reports an actionable message for a malformed `AGENT_COST_TOLERANCE` rather than an uncaught `ValueError`.
- Corrected the container build-stage count (three, not two), the `ObservabilityCollectorDown` runbook cross-reference, the Observability chapter description and incident loop, and several command-directory and cross-link notes across the course.
- Aligned the `agent-guardrails` skill's kill-switch variable name to `AGENT_WRITES_DISABLED`, converted the `agentops-course` skill's cross-links to GitHub-rendering Markdown, and clarified the source-path roots in the installable skills.

## [0.1.0] - 2026-07-16

### Added

- Source-synchronized course excerpts, staged prerequisite doctors, and a scored capstone for adapting the completed reference platform.
- A deterministic host smoke that proves the fake-model, MCP, A2A, CORS, readiness, host/container metrics, and cleanup contracts without a provider account.
- Machine-verifiable repository, Python dependency, and container-image license gates.
- A real streamed A2A approval round trip plus full-conversation MLflow scoring for exact write policy, response facts, terminal confirmation pauses, and isolated state.
- Initial AgentOps course structure, Python Ops Copilot, local dataset, documentation site, and infrastructure examples.
- Local Qwen3/Ollama and optional GKE/Vertex learning paths behind one agentgateway contract.
- Persistent A2A sessions, immutable seed data, disposable runtime state, and append-only action auditing.
- Self-hosted MLflow and OpenTelemetry observability for local and Kubernetes labs.
- Community health files, contribution templates, and end-to-end verification checkpoints.
- Release workflow publishing Trivy-scanned, cosign-signed, SBOM-attested images to GHCR on version tags, with in-workflow verification.
- Self-hosted Renovate dependency updates on a weekly schedule and a documented upgrade playbook for coordinated pins.

### Changed

- Local Qwen3/Ollama is now the default first model path; Gemini, Vertex AI, GKE, and hosted publication remain explicit optional integrations.
- Model-provider selection is independent from direct-versus-gateway topology, and live dotenv values are scoped away from offline gates.
- The Python runtime dependency set no longer installs the unused cloud-database extra.
- SQLite backups now publish atomically after complete integrity checks, and restore paths reject incomplete snapshots.
- Scheduled evaluation installs the exact checksum-verified Ollama release asset instead of a removed archive path.
- Required Helm plugin installation and both Dockerfile frontends now use immutable reviewed source/digest pins; helm-diff platform assets are checksum-verified.
- Release metadata and the pushed `v` tag must agree before any image build or publication.
- Course chapters distinguish open-source software from optional proprietary model and cloud substrates.
- Gateway, platform, and observability material tracks runnable repository resources.

### Security

- Guarded actions now fail closed without confirmed, attributable approval and a bounded rationale; persistence redacts PII/credentials and reads current context inside the write transaction.
- Host gateway tasks use a digest-pinned, non-root, loopback-published container with a bridge-only relay for loopback upstreams.
- Kubernetes denies direct A2A ingress except from agentgateway, mounts shared state read-only in read/backup workloads, and disables unused service-account tokens.
- OTLP log export uses one trace-correlated handler that redacts and bounds copied records without mutating local console logs.
- Untrusted tool-output sanitization is enabled by default.
- Release publishing now pushes and signs the exact local image that passed the pre-push scan instead of rebuilding it.

[unreleased]: https://github.com/MLOps-Courses/agentops-open-course/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/MLOps-Courses/agentops-open-course/releases/tag/v0.2.0
[0.1.1]: https://github.com/MLOps-Courses/agentops-open-course/releases/tag/v0.1.1
[0.1.0]: https://github.com/MLOps-Courses/agentops-open-course/releases/tag/v0.1.0
