---
title: "4. Quality"
description: "Make the agent correct and trustworthy: typing, linting, testing, metrics, evaluations, guardrails, and security."
slug: "4-quality"
---

{{% admonition abstract "In one glance" %}}

- **You will:** See how the chapter's seven quality layers fit together and which page owns each check.
- **You need:** Chapter 3 finished and `mise run test` passing.
- **Time:** about 5 minutes, orientation. {{% /admonition %}}

## How will you make the agent trustworthy?

Trust comes in layers. Each page below adds one, and each layer catches a class of failure the cheaper layers below it cannot.

Your agent now holds a conversation ([Chapter 2]({{< relref "/2. Agents/_index.md" >}})) and has bounded capabilities ([Chapter 3]({{< relref "/3. Capabilities/_index.md" >}})). This chapter makes it defensible.

The early checkpoints need no model, account, network, or bill. The full maintainer security gate may refresh advisory data. The marker on each line says what that page's own checkpoint needs; run `mise run doctor:model` before a model-backed one.

- **[4.0. Type Safety]({{< relref "/4. Quality/4.0. Type Safety.md" >}})** _(concept · offline)_: compiler-checked types, enums, and parsing untrusted input at the boundary.
- **[4.1. Linting]({{< relref "/4. Quality/4.1. Linting.md" >}})** _(hands-on · offline)_: Lint and format with golangci-lint and dprint.
- **[4.2. Testing]({{< relref "/4. Quality/4.2. Testing.md" >}})** _(hands-on · offline)_: Fast, offline unit tests with go test, against an isolated dataset copy.
- **[4.3. Metrics]({{< relref "/4. Quality/4.3. Metrics.md" >}})** _(reference · needs a model)_: A scorecard of deterministic gates, model-backed evidence, and observed operational indicators.
- **[4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md" >}})** _(offline validation; model-backed runs)_: black-box REST/A2A turns, deterministic scorers, sanitized artifacts, and optional calibrated judge evidence.
- **[4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md" >}})** _(hands-on · offline, except the last checkpoint step)_: Boundary redaction, stable errors, confirmation, transactions, and audit evidence.
- **[4.6. Security]({{< relref "/4. Quality/4.6. Security.md" >}})** _(hands-on · model-free; scans may use network)_: Threat modeling, offline adversarial regressions, identity, and supply-chain scanning.

Expect to write code, not just read. [4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md" >}}) proves deterministic and gateway PII layers, while [4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md" >}}) shows how to add a fixed black-box case.

## Where is gate versus evidence explained?

[4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md#what-can-you-validate-without-a-model" >}}) separates offline asset validation, model-backed evidence, and release qualification. This index only marks each page's prerequisites.

## What proves this chapter worked?

Two offline commands cover the chapter: the full test suite, then the adversarial regression suite.

```bash
cd agents/go
mise run test
cd ../..
mise run redteam
cd evals
mise run eval:validate
mise run test
```

Neither needs a model, a provider key, or a network.

**You are done when:**

- `mise run test` passes under the race detector and reports measured coverage; no percentage threshold is enforced.
- Root `mise run redteam` passes the deterministic adversarial policy cases.
- You kept the [4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md#your-turn-how-do-you-add-one-adversarial-case-and-deterministic-validator" >}}) unseen adversarial case, validator, and red-green regression; no generated result artifact or unrelated baseline changed.
- The [4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md#your-turn-does-a-tool-name-prove-trusted-provenance" >}}) name-based trust experiment went red, then the identity check was restored with a clean focused diff.
- Standalone evaluation validation and tests pass without a model.
- You can use the page markers above to say which checkpoints need a configured model and which run offline.
- You can point to [4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md#which-artifacts-can-qualify-a-release" >}}) for the chapter's release-evidence contract.
- Without reopening Chapter 3: you can name which of the six memory stores a value belongs in when it must survive the next turn but not the next session.

Continue to [4.0. Type Safety]({{< relref "/4. Quality/4.0. Type Safety.md" >}}) when you know the first three pages need no model or provider account.
