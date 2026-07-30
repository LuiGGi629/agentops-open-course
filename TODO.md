# v1.0.0 readiness review

This checklist is the release gate for the first stable AgentOps Open Course. It covers the required account-free OSS path and every repository-owned surface. Live GCP deployment is intentionally excluded; the optional GCP path receives only static, render, validation, and documentation review here.

Treat every unchecked item as unverified, even when an earlier release passed a similar check. Complete each item against the final candidate commit and link durable evidence in its pull request, issue, workflow run, or release notes. A deferral needs an owner, a public issue, and an explicit v1 non-goal.

## Release contract

- [ ] Define the v1 learner outcomes, supported platforms, public APIs, deployment interfaces, and explicit non-goals.
- [ ] Freeze the configuration, environment-variable, port, state, audit, MCP, A2A, image, and Kubernetes contracts documented in `AGENTS.md`.
- [ ] Document the compatibility and deprecation policy for course pages, Python entrypoints, data schemas, protocols, manifests, and container images.
- [ ] Document the supported upgrade path from the latest `0.x` release to `v1.0.0`, including state backup and rollback.
- [ ] Convert every remaining item in this file into completed evidence or a public, owned, explicitly deferred issue.
- [ ] Confirm there are no release-blocking issues, pull requests, security advisories, or unreviewed dependency updates.

## Clean-room learner journeys

- [ ] Run the documented core quickstart from a fresh clone on each supported Linux, macOS, and WSL2 target, or narrow the support matrix publicly.
- [ ] Verify `mise run install`, `mise run doctor`, `mise run check:core`, and `mise run test` without private dotfiles, cached virtual environments, or cloud credentials.
- [ ] Run the first Chapter 2 interaction with the pinned Qwen3 model through Ollama and verify the documented incident answer.
- [ ] Run the conversational agent, bounded workflow, and coordinator paths and compare their documented behavior.
- [ ] Complete the Chapter 3 read tools, Agent Skills, MCP, memory, guarded action, workflow, and A2A exercises in order.
- [ ] Complete the real host agentgateway path with Qwen3, including MCP, A2A, model routing, authentication, metrics, failure handling, and cleanup.
- [ ] Exercise the web client with streaming, approval, reconnect, cancellation, error, and persistent-session paths.
- [ ] Complete the local k3d path on the shared `local` cluster: install, image build, deploy, inspect, exercise, observe, back up, restore, and tear down.
- [ ] Verify host Compose and in-cluster MLflow, OpenTelemetry, Prometheus, Grafana, and Loki separately without port collisions.
- [ ] Run the documented k6 smoke and load profiles and compare measured latency/error results with the committed budgets.
- [ ] Complete the capstone from a clean clone using a learner-owned domain and score it with the published rubric.
- [ ] Confirm every journey leaves no unintended host process, relay, container, port-forward, namespace resource, volume, or modified seed file.
- [ ] Record confusing, duplicated, slow, or fragile steps as defects and re-run the affected journey after each fix.

## Agent and data correctness

- [ ] Review the Python package boundary, CLI, wheel, container, ADK discovery, and all `AGENT_ENTRYPOINT` compositions as public interfaces.
- [ ] Review configuration parsing for explicit defaults, trusted types, actionable errors, secret handling, and provider/topology independence.
- [ ] Verify immutable seed data and writable state remain separated across host, MCP, A2A, backup, restore, and Kubernetes paths.
- [ ] Verify migrations are atomic, repeatable, concurrency-safe, backward-compatible, and covered by restore tests.
- [ ] Review all read tools for bounded inputs, deterministic errors, injection hardening, recursive redaction, and read-only behavior.
- [ ] Review `load_skill` as the sole trusted-instruction exception and prove that redaction still applies.
- [ ] Review guarded writes for validation, attributable confirmation, rationale, transactionality, audit evidence, replay idempotency, and kill-switch behavior.
- [ ] Verify the append-only audit claim matches the implemented SQLite and administrator threat boundaries everywhere it appears.
- [ ] Test concurrent sessions, duplicate invocations, restarts, cancellation, timeouts, stale reads, and partial failures.
- [ ] Review A2A task/session persistence, protocol payloads, streaming events, approval continuation, reconnect, and error mapping.
- [ ] Review MCP direct and gateway modes for identical tool contracts and least-privilege authority.
- [ ] Verify planning happens only for multi-step work, the workflow stays bounded, and approved actions receive a fresh-read verification.
- [ ] Review long-term memory and semantic retrieval for provenance, relevance, data leakage, poisoning, concurrency, and deterministic fallback.
- [ ] Verify token budgets, cost accounting, circuit breakers, generation transitions, and failure isolation under parallel requests.

