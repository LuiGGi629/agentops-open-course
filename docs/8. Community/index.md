---
description: Sustain the reference project, then transform it into your own evidence-backed open-source agent platform.
---

# 8. Community

!!! abstract "In one glance"

    - **You will:** See how this chapter moves from maintaining the reference repository to owning your own, and know which page answers which question.
    - **You need:** Chapter 7 finished, and a clone where `mise run check` passes.
    - **Time:** about 6 minutes, orientation.

## Why is sustainability the final stage of the AgentOps lifecycle?

A colleague clones your repository on a Monday morning. What must they find so they never have to message you?

[Chapter 7](../7. Observability/) left you with an agent that is not only running but observable — you can trace one turn, watch its health, cost the work, and audit every approved write. That is where operating an agent stops and _sustaining a project_ begins.

An agent nobody but you can rebuild, relicense, release, or safely extend is a private artifact, not an operated system. The [AgentOps loop](../0. Overview/0.2. AgentOps.md) only closes if the people who inherit the code can keep it green.

That is why community is the last node in the lifecycle rather than a soft epilogue. **The reference on `main`** — the repository branch this course is written against — is deliberately a _completed, executable_ project: `AGENTS.md` insists it "must not drift into a collection of illustrative snippets". This chapter shows the machinery that keeps it that way, one page per part, and the table below is the map. None of it is agent-specific glamour; all of it is what makes the previous seven chapters reproducible by someone other than the author.

## How does this chapter move from the reference project to your own?

The chapter has two halves, and the split is where you stop reading someone else's repository and start owning one.

Pages 8.0–8.6 are a _maintenance tour of this reference_: how the repository you have been reading is organized, licensed, released, templated, documented, and contributed to. Page 8.7 is the _handoff_. The capstone turns that reference into a platform you own for a domain you understand, keeping the same contracts while replacing the fictional incident domain.

??? note "Deeper: the same two halves as a diagram"

    ```mermaid
    flowchart LR
        subgraph maintain["Maintain the reference · 8.0–8.6"]
            direction LR
            Repo["8.0 Repository"] --> Lic["8.1 License"] --> Rel["8.2 Releases"] --> Tmpl["8.3 Templates"] --> Doc["8.4 Documentation"] --> Con["8.5 Contributions"] --> Aaif["8.6 AAIF"]
        end
        subgraph own["Own your platform · 8.7"]
            Cap["8.7 Capstone"]
        end
        Aaif --> Cap
    ```

The order is not alphabetical; each page assumes the one before it:

| Page                                                       | What it covers                                                                                                      | Why it sits here                                                   |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| [8.0. Repository](./8.0. Repository.md) _(reference)_      | The top-level layout and the README (humans) vs AGENTS.md (agents) split                                            | You need the map before you can maintain anything                  |
| [8.1. License](./8.1. License.md) _(reference)_            | The dual license — CC-BY-4.0 for the prose, MIT for the code — and how to attribute both                            | Know what you may release and reuse before you release or reuse it |
| [8.2. Releases](./8.2. Releases.md) _(reference)_          | Deliberate SemVer, a curated Keep a Changelog history, gates, tags, and release evidence                            | A license makes a release shareable; now cut one                   |
| [8.3. Templates](./8.3. Templates.md) _(concept)_          | Extracting a reusable OSS generator without copying secrets, data, identity, or cloud assumptions                   | Once you can release, you can factor the shape out for reuse       |
| [8.4. Documentation](./8.4. Documentation.md) _(hands-on)_ | Pinned Zensical, the page structure `scripts/check_conventions.py` enforces, snippet mirroring, and safe publishing | The maintenance loop that keeps prose honest and reproducible      |
| [8.5. Contributions](./8.5. Contributions.md) _(hands-on)_ | Issue/PR hygiene and the same format/check/test/scan tasks used by hooks and CI                                     | How anyone else changes the repo without breaking a gate           |
| [8.6. AAIF](./8.6. AAIF.md) _(concept)_                    | Where MCP, A2A, agentgateway, and kagent sit under the AAIF and CNCF, and how to contribute upstream                | Situate the stack in the ecosystem that maintains it beyond you    |
| [8.7. Capstone](./8.7. Capstone.md) _(hands-on)_           | Replace the fictional domain while preserving the OSS-first, authority, quality, gateway, and evidence contracts    | The handoff: turn the reference into your own platform             |

Every page in the first half acts on the same clone you already have, and proves its change with the same `mise` tasks. Only [8.7. Capstone](./8.7. Capstone.md) asks you to change the course, the agent, and the infrastructure in one go.

??? note "Deeper: which directories each page touches"

    Every maintenance page in the first half acts on the same three top-level directories the repository ships: `docs/` (the course prose), `agents/` (the reference agent and its immutable seed), and `infra/` (the data plane and platform). They all defer to the one shared `mise` task vocabulary, so a license note, a docs edit, and a code change are all proven the same way. Chapter 8.7 is the only page that expects you to change all three at once.

## What proves this chapter worked?

This chapter starts no service and tears nothing down; its subject is the project around the agent, not a runtime. Its checkpoint is therefore the same gate every maintenance and contribution task defers to: the one another person must be able to pass on a fork of your work.

Expect a few minutes rather than seconds. `mise run scan` alone walks the full Git history with gitleaks before making two Trivy passes. A failing task names what it tripped on and exits non-zero, so silence means work in progress, not a hang.

```bash
mise run format
mise run check
mise run test
mise run scan
```

**You are done when:**

- All four tasks above pass on your own clone, and `git status --short` prints nothing afterwards.
- You can read the repository map and say which top-level directory owns a given file.
- You can cite the correct license for a given file: CC-BY-4.0 for the prose, MIT for the code.
- You can name what one CI gate protects.
- Another person reproduces your [8.7. Capstone](./8.7. Capstone.md) from a clean clone without asking you for an undocumented step.

Continue to [8.0. Repository](./8.0.%20Repository.md) when those four tasks pass on your own clone, because every page in this chapter changes something they protect.
