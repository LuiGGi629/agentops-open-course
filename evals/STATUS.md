# Phase A status

## Offline implementation

- Standalone Go module with no requirement or resolved import from `agents/go`.
- One typed `Turn` fold shared by verified ADK Go 2.1.0 REST and deployed A2A wire shapes.
- Streaming partials retained as raw events and excluded from folded text, tools, and usage.
- Deterministic trajectory, cost, groundedness, and strict report-schema scorers.
- Gateway JSON verdict judge plus balanced calibration parsing and sanitized result evidence.
- Evaluation-only OpenTelemetry run/case/score spans and score/outcome/usage metrics.
- Isolated host-process or candidate-container state and teardown per case, with child telemetry forced off.
- Scheduled evaluation selects the candidate container explicitly; engine or image failure cannot fall back to the host binary.
- Sanitized release artifacts, committed evalsets/baseline/calibration/dashboard, CLI, and offline tests.
- Typed source mode/identity/revision/tree/dirty/shallow evidence and a separate bounded platform identity across run, cost, calibration, retrieval, comparison, and OTel surfaces.
- Repository-owned `go-v1.0` release policy with mandatory safety/capability cases, deterministic injection and PII checks, policy-bound qualifier logic, and exact-source REST/A2A calibration trial tasks.

The design facts recovered from the discarded Python scaffold are preserved in `DESIGN.md` and pinned by fixed-wire and transport-equivalence tests. There are no Python files or Python project artifacts under `evals/`.

## Proof boundary

Offline checks can prove parsing, scoring, transport normalization, asset consistency, import separation, process isolation, evidence serialization, and in-memory telemetry. They do not call a model or start the repository observability stack.

The following Phase A exit evidence therefore remains model/runtime-backed:

- a green full `eval:*` campaign against the compiled Go agent;
- a reviewed Go-agent replacement for the inherited `cost_baseline.json`;
- reviewed repeated trial evidence sufficient to replace `calibration-required` with an approved minimum, repeat floor, and total token/model-call budgets;
- judge agreement from the configured gateway;
- exported trace/metrics visible in Tempo, Prometheus, and the Grafana comparison dashboard.

Do not use the offline suite, a green `eval:policy-trial`, or the inherited baseline to claim any of those outcomes. Canonical `eval` and `eval:a2a` fail closed while the policy is `calibration-required`, and the release qualifier independently rejects that status. Until one clean-source campaign is reviewed and the status is approved, the I-7 deletion gate is not met and `agents/python/` remains a frozen, non-learner behavioral reference.
