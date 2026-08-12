# AGENTS.md

Guidance for coding agents working in the AgentOps Open Course. Humans should start with [README.md](./README.md). This repository dogfoods the [AGENTS.md](https://agents.md/) convention taught in Chapter 1.

## Repository purpose

The course teaches the complete lifecycle of one Go AgentOps Agent with Google ADK, agentgateway, kagent, and OpenTelemetry into Tempo, Loki, Prometheus, Alertmanager, and Grafana. `main` is a completed executable reference learners inspect and extend, not a collection of illustrative snippets.

- `agents/go/` is the Go reference agent, offline tests, state commands, protocol servers, and distroless image.
- `agents/data/` is immutable seed input: SQLite, logs, runbooks, and runtime Agent Skills.
- `evals/` is a standalone black-box Go evaluation module. It must not require or import the agent module.
- `tools/` is a standalone Go module for repository conventions, accessibility, release evidence, and local support commands.
- `content/` contains 84 Hugo pages: 74 course pages, nine chapter indexes, and the landing page.
- `layouts/`, `assets/`, `data/nav.yaml`, and `hugo.toml` own the Hextra site build and explicit learning path.
- `skills/` contains portable Agent Skills distilled from the course, distinct from runtime skills under `agents/data/skills`.
- `clients/web/` is a minimal dependency-free A2A client.
- `load/` contains k6 load tests and documented latency budgets.
- `infra/agentgateway/{host,k3d,gke}/` contains data-plane profiles.
- `infra/k8s/base` and `infra/k8s/overlays/{local,gke}` contain shared deployment resources.
- `infra/kagent/` declares the BYO Agent, ModelConfig, and governed RemoteMCPServer.
- `infra/observability/` contains host and in-cluster OpenTelemetry backends.
- `infra/gcp/` is a plan-first OpenTofu module for the optional GKE laboratory.

The root `go.mod` exists only for the Hextra Hugo Module. Never add agent, evaluator, or repository-tool dependencies to it.

## Course invariants

- **Docs mirror source.** Critical excerpts use `{{< include >}}` over exact named `--8<-- [start:name]` and end regions. Missing files, missing or duplicate regions, and empty excerpts fail the Hugo build.
- **Seed and state stay separate.** `agents/data/incidents.db` is never mutated. Host writes go to `agents/go/.state`; Kubernetes writers share `agentops-agent-state`.
- **Only write-owning boundaries prepare state.** A2A startup and direct state commands may copy or migrate runtime state. Probes and read tools remain read-only.
- **Restore is crash-recoverable.** Stop every writer first. State restore holds a process lock, fsyncs a three-phase journal, and recovers interrupted transactions before schema preflight or publication. Never bypass it with file copies or delete unexplained `.restore-*` evidence.
- **Reads and writes have different authority.** Only the conversational entrypoint can move its six read/runbook tools from local calls to MCP through `AGENT_MCP_URL`. Workflow and coordinator specialists keep local tools. Guarded writes remain in process.
- **MCP cannot widen the surface.** The client filters the server catalog through `MCPReadToolNames`; an advertised extra tool never joins the model request.
- **Writes require confirmation and attribution.** `restart_service` and `resolve_incident` require ADK confirmation, valid targets, approver/session/invocation identity, and a bounded redacted rationale. Mutation and audit insert share one transaction.
- **Action replay is idempotent.** The same invocation, action, and target returns the original audit result without another mutation.
- **Policy is attached once.** One ADK plugin at the app boundary covers every agent, sub-agent, workflow node, and coordinator specialist.
- **Guard order is load-bearing.** Before-model order is budget, compaction, redaction. The first non-nil callback result short-circuits later guards. After-model usage accounting runs before response redaction.
- **PII has two layers.** In-process deterministic Go redaction is always on. agentgateway adds central builtin masking and a private Go webhook that asks local Ollama for person, location, and organization spans. The webhook validates exact byte spans and fails closed.
- **Layer 1 remains necessary.** A gateway cannot see direct model calls, local logs, saved notes, audit writes, or pre-gateway chapters. It also cannot replace checksum-backed validation and credential tripwires.
- **Skills and retrieved data have different trust.** Only the locally constructed concrete skill loader can mark repository-reviewed instruction as trusted. Tool name strings cannot grant that status. Secret and PII redaction still applies.
- **Audit is append-only, not immutable.** SQLite triggers block ordinary update and delete, and every row carries its schema version. An administrator with file or schema authority can still alter it.
- **Telemetry content stays private by default.** ADK and GenAI content-capture settings default to literal `false`. Redaction covers model and tool boundaries, but raw session ingestion happens earlier.
- **Collection and evaluation are separate.** Runtime OTLP flows through the collector. `evals` exports only when `EVAL_OTEL_EXPORTER_OTLP_ENDPOINT` is set explicitly and forces child-agent export off.
- **Evaluation is black-box.** ADK REST and A2A events fold into one typed turn. Streaming partials never contribute duplicate usage. Expected domain values come from immutable seed data.
- **Evaluation evidence is sanitized.** Release artifacts and OTel attributes exclude prompts, answers, references, tool payloads, rationales, URLs, credentials, and provider errors.
- **Build identity has one authority.** Linker-owned mode, version, source identity, revision, tree digest, timestamp, and dirty state feed CLI output, AgentCard version, OTel resources, OCI labels, and backup manifests. Runtime environment variables cannot relabel a binary.
- **Dirty work never claims `HEAD`.** Release-bearing source resolution rejects tracked or untracked changes. Development may use `unknown+dirty.<digest>`, with revision empty and the deterministic tree digest recorded separately.
- **Planning is bounded.** The root agent plans multi-step investigations and verifies approved actions. The workflow is plan, investigate, evidence review, recommend; never introduce an unbounded reflection loop.
- **Cost-efficient by default.** Prefer deterministic offline tests, fakes, and the smallest model that materially proves the boundary. Do not start clusters, collectors, model servers, paid APIs, or cloud resources for an offline claim.
- **Go coverage has a floor.** `mise run test` fails when any package in `agents/go` or `evals` drops below 80% line coverage; `scripts/check-coverage.sh` measures it per package, because a repository total hides exactly the package worth worrying about. `cmd/` packages are excluded by kind — they are `package main` wiring exercised through subprocess tests. `tools/` is maintainer scaffolding and sits outside the floor; its two largest packages are verified by execution rather than unit tests, and the same script reports its real numbers on demand.

## Open-source boundary

The required path uses Google ADK for Go, agentgateway, kagent, OpenTelemetry, Tempo, Loki, Prometheus, Alertmanager, Grafana, Ollama, the Apache-2.0 Qwen3 open-weight model, and repository code. It requires no account, mandatory SaaS, or usage fee.

Gemini, Vertex AI, GKE, GCS, Artifact Registry, and hosted repository services are optional proprietary substrates. Never call the optional cloud environment fully open source.

Local Qwen3 through Ollama is the stable default:

```text
AGENT_MODEL_PROVIDER=openai-compatible
AGENT_MODEL=qwen3:4b-instruct
OPENAI_BASE_URL=http://127.0.0.1:11434/v1
OPENAI_API_KEY=local-ollama
```

Chapter 5 changes only `OPENAI_BASE_URL` to the agentgateway listener. Native Gemini and GKE/Vertex are comparisons.

The optional GKE path keeps its compatibility-pinned model until `mise run gke:smoke` passes both synthetic tool-result and stable-seed read-only A2A retrieval against a replacement gateway/model pair.

## Pinned contracts

Use locks and manifests as authority, never a number copied into prose:

- Agent dependencies: `agents/go/go.mod` and `agents/go/go.sum`.
- Evaluation dependencies: `evals/go.mod` and `evals/go.sum`.
- Repository-tool dependencies: `tools/go.mod` and `tools/go.sum`.
- Go and cross-repository CLI tools: root `mise.toml` and `mise.lock`.
- Agent build stage: `agents/go/Dockerfile`; its Go version must match every Go module and root mise.
- Hextra: root `go.mod` and `go.sum`; Hugo: root `mise.toml`.
- Self-hosted Mermaid and FlexSearch bundles: `assets/js/vendor/versions.json`.
- kagent charts: `infra/helmfile.yaml`; API resources use the pinned version declared there.
- Container images: digest-pinned at their use sites under `infra/` and the agent Dockerfile.
- Workflow-only Buildx: explicit version inputs in the release workflow.
- Evaluation inputs: three `evals/*.evalset.json` files and `judge-calibration.json`; the run thresholds are the `eval` task command line in `evals/mise.toml`.

Two transitive families are explicit ADK Go v2.2.0 compatibility ceilings, not stale pins:

- ADK's own module pairs `github.com/openai/openai-go/v3` v3.49.0 with `google.golang.org/genai` v1.66.0, and minimal version selection resolves both from ADK itself. ADK owns this pair; do not bump either client independently. (openai-go v3.50.0 does compile and pass against ADK v2.2.0 — the union-struct breakage that justified the older v3.8.1 floor is fixed upstream — but the pair still moves with ADK, not ahead of it.)
- ADK uses OpenTelemetry log `Value` and `KeyValue` APIs removed by the OTel 1.45 and log 0.21 family. OTel stable 1.44 with log 0.20 is the highest compiling family for this ADK release.

The validator for either ceiling is `cd agents/go && mise run check && mise run test`; it must compile ADK and pass the focused telemetry, command, and full race suite before the constraint or prose changes. A newer resolved module is not supported evidence.

Generated result files are transient handoffs. The organization caps artifact and log retention at **7 days**; durable release evidence belongs on an owner-approved immutable release and in OCI attestations.

## Evaluation evidence contract

`evals/mise.toml` is the contract. `mise run eval` writes `results.json`, `mise run eval:judge-calibration` writes `judge-calibration-results.json`, and `mise run eval:ab` writes `prompt-comparison.json`. Both are gitignored, so the evidence a page shows is the evidence a page carries. There is no `release-policy.json` and no automatic release qualifier: the thresholds live on the `eval` task's command line, which is what `content/4. Quality/4.4. Evaluations.md` teaches learners to read, and `checkEvalThresholds` pins the page to that command line.

The judge is scored but never decides a safety case. `evals/score.go` tags a judged verdict `Stochastic`, and `summarizeCases` folds `required_cases_passed` over deterministic scores only — a judged failure costs the run its `pass_rate` and cannot on its own declare a required case failed.

Stable OTel names are:

- Spans: `agentops.eval.run`, `agentops.eval.case`, `agentops.eval.score`.
- Metrics: `agentops.eval.score`, `agentops.eval.case.passed`, `agentops.eval.tokens`, `agentops.eval.model_calls`, `agentops.eval.run.passed`.

Do not change these names or JSON schemas without a compatibility decision and coordinated release-qualifier update.

## Stable network inventory

This file owns the stable network inventory. Repository convention checks map MCP `:3000`, A2A `:3001`, model `:4000`, gateway metrics `:15020`, gateway readiness `:15021`, raw MCP `:8000`, raw A2A `:8080`, kagent control plane `:8083`, web client `:8001`, ADK web `:8002`, docs `:8003`, Ollama `:11434`, Tempo `:3200`, OTLP `:4317` and `:4318`, collector metrics `:8889`, collector health `:13133`, Prometheus `:9090`, Alertmanager `:9093`, Grafana `:3002`, Loki `:3100`, and registry `:5050` to executable owners.

Adding a port requires updating this inventory, the convention checker contract, the executable owner, and `content/0. Overview/0.4. Ecosystem.md`.

## Hugo documentation build

Hugo Extended builds the site with Hextra as a Hugo Module. `mise run serve` previews on `:8003`; `mise run build:docs` writes `site/`.

| Concern                              | Location                                                |
| ------------------------------------ | ------------------------------------------------------- |
| Site configuration and source mounts | `hugo.toml`                                             |
| Learning path                        | `data/nav.yaml` and sidebar partial                     |
| Source include                       | `layouts/_shortcodes/include.html` and include partials |
| Admonitions and collapsibles         | shortcode layouts and custom CSS                        |
| Self-hosted search and diagrams      | `assets/js/vendor/`                                     |
| Search accessibility                 | `assets/js/search-a11y.js`                              |
| Search route index                   | `assets/json/search-data.json`                          |

Four non-default contracts are easy to break:

1. Strict mode combines Hugo `--panicOnWarning`, reference-link errors, and the navigation checker.
1. Every non-home page has an explicit lowercase kebab-case slug; Hugo combines reviewed section slugs and regular-page slugs through the permalink configuration.
1. Includes read through `assets/source/**` mounts so Hugo watches quoted code.
1. The title lives in front matter; never add a second Markdown H1.

This repository is a Hugo evaluation build and is not deployed. `baseURL` preserves canonical and social metadata for comparison, but no CNAME, Pages workflow, DNS, or publication claim ships here.

**Chapter numbering was reviewed on 11 August 2026 and deliberately left as it is.** Renumbering — `7.2b`/`7.3b` into the sequence, `8.7 Capstone` to `8.0`, `1.2` and `1.3` into the chapters they render inside — was free only while nothing is published, and it was considered on that basis. It was declined because the cost is the largest mechanical change available here (83 slug fields, every `relref`, `data/nav.yaml`, and every hard-coded `content/` path in `tools/`) against a benefit that is presentational, and because the numbers that look wrong are load-bearing: `1.2` and `1.3` are cross-chapter prerequisites the navigation states explicitly, and `8.7` first is the correct pedagogical order. Three narrower fixes landed instead: `1.2. Container Engine` and `0.5. Provider Options` were retitled so neither collides with a later page in search, and the 48 never-linked heading anchors were deleted so a copied section link cannot contradict the heading it lands on. Revisit only before the first published URL exists.

## Documentation page frame

`content/` holds 84 Markdown pages: 74 course pages, nine chapter `_index.md` files, and the landing `content/_index.md`. Every one of them follows this shape:

```markdown
---
title: "N.M. Title"
description: One sentence.
slug: "n-m-title"
---

{{% admonition abstract "In one glance" %}}

- **You will:** Outcome.
- **You need:** Checkable precondition.
- **Time:** about N minutes, kind. {{% /admonition %}}

## A declarative claim this section proves

Prose, a runnable command, and the output that command produced.

## Your turn: do the thing

Predict first: one question the learner answers before running anything.

- **Mode**: `inspect`, `temporary experiment`, `keep`, or `capstone carry-forward`.
- **Goal**: what the learner ends up able to do.
- **Files to touch**: exact paths, or `none`.
- **Preflight**: the command that proves the starting state.
- **Steps**: the ordered work.
- **Gate that proves completion**: the command whose output decides it.
- **Final state**: what stays on disk afterwards.

## What you can do now

- Observable capability.

Continue to [Full page name](link), which does the next thing.
```

Rules:

- **Headings state a claim; they do not ask a question.** An H2 names what its section proves, so headings can carry different weight from one another. One interrogative H2 per page is allowed, for the tension the page actually resolves. `0.7. Troubleshooting` and `0.8. Glossary` are the two exceptions, because their headings are the symptoms and terms a reader scans for.
- **Keep an anchor whenever a link depends on it.** Two mechanisms carry deep links, and both break silently. 29 H2s carry an explicit `{#kebab-slug}`, so rewording one of those headings without preserving its slug orphans every link that points at it. A further 108 inline `<a id="…"></a>` anchors open a paragraph or a list item, so deleting or merging that paragraph destroys the target even though no heading changed. Check both before rewriting a section, including the links inside `content/` itself.
- **Close on the page's kind.** Course pages end with `## What you can do now`, chapter indexes with `## What this chapter proved`, and the four pure lookup pages — `0.4. Ecosystem`, `0.6. Resources`, `0.7. Troubleshooting`, `0.8. Glossary` — with `## How to use this page later`. The landing page has no closer. Every other page's final paragraph names the next page, usually as a `Continue to [Full page name](link)` line.
- **An exercise declares all seven fields, in order.** A `## Your turn: …` section carries Mode, Goal, Files to touch, Preflight, Steps, Gate that proves completion, and Final state. A hands-on page states a prediction before its exercise, so the learner commits to an answer before the command supplies one.
- **Mode is a promise about the working tree.** `inspect` changes nothing. `temporary experiment` requires a target-specific dirty preflight (`git diff --quiet -- <paths>` or `test ! -e <path>`) and target-specific cleanup (`git restore -- <paths>` or `rm -- <path>`); never restore a whole directory, which would discard unrelated learner work. `keep` leaves the change in the diff.
- **A ```text block is a verbatim capture.** Paste what the command printed. Trimming must be declared in the surrounding prose ("trimmed to seven of the twenty-two package lines"); reordering, re-spacing, and hand-edited figures are never allowed. When a capture carries a count or a percentage, that figure must be derivable from a file in this repository, and `tools/internal/conventions` must derive it.
- **Comment density is a deliverable.** Quoted Go carries the rationale comments that explain why a non-obvious choice was made. Remove such a comment only when its rationale stops being true, never to shorten an excerpt.
- **A hands-on page reaches a runnable command within its first two H2 sections.**
- **Use zero to three `{{% collapsible note "Deeper: …" %}}` blocks per page.** Never collapse definitions, commands, expected output, security bounds, cost, or destructive actions.
- **Open each H2 with a concrete sentence of 25 words or fewer.** Keep sentences readable and cross-links sparse.
- **Every new or changed Mermaid diagram has adjacent `**Diagram in words:**` prose.**
- **Use descriptive full-page link labels and define unfamiliar terms at first use.**
- **Include shortcodes stand alone outside code fences and quote the smallest stable source region.**
- **Never add a front-matter `url` override.** Hugo gives it precedence, so it shadows the reviewed slug and permalink route. Home alone omits `slug`; chapter sections and regular pages require one.
- **Distinguish offline, live model, container, Kubernetes, cloud, destructive, and paid commands** before asking a learner to run them.
- **Do not claim what the repository does not ship:** no dollar-denominated cost panel, no automatic live judge, no external paging integration, and no cryptographically immutable audit store.

## Development commands

Root task vocabulary:

```bash
mise run install
mise run install:platform
mise run install:maintainer
mise run doctor
mise run doctor:model
mise run doctor:gateway
mise run doctor:platform
mise run doctor:gcp
mise run format
mise run check:core
mise run check
mise run test
mise run scan
mise run build:docs
mise run serve
```

Agent module vocabulary:

```bash
cd agents/go
mise run check
mise run test
mise run coverage
mise run build
mise run run
mise run workflow
mise run coordinator
mise run web
mise run a2a
mise run mcp
mise run mcp:http
mise run config:check
mise run data:reset
```

Evaluation module vocabulary:

```bash
cd evals
mise run eval:validate
mise run check
mise run test
mise run build
mise run eval
mise run eval:judge-calibration
mise run eval:ab -- --baseline "$BASELINE_ARTIFACT" --candidate "$CANDIDATE_ARTIFACT"
```

Set `BASELINE_ARTIFACT` and `CANDIDATE_ARTIFACT` to reviewed sanitized run files before `eval:ab`. `eval:validate` and artifact-only `eval:ab` are offline; `eval` and `eval:judge-calibration` call a configured generative model and stay outside offline test gates. Every other capability the harness exposes — the A2A transport, the workflow and triage-report evalsets, streaming, schema checks, and groundedness — is a flag on `mise run eval` rather than a task of its own, and `evals/README.md` documents each recipe.

## Local and cloud safety

Host quickstarts use the digest-pinned gateway wrapper. Published listeners bind to loopback. On native Linux, the wrapper owns a bridge-address-only relay so the gateway container can reach host MCP, A2A, and Ollama without exposing those upstreams.

Do not run host observability while in-cluster observability is forwarded on the same ports. No profile creates an Ingress, LoadBalancer, or public application endpoint; clients use temporary port-forwards.

Local Kubernetes starts only from `infra/k3d.yaml`. `mise run platform:dev` resolves the working tree through the source-identity tool; a dirty tree receives `unknown+dirty.<digest>` and no revision. `mise run platform:run` and release workflows require a clean exact revision. Raw Skaffold builds must supply the complete mode/identity/revision/tree/dirty/version/timestamp tuple, and the Dockerfile rejects missing, templated, or inconsistent release inputs.

The GKE path stops at `tofu plan` unless the user explicitly approves deployment. It bills real money. `skaffold delete`, PVC deletion, cluster deletion, `tofu apply`, and `tofu destroy` require exact-context review; cloud apply and destroy require explicit approval.

## Maintainer recipes

- **Add a Go dependency:** change only the owning module, run `go mod tidy`, review both manifest and checksum diff, then run that module's check and test gates.
- **Add a network port:** update the stable inventory, convention contract, executable owner, and ecosystem table.
- **Add a course page:** preserve the page frame, explicit slug, chapter index entry, navigation entry, accessibility prose, and closing contract.
- **Bump a coordinated pin:** update its authority, regenerate lock or digest evidence, search for compatibility copies, and run every affected profile.
- **Change evaluation evidence:** coordinate harness schema, serialization tests, documentation, release qualifier, and workflow consumer in one change.
- **Change state schema:** add forward migration, unknown-future rejection, backup/restore evidence, and rollback notes before changing prose.

Release evidence is commit-scoped. Freeze the candidate, dispatch evaluation and platform evidence at that exact SHA, then dispatch release with the same SHA and fresh handoffs. Any push creates a new candidate.

## Definition of done

Re-read the request, inspect the scoped diff, and run:

```bash
mise run install:maintainer
mise run format
mise run check
mise run test
mise run scan
```

Also run `mise run check && mise run test` inside each changed Go module.

The complete offline gate must not call a model, collector, cluster, paid API, or cloud service. Local green evidence does not prove hosted CI, deployed runtime, immutable release, or public publication. Never suppress a real failure, weaken a scorer, invent a coverage threshold, or claim an external boundary you did not observe.
