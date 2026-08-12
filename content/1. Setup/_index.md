---
title: "1. Setup"
description: Take a clean clone to a green offline suite and a local model, installing each heavier dependency only on the page that actually needs it.
slug: "1-setup"
---

{{% admonition abstract "In one glance" %}}

- **You will:** See how this chapter is staged, run the one command that reads your machine, and know which pages you can skip today.
- **You need:** Git, a Unix-like shell, and permission to install tools into your own user account.
- **Time:** about 6 minutes, orientation. {{% /admonition %}}

## Nothing here calls a model, and that is the plan

Most agent tutorials open by asking for an API key. This one opens by asking for nothing, because the interesting part of the next four pages is how much of an agent you can prove correct before a model is anywhere near it.

So the chapter is staged, and each stage buys something specific. The offline stage costs a download and gives you a compiler, a linter, and a test suite that decide most of the agent's behavior. The model stage costs 2.5 GB of weights and gives you an agent that answers questions on your own hardware. The container, Kubernetes, and cloud stages cost progressively more and buy nothing you need before Chapter 5, which is exactly why they are not here.

You can see where you stand in about a second:

```bash
mise run doctor
```

```text
[doctor] $ ./scripts/doctor.sh base
base       ready
env        optional .env is absent
```

Two lines of verdict under the command mise echoes before running it, and neither is a blessing. The first says the pinned tools this repository shells out to are all on your `PATH`. The second says no `.env` file exists, which is not a warning: the required path needs no credentials at all, and the deterministic checks deliberately never read that file even when it does exist.

**By the end of [1.4. Providers]({{< relref "/1. Setup/1.4. Providers.md" >}}) a language model will be answering on your own hardware — and by the end of [1.1. Go]({{< relref "/1. Setup/1.1. Go.md" >}}) you will have watched a 1,815-test suite pass without one.**

## Four required pages, two you should skip today

Read the four in order; together they take about ninety minutes, most of it waiting for downloads. The other two are prerequisites for later chapters, and the navigation brings you back to each of them at the moment it becomes load-bearing — budget another half hour when it does.

- **[1.0. System]({{< relref "/1. Setup/1.0. System.md" >}})** _(hands-on)_: install the pinned toolchain and take a clean clone to a green offline suite.
- **[1.1. Go]({{< relref "/1. Setup/1.1. Go.md" >}})** _(hands-on)_: the three Go modules, the dependencies the agent actually ships, and the coverage floor.
- **[1.4. Providers]({{< relref "/1. Setup/1.4. Providers.md" >}})** _(hands-on)_: Ollama, the Qwen3 pull, the model doctor, and the deadline that trips first-hour learners.
- **[1.5. Workspace]({{< relref "/1. Setup/1.5. Workspace.md" >}})** _(hands-on)_: what is source, what is disposable, and which check runs at which moment.

Two more sit in this chapter but belong to later ones. **[1.2. Container Engine]({{< relref "/1. Setup/1.2. Container Engine.md" >}})** _(hands-on)_ prepares a Docker-compatible engine and is reached from [5.1. Gateway Setup]({{< relref "/5. Gateway/5.1. Gateway Setup.md" >}}); **[1.3. Kubernetes]({{< relref "/1. Setup/1.3. Kubernetes.md" >}})** _(reference)_ prepares the local platform tier and is reached from [6.1. Containers]({{< relref "/6. Platform/6.1. Containers.md" >}}). Open them early only if you enjoy installing things you will not use for several hours.

## Each tier has its own doctor, and none of them implies the next

The profiles are independent diagnostics, not a ladder you climb once. A green base profile says nothing about whether a model is serving, and a green model profile says nothing about whether Docker is running.

| What you are about to do                      | Profile that proves you are ready                 |
| --------------------------------------------- | ------------------------------------------------- |
| Chapters 0 and 1, plus every offline check    | `mise run doctor`                                 |
| Any model-backed page in Chapters 2 through 4 | `mise run doctor:model`                           |
| The Chapter 5 host gateway                    | `mise run doctor:gateway` plus the model profile  |
| The Chapter 6 local platform                  | `mise run doctor:platform`                        |
| The optional GKE laboratory                   | `mise run doctor:gcp` and explicit cloud approval |

Setup itself never starts a model, a gateway, a cluster, or a cloud resource. Every one of those has an owning page later that can also state what it costs and how to tear it down.

## What this chapter proved

The chapter is still ahead of you; this page has already settled four things about how it goes:

- `mise run doctor` reports the base profile ready on your machine, or names the tool that is missing and the command that installs it.
- You can say which of the six pages you need today and which two the navigation will bring you back to.
- You can name the profile that proves each later chapter's prerequisites before you open that chapter.
- You know the required path through this chapter asks you for no credential at all — the optional Gemini and cloud branches on [1.4. Providers]({{< relref "/1. Setup/1.4. Providers.md" >}}) are the only places one appears.

An hour from now the phrase "it works on my machine" will have a precise, checkable meaning here, because every tool that decides a verdict is pinned in one table and compared against your machine by one command.

Continue to [1.0. System]({{< relref "/1. Setup/1.0. System.md" >}}) and install the toolchain.
