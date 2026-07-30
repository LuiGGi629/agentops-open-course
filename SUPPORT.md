# Support

This policy says which surfaces are stable, where the complete path is verified, and how to upgrade or roll back. The course is pre-1.0: the software contracts below are versioned and change deliberately, while the course prose is still being improved release by release.

## What does the course support?

The supported outcome is the account-free OSS path from a clean checkout to:

- the offline course, agent, security, and infrastructure gates;
- the conversational agent, bounded workflow, and coordinator on Qwen3 through Ollama;
- the six read tools, repository Agent Skills, MCP, guarded writes, memory, and A2A;
- the host agentgateway path and the local k3d platform;
- self-hosted MLflow, OpenTelemetry, Prometheus, Grafana, and Loki;
- state backup and restore, deterministic adversarial tests, model evaluation, and load testing.

The complete release gate is verified on Linux x86_64 with cgroup v2. CI uses Ubuntu 24.04; the maintainer gate also runs on Debian 12. Pinned Kubernetes 1.35 refuses cgroup v1, and `mise run doctor:platform` checks this before creating a cluster. A machine running the entire local model and Kubernetes path should have at least 14 GiB RAM and 15 GiB free disk. The core documentation and offline Python path needs substantially less.

macOS, Linux arm64, and WSL2 remain best-effort. Their lock entries keep tool installation reproducible, but they are not release-gated through the full container, loopback-relay, and k3d journey. Report platform-specific defects, but do not infer full-platform support from a successful `mise run install`.

## Which interfaces are stable?

Stability is split by payload, because a course page and a database schema fail differently.

**Software contracts** are versioned and change only as described below:

- the `agent` Python distribution, its package-level `root_agent`, and the `agent`, `workflow`, and `coordinator` `AGENT_ENTRYPOINT` values;
- documented environment variables, defaults, validation rules, and the network ports listed in `AGENTS.md`;
- immutable seed data, writable-state separation, SQLite schema versions, append-only audit behavior, and backup snapshot format;
- the documented six read-only MCP tools, the guarded in-process write tools, and the A2A task, streaming, approval, reconnect, persistence, cancellation, and error behavior;
- the host, local k3d, and optional GKE configuration shapes; Kubernetes resource names; image names; and versioned GHCR tags.

**Course prose** gets exactly one guarantee: published course URLs are never left to 404. Chapters may be reordered, pages renamed, split, merged, or rewritten in any release, and a moved page keeps a redirect from its previous URL. Pedagogy improves faster than schemas do, and freezing page names would only protect the wrong thing.

Internal Python modules, test helpers, generated HTML, unversioned Git commits, and upstream implementation details are not public APIs. Upstream MCP, A2A, ADK, kagent, and agentgateway contracts remain pinned dependencies; this repository adapts incompatible upstream changes before changing its own documented surface.

## How are compatibility and deprecations handled?

Patch releases fix defects without intentionally breaking a stable software contract. Minor releases may add backward-compatible configuration, tools, schema fields, or manifests, and may restructure course pages. While the project is pre-1.0, a breaking change to a software contract requires a minor release and a documented migration; after 1.0 it will require a major release.

When practical, a deprecated interface remains functional for at least the next minor release. Its replacement and removal target appear in the changelog and at the use site. Security fixes may remove an unsafe interface sooner; the release notes then explain the risk and migration. Migrations must be atomic, repeatable, restore-tested, and reject unknown future schema versions.

Versioned container tags are immutable. Deploy by digest when exact bytes matter; the project does not publish a floating `latest` deployment contract.

## How do I upgrade from v0.2.0?

The current code prepares runtime state at the normal writable startup boundary. Seed data is never migrated.

1. Stop the agent, MCP, A2A, gateway, and platform processes that can write state.
1. On `v0.2.0`, run `mise run state:backup` and keep the completed snapshot outside the working tree.
1. Record the current Git tag and any deployed image digests.
1. Check out `v0.3.0`, then run `mise run install` and `mise run check:core`.
1. Start one writable agent or A2A process and run the shortest incident read before restoring normal traffic.
1. Run `mise run state:drill` or the Chapter 6 Kubernetes restore drill before treating the upgrade as accepted.

To roll back, stop writers, return to `v0.2.0` or its recorded image digests, and restore the pre-upgrade snapshot with `mise run state:restore -- <snapshot>`. Do not open a migrated database with older code unless the release notes explicitly declare that downgrade safe.

## What is outside the contract?

- Live GCP deployment, Vertex calls, cloud cost acceptance, public TLS/auth, high availability, disaster recovery, and production operations.
- Completing the capstone in a maintainer-chosen domain. The capstone is intentionally the learner's adaptation and is scored by its published rubric.
- Full-path release qualification on macOS, Linux arm64, or WSL2.
- Formal WCAG conformance certification or an exhaustive assistive-technology matrix.
- PDF and ebook publication. Repository Markdown is the accessible offline fallback.
- Compatibility for forks, local patches, mutable upstream tags, or unsupported dependency combinations.

## How long is a release supported?

The latest release and `main` receive security and correctness fixes. Older releases do not. This file owns that window: `SECURITY.md` and `GOVERNANCE.md` link here rather than restating it.

Dependency updates and security triage are best effort from a single maintainer — typically within a week, faster for an exploitable finding with a published fix. The maintainer targets small patch releases as fixes accumulate and a reviewed minor release when a capability is ready. If the project cannot be maintained safely for six months, the maintainer will announce archival, disable unsupported publication workflows, and seek a successor under `GOVERNANCE.md`.

Use [SECURITY.md](./SECURITY.md) for vulnerabilities, [ACCESSIBILITY.md](./ACCESSIBILITY.md) for accessibility barriers, and a public issue for other support requests.
