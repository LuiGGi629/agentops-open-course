---
title: "4. Quality"
description: "Prove the agent behaves under adversarial pressure: types, linting, tests, metrics, evaluations, guardrails, and security."
slug: "4-quality"
---

{{% admonition abstract "In one glance" %}}

- **You will:** See which failure each of the chapter's seven layers catches, and run the three offline commands that cover all of them.
- **You need:** Chapter 3 finished and `mise run test` passing.
- **Time:** about 6 minutes, orientation. {{% /admonition %}}

## Three ways to be wrong while everything looks right

A value that compiles. A value that lies. A run that is green for the wrong reason.

The first is a `string` holding `INC-2; DROP TABLE incidents`. The compiler has no opinion about it, and neither does a test that never thought to try it. The second is a confident paragraph with nothing underneath it — indistinguishable, from the outside, from the grounded one you watched in [2.1. First Agent]({{< relref "/2. Agents/2.1. First Agent.md" >}}). The third is the one that ends careers quietly: a suite that passes because the interesting case was never in it, or a pass rate that clears its floor while the safety case underneath it fails.

Chapters 2 and 3 gave the agent a conversation and capabilities. This chapter answers the question those two raise: **does it still behave when its inputs are hostile, and how would you know?** That is not one property, so it is not one gate. Each page below adds a layer, and each layer catches a class of failure the cheaper layers underneath cannot see.

## Seven layers, and the failure each one catches

Read the order as a cost curve. The early layers are free, offline, and instant; the later ones need a model or a running gateway, and they exist because the cheap layers genuinely cannot answer their questions.

- **[4.0. Type Safety]({{< relref "/4. Quality/4.0. Type Safety.md" >}})** _(concept · offline)_: a hostile tool argument dies at the boundary that owns it, instead of reaching the database.
- **[4.1. Linting]({{< relref "/4. Quality/4.1. Linting.md" >}})** _(hands-on · offline)_: an ignored error in a transaction — a bug that compiles perfectly — is caught by a machine.
- **[4.2. Testing]({{< relref "/4. Quality/4.2. Testing.md" >}})** _(hands-on · offline)_: the model is replaced and everything else stays real, so most of the agent is decided in seconds.
- **[4.3. Metrics]({{< relref "/4. Quality/4.3. Metrics.md" >}})** _(reference · needs a model for the live rows)_: a scorecard where every number names its command, candidate, and proof class.
- **[4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md" >}})** _(hands-on · offline validation, model-backed runs)_: black-box turns over the wire, scored by structure first and by a judge only for the residue.
- **[4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md" >}})** _(hands-on · offline, except the optional gateway run)_: injected text is defused, personal data is masked twice, and no write lands without confirmation and one transaction.
- **[4.6. Security]({{< relref "/4. Quality/4.6. Security.md" >}})** _(hands-on · offline; scanners may use the network)_: authority the agent never holds cannot be misused, and a suite proves nobody quietly added it back.

Expect to write code rather than only read it. Three pages ask you to break something on purpose and restore it — [4.1. Linting]({{< relref "/4. Quality/4.1. Linting.md" >}}), [4.5. Guardrails]({{< relref "/4. Quality/4.5. Guardrails.md#your-turn-does-a-tool-name-prove-trusted-provenance" >}}), and [4.6. Security]({{< relref "/4. Quality/4.6. Security.md" >}}) — and two ask you to add something you keep, in [4.2. Testing]({{< relref "/4. Quality/4.2. Testing.md" >}}) and [4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md#your-turn-how-do-you-add-one-adversarial-case-and-deterministic-validator" >}}).

One distinction runs through all seven pages and is owned in one place: what a deterministic gate proves versus what a model-backed observation suggests. [0.2. Evidence]({{< relref "/0. Overview/0.2. Evidence.md" >}}) holds it, and each page links there rather than restating it.

## One offline sweep covers the chapter

Three commands, run from the repository root, none of which needs a model, a provider key, or a network:

```bash
mise run test
mise run redteam
mise run eval:validate
```

The first is the widest: it fans out to all three Go modules at once, and two of them end by enforcing a coverage floor.

```text
[test:evals] DONE 395 tests in 12.386s
[test:evals] evals meets the 80% per-package coverage floor
[test:tools] DONE 296 tests in 26.869s
[test:go] DONE 1815 tests, 1 skipped in 25.223s
[test:go] agents/go meets the 80% per-package coverage floor
Finished in 39.17s
```

Six lines lifted from that run in the order they arrived. The three suites run concurrently, so the full log interleaves them and prints a coverage line for every package; `tools` is the maintainer module and deliberately sits outside the floor.

## What this chapter proved

- Just under twenty-five hundred tests decide the agent, the evaluator, and the repository tooling under the race detector in well under a minute, with no service to start and nothing spending a token.
- `mise run redteam` settles the adversarial policy cases and `mise run eval:validate` proves the evalsets against the seed, both without a model.
- Three of the seven pages hand you something to break and restore; two hand you something to keep — a table case in [4.2. Testing]({{< relref "/4. Quality/4.2. Testing.md" >}}) and an evaluation case in [4.4. Evaluations]({{< relref "/4. Quality/4.4. Evaluations.md#your-turn-how-do-you-add-one-adversarial-case-and-deterministic-validator" >}}).
- By the end you can take any green result in this chapter and say whether it is a gate or an observation — and name the command behind it.

The agent refuses hostile input either way; the guardrails were already there when you arrived. What changes over these seven pages is that you can prove it in a second, and say which of its guarantees are deterministic and which are merely encouraging — which is exactly the distinction you need before putting it behind a gateway that other people can reach.

Continue to [4.0. Type Safety]({{< relref "/4. Quality/4.0. Type Safety.md" >}}); the first three pages need no model and no account.