## Tests, evaluations, and performance

- [ ] Map every public invariant and failure boundary to a deterministic test; remove tests that assert implementation trivia without protecting behavior.
- [ ] Maintain at least 95% branch coverage and inspect uncovered branches rather than treating the percentage alone as proof.
- [ ] Run the complete offline suite repeatedly to detect order dependence, leaked state, timing sensitivity, and flaky tests.
- [ ] Review fixtures and fakes against the real ADK, MCP, A2A, gateway, SQLite, and telemetry boundaries they represent.
- [ ] Run model-backed agent and workflow evaluations on the exact pinned Qwen3 model and record model digest, prompt, source revision, sampling, and serving context.
- [ ] Require every named safety and tool-use trajectory to pass; do not hide a failed contract behind an aggregate score.
- [ ] Review evaluation cases for groundedness, retrieval, planning, reflection, approvals, refusals, injection, PII, budget, resilience, and regression sensitivity.
- [ ] Verify MLflow receives the exact evaluated transcript and that cost and groundedness verdicts reuse it without an untracked second model call.
- [ ] Measure host and k3d cold start, steady CPU, peak memory, disk growth, request latency, throughput, and error rate on the documented minimum machine.
- [ ] Profile meaningful bottlenecks before changing concurrency, caches, context size, model size, replicas, or resource requests.
- [ ] Confirm timeouts, retry limits, backoff, queues, and load-test concurrency fail safely under saturation.
- [ ] Publish the v1 evaluation and performance baseline with reproducible commands and explicit environmental limits.

## Security, privacy, and supply chain

- [ ] Re-review the threat model across model input/output, tools, retrieved data, skills, A2A, MCP, gateway, telemetry, web client, state, and CI.
- [ ] Run deterministic adversarial regressions plus manual tests for prompt injection, tool manipulation, data exfiltration, PII, credentials, unsafe approvals, and denial of service.
- [ ] Verify content capture remains literal `false` by default and document the raw-session-ingestion boundary before callbacks.
- [ ] Review authentication, CORS, bind addresses, forwarded headers, network policies, service accounts, token mounts, and Workload Identity manifests.
- [ ] Confirm public auth/TLS, HA, backups, alerts, and immutability are never claimed beyond what the repository proves.
- [ ] Run repository, history, dependency, IaC, image, secret, vulnerability, and license scans with zero untriaged findings.
- [ ] Review every dependency, action, binary, chart, image, model, and installer for provenance, immutable pinning where appropriate, license compatibility, and maintenance health.
- [ ] Build both images as non-root, scan their final contents, inspect capabilities and writable paths, and minimize packages and layers.
- [ ] Verify the release workflow scans before push, publishes the scanned digest, produces SPDX SBOMs, signs and attests both images, and independently verifies them.
- [ ] Review GitHub Actions permissions, OIDC identities, artifact retention, cache trust, fork behavior, concurrency, and tag protections.
- [ ] Reconcile `SECURITY.md`, supported-version policy, private reporting, response ownership, and disclosure expectations with v1.

## OSS-first efficiency

- [ ] Prove the complete required learning path needs no account, mandatory SaaS, proprietary runtime, paid API, or cloud resource.
- [ ] Verify ADK, agentgateway, kagent, MLflow, OpenTelemetry, Prometheus, Grafana, Ollama, Qwen3, and repository code are described with accurate licenses and roles.
- [ ] Keep Gemini, Vertex AI, GKE, GCS, Artifact Registry, GitHub hosting, and other proprietary services clearly optional.
- [ ] Remove unused dependencies, extras, tools, images, services, feature flags, duplicate configuration, and dead documentation.
- [ ] Review whether every always-on local component is needed; preserve single replicas and measured resource bounds by default.
- [ ] Minimize model downloads, container sizes, build contexts, cache invalidation, startup work, telemetry volume, retained state, and load concurrency.
- [ ] Verify the required stack fits the published workstation baseline and document lighter alternatives for constrained machines.
- [ ] Confirm no LiteLLM, garak, mandatory hosted judge, or hidden cloud contract has re-entered the implementation or prose.
- [ ] Compare viable OSS replacements before accepting any new proprietary or operationally expensive dependency.

