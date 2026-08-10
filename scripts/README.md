# Repository automation

Automation sits behind the `mise run` vocabulary. Contributors normally call the task, not its implementation.

- `tools/` contains native Go authoring, browser, evidence, fixture-server, relay, and release commands.
- `scripts/` orchestrates repository gates and host setup.
- `infra/scripts/` orchestrates gateway, state, local-platform, and optional GCP boundaries.

## Repository tasks

| Owner                       | Task or caller                                         | What it does                                                              |
| --------------------------- | ------------------------------------------------------ | ------------------------------------------------------------------------- |
| `tools/bin/conventions`     | `check:docs`, `check:skills`, `check:release-metadata` | Page frames, includes, navigation, rendered HTML, skills, and metadata.   |
| `tools/bin/accessibility`   | `check:accessibility`                                  | Real-browser documentation and A2A web-client acceptance.                 |
| `tools/bin/course-evidence` | `course:evidence*`                                     | Creates and verifies a sanitized learner completion manifest.             |
| `tools/bin/release-*`       | Release workflow                                       | Qualifies evidence, freshness, attestations, and reversible indexes.      |
| `tools/bin/fake-model`      | `model:fake`, `smoke:host`                             | Deterministic OpenAI-compatible model fixture.                            |
| `tools/bin/loopback-relay`  | host gateway wrapper                                   | Bounded bridge-to-loopback relay for containerized agentgateway.          |
| `check-licenses.sh`         | `check:licenses*`                                      | Repository and Go dependency licences through Trivy.                      |
| `check-infra.sh`            | `check:infra`                                          | Renders and validates Kubernetes overlays and the OpenTofu module.        |
| `check-task-shell.sh`       | `check:shell`                                          | Parses every inline mise task body with the shell that will run it.       |
| `doctor.sh`                 | `doctor*`                                              | Verifies one prerequisite tier and names its installation remedy.         |
| `install-helm-diff.sh`      | `install:platform`                                     | Verifies the pinned plugin checkout and executable on every install.      |
| `cluster-start.sh`          | `cluster:start`                                        | Creates or resumes the local k3d cluster and registry.                    |
| `promote.sh`                | `promote`                                              | Runs source/eval gates, renders the overlay, and prints guarded commands. |
| `smoke-host.sh`             | `smoke:host`                                           | Proves the account-free host composition, then tears it down.             |

## Runtime orchestration

| Script                             | Task or caller        | What it does                                                                    |
| ---------------------------------- | --------------------- | ------------------------------------------------------------------------------- |
| `infra/scripts/gateway-host.sh`    | `gateway:host*`       | Runs digest-pinned agentgateway on loopback and its private bridge.             |
| `infra/scripts/gateway-tls.sh`     | `gateway:host:auth`   | Generates gitignored demo TLS material.                                         |
| `infra/scripts/gateway-jwt.sh`     | `gateway:host:auth`   | Generates gitignored demo JWT keys and tokens.                                  |
| `infra/scripts/backup-state.sh`    | `state:backup`        | Snapshots agent state through the Go binary.                                    |
| `infra/scripts/restore-state.sh`   | `state:restore`       | Restores a reviewed snapshot after writers stop.                                |
| `infra/scripts/backup-drill.sh`    | `state:drill`         | Proves a local throwaway backup restores end to end.                            |
| `infra/scripts/check-state.sh`     | `check:infra`         | Asserts the shared claim, `fsGroup`, and read-only mounts.                      |
| `infra/scripts/deploy-gke.sh`      | `gke:deploy`          | Verifies context, resolves cloud coordinates, and applies the GKE bundle.       |
| `infra/scripts/render-gke.sh`      | GKE checks and deploy | Resolves project-neutral placeholders from OpenTofu outputs.                    |
| `infra/scripts/smoke-gke-model.sh` | `gke:smoke`           | Proves exact GKE config, Vertex tool results, and read-only A2A seed retrieval. |
| `infra/scripts/secrets.sh`         | Chapter 6.5           | Manages SOPS and age ciphertext.                                                |

## Conventions

1. Shell owns orchestration; Go owns parsing, network services, and structured evidence.
1. Shell scripts source `lib.sh` and declare prerequisites with `require_cmd <tool> <tier>`.
1. A prerequisite tier must be a profile `doctor.sh` actually verifies.
1. Task names are the public interface. Implementation filenames may change without changing course commands.
1. Release jobs compile the same native validators from the exact qualified source before using them.
