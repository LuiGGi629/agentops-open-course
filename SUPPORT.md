# Support

Version 1.0 freezes the public learning and runtime contracts of the AgentOps Open Course. This policy says which surfaces are stable, where the complete path is verified, and how to upgrade or roll back.

## What does v1 support?

The supported v1 outcome is the account-free OSS path from a clean checkout to:

- the offline course, agent, security, and infrastructure gates;
- the conversational agent, bounded workflow, and coordinator on Qwen3 through Ollama;
- the six read tools, repository Agent Skills, MCP, guarded writes, memory, and A2A;
- the host agentgateway path and the local k3d platform;
- self-hosted MLflow, OpenTelemetry, Prometheus, Grafana, and Loki;
- state backup and restore, deterministic adversarial tests, model evaluation, and load testing.

The complete release gate is verified on Linux x86_64. CI uses Ubuntu 24.04; the maintainer gate also runs on Debian 12. A machine running the entire local model and Kubernetes path should have at least 14 GiB RAM and 15 GiB free disk. The core documentation and offline Python path needs substantially less.

macOS, Linux arm64, and WSL2 remain best-effort for v1. Their lock entries keep tool installation reproducible, but they are not release-gated through the full container, loopback-relay, and k3d journey. Report platform-specific defects, but do not infer full-platform support from a successful `mise run install`.

## Which interfaces are stable?

The following repository-owned contracts are stable throughout the 1.x series:

- the chapter order, full page names, and public course URLs;
- the `agent` Python distribution, its package-level `root_agent`, and the `agent`, `workflow`, and `coordinator` `AGENT_ENTRYPOINT` values;
- documented environment variables, defaults, validation rules, and the network ports listed in `AGENTS.md`;
- immutable seed data, writable-state separation, SQLite schema versions, append-only audit behavior, and backup snapshot format;
- the documented six read-only MCP tools, the guarded in-process write tools, and the A2A task, streaming, approval, reconnect, persistence, cancellation, and error behavior;
- the host, local k3d, and optional GKE configuration shapes; Kubernetes resource names; image names; and versioned GHCR tags.

Internal Python modules, test helpers, generated HTML, unversioned Git commits, and upstream implementation details are not public APIs. Upstream MCP, A2A, ADK, kagent, and agentgateway contracts remain pinned dependencies; this repository adapts incompatible upstream changes before changing its own documented surface.

## How are compatibility and deprecations handled?

Patch releases fix defects without intentionally breaking a stable contract. Minor releases may add backward-compatible pages, configuration, tools, schema fields, or manifests. A breaking course, application, state, protocol, or deployment change requires a new major version.

When practical, a deprecated 1.x interface remains functional for at least the next minor release. Its replacement and removal target appear in the changelog and at the use site. Security fixes may remove an unsafe interface sooner; the release notes then explain the risk and migration. Migrations must be atomic, repeatable, restore-tested, and reject unknown future schema versions.

Versioned container tags are immutable. Deploy by digest when exact bytes matter; the project does not publish a floating `latest` deployment contract.

## How do I upgrade from v0.2.0?

The v1 code can prepare current runtime state at the normal writable startup boundary. Seed data is never migrated.

1. Stop the agent, MCP, A2A, gateway, and platform processes that can write state.
1. On `v0.2.0`, run `mise run state:backup` and keep the completed snapshot outside the working tree.
1. Record the current Git tag and any deployed image digests.
1. Check out `v1.0.0`, then run `mise run install` and `mise run check:core`.
1. Start one writable agent or A2A process and run the shortest incident read before restoring normal traffic.
1. Run `mise run state:drill` or the Chapter 6 Kubernetes restore drill before treating the upgrade as accepted.

To roll back, stop writers, return to `v0.2.0` or its recorded image digests, and restore the pre-upgrade snapshot with `mise run state:restore -- <snapshot>`. Do not open a v1-migrated database with older code unless the release notes explicitly declare that downgrade safe.

## What is outside the v1 contract?

- Live GCP deployment, Vertex calls, cloud cost acceptance, public TLS/auth, high availability, disaster recovery, and production operations.
- Completing the capstone in a maintainer-chosen domain. The capstone is intentionally the learner's adaptation and is scored by its published rubric.
- Full-path release qualification on macOS, Linux arm64, or WSL2.
- Formal WCAG conformance certification or an exhaustive assistive-technology matrix.
- PDF and ebook publication. Repository Markdown is the accessible offline fallback.
- Compatibility for forks, local patches, mutable upstream tags, or unsupported dependency combinations.

## How long is a release supported?

The latest 1.x release and `main` receive security and correctness fixes. After a new major release, the previous major receives critical security fixes for 90 days. Routine dependency updates are reviewed weekly; critical exploitable dependency findings are triaged within three business days.

The maintainer targets small patch releases as fixes accumulate and a reviewed minor release when a backward-compatible capability is ready. If the project cannot be maintained safely for six months, the maintainer will announce archival, disable unsupported publication workflows, and seek a successor under `GOVERNANCE.md`.

Use [SECURITY.md](./SECURITY.md) for vulnerabilities, [ACCESSIBILITY.md](./ACCESSIBILITY.md) for accessibility barriers, and a public issue for other support requests.
