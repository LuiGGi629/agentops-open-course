# Contributing

Contributions that make the course more accurate, runnable, accessible, or useful are welcome. Small fixes can go directly to a pull request; discuss new dependencies, architecture changes, and substantial chapter rewrites first.

[GOVERNANCE.md](./GOVERNANCE.md) explains how this single-maintainer project makes and reviews decisions.

## How do I set up the repository?

Install [mise](https://mise.jdx.dev/), clone your fork, and run:

```bash
mise run install:maintainer
mise run doctor
```

The maintainer tier installs the pinned Go, documentation, infrastructure, and security tools used by the complete gate. Learners use the smaller `mise run install` tier.

## What should every contribution preserve?

- Keep documentation synchronized with exact sources under `agents/go`, `evals`, `tools`, and `infra`.
- Keep every `content/**/*.md` page in the FAQ frame documented in `AGENTS.md`.
- Give every non-home content page one explicit lowercase kebab-case `slug`; never add a full front-matter `url` that shadows Hugo's reviewed permalink contract.
- Give every new or changed Mermaid diagram adjacent prose describing the same actors, relationships, and sequence.
- Keep the required path account-free and open source; describe hosted models and cloud services as optional proprietary substrates.
- Preserve immutable seed data and keep runtime state under `agents/go/.state` or the documented Kubernetes volume.
- Never commit credentials, populated environment files, generated eval results, runtime state, or private incident content.
- Use `1.` for every ordered Markdown item.
- Preserve local, hosted-CI, runtime, release, and public-proof boundaries in claims.

## Which checks must pass?

Run the same task vocabulary used by hooks and hosted CI:

```bash
mise run format
mise run check
mise run test
mise run scan
```

`format` rewrites Go and markup. `check` validates formatting, static analysis, docs, workflows, data, infrastructure, licenses, and vulnerability boundaries. `test` is deterministic and must not call a model, cluster, collector, paid API, or cloud service. `scan` runs repository secret, vulnerability, licence, and configuration scans.

Also run the module-local gates for a changed module:

```bash
mise run --cd agents/go check
mise run --cd agents/go test
mise run --cd evals check
mise run --cd evals test
mise run --cd tools check
mise run --cd tools test
```

No Go coverage threshold is enforced because the owner has not selected one. Report measured coverage when it helps review; never turn the current measurement into implied policy.

## When should I run model-backed evaluation?

Run it when agent behavior, prompts, model wiring, gateway translation, or scoring expectations change and a configured local model is available:

```bash
mise run --cd agents/go build
mise run --cd evals eval:dev
# Maintainers collect these only from one exact, clean candidate while calibrating policy:
mise run --cd evals eval:policy-trial
mise run --cd evals eval:a2a:policy-trial
mise run --cd evals eval:judge-calibration:trial
# Run these only after the reviewed release policy is approved:
mise run --cd evals eval
mise run --cd evals eval:a2a
```

These tasks can be slow and stochastic. They remain outside the offline gate. Policy and judge trials preserve unsuccessful observations and cannot qualify a release. Canonical REST, A2A, and judge-calibration runs take their pass floor, judge-agreement floor, repeats, mandatory cases, and budgets from `evals/release-policy.json` and fail closed while it is `calibration-required`. Release-bearing artifacts must record matching source-tree, platform, typed model/judge, evalset, calibration, transport, and policy identity.

Do not paste prompts, answers, tool payloads, provider errors, credentials, or judge rationales into a pull request. Use the sanitized result artifacts documented in `evals/README.md`.

## How should I change course examples?

Treat an executable snippet as a public contract:

1. Read the exact source file and installed dependency API it relies on.
1. Run the command from the documented working directory.
1. State whether it is offline, model-backed, container, Kubernetes, cloud, or destructive.
1. Include an observable result and teardown where the exercise creates state.
1. Quote the smallest named source region instead of copying an implementation.
1. Run the documentation, link, accessibility, freshness, and Hugo build gates.
1. Inspect the rendered page at wide and narrow widths.

A source include must stand alone outside a code fence. Add comment-only start and end markers around a stable excerpt and keep exactly one pair per file and region.

## How should I change evaluation assets?

Keep `evals` black-box: it must not require or import the agent module. Use domain vocabulary from immutable `agents/data`, not producer packages.

For a new case:

1. Select the smallest evalset that owns the behavior.
1. Avoid copying the desired answer into the prompt.
1. Declare only the required ordered tool subset.
1. Validate schema, domain references, cost coverage, and import boundaries offline.
1. Run model-backed evidence only after offline validation.
1. Record failures instead of weakening a case or scorer to fit one response.

Release artifacts must remain content-free and include the qualification fields a separate verifier needs.

## How should I submit a pull request?

- Keep one pull request focused on one outcome.
- Explain what changed, why it was needed, how it works, and which proof boundaries passed.
- Use a [Conventional Commits](https://www.conventionalcommits.org/) subject such as `docs: clarify the local gateway setup`.
- Do not add generated-by or co-author attribution.
- Do not claim publication, deployment, exact-head CI, or runtime evidence that you did not observe.

By participating, you agree to follow the [Code of Conduct](./CODE_OF_CONDUCT.md) and [accessibility contract](./ACCESSIBILITY.md). Report security issues through [SECURITY.md](./SECURITY.md), not a public issue.