## Course and content

- [ ] Read every page in sequence as a learner and verify one coherent progression from orientation through capstone.
- [ ] Verify every page passes the FAQ frame, opening summary, early runnable-command, collapsible-depth, admonition, prose, and closing-proof conventions.
- [ ] Verify every command from the documented directory in a clean environment and update its expected output, duration, prerequisites, cost, and teardown.
- [ ] Confirm critical excerpts come from checked source snippets and that prose does not maintain a second pseudo-implementation.
- [ ] Review every factual, security, performance, cost, compatibility, and production-readiness claim against current source or an authoritative citation.
- [ ] Label offline tests, local model calls, hosted model calls, Kubernetes changes, destructive steps, and optional cloud changes before execution.
- [ ] Check all internal links, anchors, source links, external links, glossary references, image paths, and navigation order.
- [ ] Review terminology, naming, capitalization, port numbers, environment variables, versions, diagrams, examples, and incident outputs for consistency.
- [ ] Remove repetition and obsolete history while preserving the reason behind non-obvious design and safety decisions.
- [ ] Verify every unfamiliar term is defined at first use and dense pages link to the glossary without excessive inline navigation.
- [ ] Review time estimates and prerequisites using actual clean-room runs.
- [ ] Conduct a final editorial pass for clarity, grammar, authentic voice, learner motivation, and concise sentences.
- [ ] Verify the capstone contract is sufficient to distinguish adaptation from copying and produces evidence a reviewer can score.

## Accessibility and user experience

- [ ] Complete a documented WCAG-oriented audit of the rendered course and web client; triage every finding before v1.
- [ ] Test full keyboard operation, focus order and visibility, skip navigation, search, code copy, dialogs, streaming updates, and approvals.
- [ ] Test contrast, zoom, reflow, reduced motion, mobile layout, high-contrast mode, and color-independent meaning.
- [ ] Test representative pages and the web client with screen-reader/browser combinations from the support matrix.
- [ ] Verify adjacent prose communicates every diagram's actors, relationships, sequence, and conclusion without a renderer.
- [ ] Review headings, landmarks, link labels, table structure, form labels, status announcements, language, and error messages.
- [ ] Decide whether PDF/offline ebook output is a v1 deliverable or an explicit non-goal with an accessible fallback.
- [ ] Update `ACCESSIBILITY.md` with tested support, known limits, audit date, and a maintained reporting path.

## Platform and observability

- [ ] Render and diff the host, local k3d, and GKE gateway profiles against the documented stable network contract.
- [ ] Review the Kubernetes base and local overlay for probes, resources, security contexts, state sharing, disruption behavior, and least privilege.
- [ ] Verify the local overlay on the pinned k3d, Kubernetes, Helm, Skaffold, kagent, and agentgateway versions.
- [ ] Test fresh install, idempotent reinstall, rolling update, interrupted rollout, rollback, backup, restore, and scoped deletion.
- [ ] Verify no profile creates an unintended Ingress, LoadBalancer, public endpoint, cloud dependency, or cross-namespace authority.
- [ ] Review OpenTelemetry collection, redaction, cardinality, sampling, backpressure, failure isolation, and storage retention.
- [ ] Verify traces, metrics, logs, audit rows, request IDs, model usage, and tool events correlate without capturing private content by default.
- [ ] Verify dashboards and documented queries display implemented signals and do not imply unimplemented alerts, scorers, or cost metrics.
- [ ] Test collector, MLflow, Prometheus, Grafana, Loki, gateway, model, MCP, and A2A failure modes independently.
- [ ] Confirm teardown instructions distinguish stopped workloads, preserved data, destructive volume deletion, cluster ownership, and host cleanup.

