---
title: "1. Setup"
description: Install the pinned Go-first toolchain, prepare the local model path, and prove the deterministic workspace before building an agent.
slug: "1-setup"
---

{{% admonition abstract "In one glance" %}}

- **You will:** Prepare the Go toolchain, optional runtimes, providers, and workspace contracts used by later chapters.
- **You need:** Git, a Unix-like shell, and permission to install user-scoped tools.
- **Time:** about 2 hours for the complete chapter, hands-on. {{% /admonition %}}

## Which pages do you need now?

Take the shortest path that matches your next chapter.

- **1.0. System:** install mise and prove the model-free baseline.
- **1.1. Go:** inspect the three Go modules, their locks, and the agent's runtime dependencies.
- **1.2. Containers:** install a container engine only before the image or gateway labs.
- **1.3. Kubernetes:** install the local platform tools only before Chapter 6.
- **1.4. Providers:** prepare the account-free Ollama path, then compare optional hosted providers.
- **1.5. Workspace:** understand generated state, configuration validation, hooks, and gates.

## What will you set up in this chapter?

The base install resolves Go modules for the agent, standalone evaluation harness, and repository tools, plus Hugo and dprint for the course site.

```bash
mise run install
mise run doctor
```

The local model path is separate: Ollama serves `qwen3:4b-instruct` over an OpenAI-compatible endpoint. Container, Kubernetes, and cloud tooling remain opt-in.

## Why are the prerequisites staged instead of installed up front?

Each stage adds cost, state, or operating surface.

The deterministic Go and documentation gates need no model. Model exercises add Ollama. Gateway and image exercises add a container engine. Platform exercises add k3d and Kubernetes tools. The optional GKE lab adds a cloud account and billable resources.

Staging keeps each checkpoint honest: passing an early gate does not imply a later service exists.

## Which tier does each chapter actually require?

| Chapters                                   | Required profile                                  |
| ------------------------------------------ | ------------------------------------------------- |
| 0, 1, and deterministic parts of 2-4 and 8 | `mise run doctor`                                 |
| Model-backed parts of 2-4                  | `mise run doctor:model`                           |
| Chapter 5 host gateway                     | `mise run doctor:gateway` plus the model profile  |
| Chapter 6 local platform                   | `mise run doctor:platform`                        |
| Optional GKE path                          | `mise run doctor:gcp` and explicit cloud approval |

## What is deliberately not part of this chapter?

Setup does not call a live model, start a gateway, create a cluster, deploy Kubernetes, or mutate cloud resources.

Those actions belong to pages that can state the expected evidence and teardown. A provider credential is never required for the account-free path.

## What proves this chapter worked?

Run the deterministic checkpoint from the repository root:

```bash
mise run doctor
mise run config:check
mise run check:core
mise run test
```

The Go suites report measured coverage, but the repository enforces no percentage threshold because the owner has not selected one.

**You are done when:**

- The base doctor and typed configuration check exit zero.
- The core check and all three Go module test suites pass without a model or platform service.
- You know which later page authorizes each heavier dependency.

Continue to [2. Agents]({{< relref "/2. Agents/_index.md" >}}) when the deterministic setup is green.