## Optional GCP path without deployment

- [ ] Keep `project_id`, region, zone, names, principals, repositories, buckets, and rendered identities variable-driven; search for project-specific literals.
- [ ] Run OpenTofu formatting, initialization without backend, validation, linting, and a reviewed plan using non-secret test variables.
- [ ] Render the GKE bundle from module outputs and verify Workload Identity, Artifact Registry, GCS, Vertex, storage class, and image references.
- [ ] Review IAM roles, API enablement, Spot-node assumptions, quotas, cleanup behavior, and least-privilege boundaries statically.
- [ ] Recalculate the documented estimate from current official pricing and preserve the free-tier, interruption, non-HA, traffic, storage, and Vertex caveats.
- [ ] Verify the project remains an input variable throughout tasks, scripts, docs, examples, and generated manifests.
- [ ] Do not run `tofu apply`, deploy to GKE, call Vertex, create cloud resources, or perform live cloud acceptance as part of this v1 review.

## Documentation site and publication

- [ ] Build the Zensical site locally with warnings treated as defects and inspect representative desktop, mobile, code, table, diagram, and admonition pages.
- [ ] Run the anonymous publication gate for the public domain, navigation, assets, source links, edit links, redirects, custom 404, and cache behavior.
- [ ] Verify DNS, HTTPS, custom-domain, Pages environment, permissions, concurrency, and deployment workflow documentation.
- [ ] Validate sitemap, robots behavior, metadata, canonical URLs, social previews, favicon, search index, and discoverability.
- [ ] Compare the public site to the exact release commit and ensure no unpublished local state is required to reproduce it.
- [ ] Confirm the repository remains the authoritative verification surface when the hosted site is unavailable.

## Maintenance and community

- [ ] Synchronize `README.md`, `AGENTS.md`, component READMEs, course prose, task help, and actual repository behavior.
- [ ] Review `CONTRIBUTING.md`, `GOVERNANCE.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `ACCESSIBILITY.md`, licenses, and `CITATION.cff`.
- [ ] Verify issue forms, pull-request template, labels, discussions, support routes, and contribution setup are usable by a first-time contributor.
- [ ] Document ownership, review authority, release authority, security response, inactivity handling, succession, and path to maintainership.
- [ ] Review Dependabot coverage and the manual coordinated-pin/freshness process for tools, charts, manifests, actions, models, and documentation.
- [ ] Test the scheduled freshness workflow and ensure it creates actionable, non-duplicated maintenance work with an owner.
- [ ] Define the post-v1 release cadence, supported versions, patch policy, dependency response targets, and archival criteria.
- [ ] Audit repository settings, branch/tag protection, required checks, environments, Pages, packages, releases, topics, description, and homepage.
- [ ] Confirm clean clones and forks can run the local gates without maintainer-only secrets or permissions.

## Final v1 go/no-go

- [ ] Freeze a release candidate commit and perform all evidence collection on that exact SHA.
- [ ] From a clean checkout, run `mise run install:maintainer`, `mise run format`, `mise run check`, `mise run test`, `mise run scan`, and `mise run build`.
- [ ] Run `mise run smoke:host`, the state backup/restore drill, the complete local k3d journey, and the documented teardown on the exact SHA.
- [ ] Run the pinned Qwen3 agent/workflow evaluation suite and the approved load profiles on the exact SHA.
- [ ] Confirm `git diff --check`, the release metadata check, all local gates, and every required GitHub check are green with zero warnings or untriaged findings.
- [ ] Review the final diff, lockfiles, generated manifests, changelog, migration notes, known limits, licenses, SBOM policy, and release notes.
- [ ] Bump all version surfaces to `1.0.0`, date the changelog, create the annotated `v1.0.0` tag, and push only the reviewed commit.
- [ ] Verify both published image digests, vulnerability/license scans, SPDX SBOMs, keyless signatures, attestations, and independent verification.
- [ ] Install from the tag, pull images by digest anonymously, and repeat the shortest representative host and local Kubernetes acceptance paths.
- [ ] Publish the GitHub release and public course only after every required check and artifact gate is green.
- [ ] Record the final go/no-go decision, exact SHA, evidence links, remaining known limits, and rollback owner.
