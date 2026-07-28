# TODO

Next steps for the AgentOps Open Course, in the order to do them. Written to be picked up by a fresh session with no memory of the previous work.

## Where the repository stands

Two passes have been applied but are **not committed**. `HEAD` is `df28783`; `git status` shows 96 changed files, including this one.

**A newcomer-smoothing pass over `docs/`:**

1. All 76 pages open with `!!! abstract "In one glance"` (**You will** / **You need** / **Time** + page kind) and close with one of three headings — `What proves this page worked?`, `What proves this chapter worked?` (chapter indexes), or `How should you use this page later?` (0.5, 0.6, 0.7) — followed by a `**You are done when:**` list and a `Continue to …` line.
1. 188 `??? note "Deeper: …"` collapsibles (there were none) hold roughly 24k words of relayered depth. Nothing was deleted; a dedicated audit accounted for every removed line and found no losses.
1. `docs/index.md` gained a clone-to-conversation quickstart, so the first command is on the first page instead of ~30k words in.
1. Ten hands-on pages were reordered so a runnable command lands in the first two sections.
1. The page contract is written down in `AGENTS.md` ("Documentation workflow") and `CONTRIBUTING.md`.

**A `scripts/` consolidation:**

1. `check-docs.sh`, `check_frontmatter.py`, `check-skills.sh` and `check-release-metadata.sh` became one `scripts/check_conventions.py` with three subcommands. The `mise run check:docs` / `check:skills` / `check:release-metadata` task names are unchanged, and every page that cited the old filenames was updated.
1. That checker now also enforces the page frame the smoothing pass introduced: the glance block and its three fields, the closing heading, the page-kind vocabulary, chapter-index labels agreeing with the page they describe, `Deeper:` collapsible summaries, a four-per-page collapsible ceiling, no bare-number link labels, and snippet includes inside a code fence. It caught four real label drifts on its first run.
1. `scripts/lib.sh` gives the remaining shell scripts one strict-mode preamble plus `require_cmd`, `make_tmpdir`, `log` and `fail`. Six scripts now declare their tools, so a missing binary says `missing k3d: run 'mise install', then 'mise run doctor:platform' …` instead of `command not found`.
1. `scripts/README.md` maps every script to its task and prerequisite tier.
1. The offline tier no longer needs `ripgrep`: `check_conventions.py release-metadata` deliberately runs on a bare `python3`, because the release workflow has a checkout and nothing else.

Gates after both passes: `mise run format`, `mise run check` (core + infra), `mise run test` (318 passed, 96.16% branch coverage), `mise run check:links` (0 errors over 466 unique links), and a strict Zensical build all pass.

**Decide first:** review the uncommitted diff and either keep it or ask for changes. Everything below assumes it stays. Do not commit anything without being asked.

## 1. Validate the course end to end, against Gemini on Vertex AI

Highest value and the biggest blind spot. Every claim in the smoothing pass was verified **by reading source** — no session has run the agent against a live model, started the host gateway, or created a cluster. Source-reading alone still caught three defects that only a real run should have found, which is the argument that more are waiting.

This machine cannot run `qwen3:4b-instruct` at a usable speed, so validate with **Gemini on Vertex AI through Application Default Credentials**, on the `agentops-open-course` project.

> **This changes how you verify, not what the course teaches.**
>
> The required path stays local Qwen3 through Ollama: account-free, no SaaS, no usage fee. That is a locked course invariant (`AGENTS.md` → "Open-source boundary"). Gemini remains the _optional_ comparison. Do not rewrite pages to make Gemini the default, and do not weaken any "no account needed" claim. You are borrowing the optional path as a test harness because the hardware forces it.

### Set it up

None of this is configured yet: `gcloud config get-value project` returns `fmind-workspace-cli`, ADC is not initialised, and the local `.env` sets the **wrong variable name** (`GOOGLE_GENAI_USE_VERTEXAI`, which `agents/python/src/agent/config.py` does not read — it reads `GOOGLE_GENAI_USE_ENTERPRISE`).

1. ADC login is interactive, so ask the user to run it (in Claude Code, `! gcloud auth application-default login` runs it in-session):

   ```bash
   gcloud auth application-default login
   gcloud auth application-default set-quota-project agentops-open-course
   ```

1. Edit the gitignored local `.env` — not `.env.example` — to the ADC block that `.env.example` already documents:

   ```bash
   AGENT_MODEL_PROVIDER=gemini
   AGENT_MODEL=gemini-3.5-flash
   GOOGLE_GENAI_USE_ENTERPRISE=true
   GOOGLE_CLOUD_PROJECT=agentops-open-course
   GOOGLE_CLOUD_LOCATION=global
   ```

   Remove or blank `GOOGLE_API_KEY`. The cross-field validator in `config.py` **refuses** a key combined with `GOOGLE_GENAI_USE_ENTERPRISE=true`, and its message names the fix.

1. Confirm with `cd agents/python && mise run config:check` before running anything else.

### Then walk it

1. Chapters 1 → 2 → 3 exactly as written. Record every command that fails, every "expected output" block that does not match, and every step slower than its **Time** line implies.
1. Chapter 4: the eval tasks (`eval`, `eval:mlflow`, `eval:ground`, `eval:retrieval`). Note that `eval:retrieval` needs a local embeddings endpoint (`nomic-embed-text` on Ollama) which `doctor:model` does not check — decide whether that page should say "skip this one on the Gemini path".
1. Chapter 5 host gateway: `mise run smoke:host` first (fake model, no spend), then `mise run gateway:host` and the listener checks.
1. Chapter 6 platform: `cluster:start`, `platform:install`, `platform:dev`, then teardown.
1. Fix what breaks. Replace any expected-output block with the real one. Correct the per-page **Time** lines against the clock, then re-reconcile the two course-level totals in `docs/index.md` and `docs/0. Overview/0.0. Course.md`.

Two cautions: Vertex calls are billed, so keep runs small and prefer `smoke:host` over live model loops where a fake model proves the same composition. And a Gemini run will differ from a Qwen3 run — where a page's expected output is model-dependent, say so on the page rather than pinning Gemini's wording.

## 2. Add an exercise at the end of every chapter

Chapters 0, 1, 2, 5, 6 and 8 ship no exercise; Chapter 3 has eight, Chapter 4 has two, Chapter 7 has one. Tracked as [#47](https://github.com/MLOps-Courses/agentops-open-course/issues/47), which lists 0, 1, 2, 5 and 6 — add 8.

Place the exercise **at the end**, as the last section before the closing `What proves this page worked?`, so the reader has finished the material before being asked to extend it. Copy the shape that already works, for example in `docs/3. Capabilities/3.1. Tools.md`:

```markdown
## How would you add a `get_oncall_schedule` read tool?

Exercise: go beyond the checkpoint by shipping a new read-only tool end to end.

- **Goal**: …
- **Files to touch**: …
- **Gate that proves completion**: …
```

Rules that make an exercise good here:

1. The H2 is a `How would you …?` question. Every H2 must end with `?` — CI enforces it.
1. The gate is a command that already exists and actually passes once the exercise is done. Run it before publishing it.
1. One exercise per chapter is enough, on the chapter's most hands-on page. Prefer an offline gate; use a model-backed gate only where the chapter already requires a model.
1. Keep the difficulty honest for the audience: knows Python and a terminal, does not know agents, Kubernetes or observability.

Before writing these, settle [#46](https://github.com/MLOps-Courses/agentops-open-course/issues/46) ("Check your understanding" self-assessment blocks). A quiz block and an exercise compete for the same slot at the end of a page; pick one rather than shipping both on 76 pages.

## 3. Deduplicate and unify wording across chapters

The audit found the same argument stated three or four times in most chapters — the doctor tier ladder, `mise install` versus `mise run install`, the host/local/GKE profile table, the gate-versus-evidence split, the "a policy enforced once at a shared boundary cannot be forgotten by the next client" maxim. Each repetition is worded slightly differently, so it reads as new information rather than reinforcement.

**Do this with one agent per chapter, never one per page.** The smoothing pass deliberately told the per-page agents not to delete duplicated content, because two agents each removing "their" copy loses it. One agent that sees a whole chapter can pick the owning page and replace the others with a link.

Start where the audit was most specific:

1. Chapter 1 — four topics with no single owner (doctor ladder, the two install commands, `check:core` vs `check`, the dotenv/offline-gate rule).
1. Chapter 4 — the gate-versus-evidence split is explained on the index, in 4.3, and four times inside 4.4.
1. Chapter 7 — the host/local/GKE profile split is restated on the index, 7.1, 7.2 and 7.3. The index literally says "every sibling page re-states this split".
1. Chapter 5 — the index restates 5.0's opening argument almost claim for claim.

Consistency belongs in the same pass: one name per concept, one definition per term (at first use, linked to the glossary afterwards), and identical wording for a caveat that appears on several pages.

## 4. Keep `scripts/` honest as it grows

The consolidation described at the top of this file is **done**. What is left is upkeep, not a project:

1. `infra/scripts/` was left alone on purpose — `gateway-host.sh` (562 lines), `smoke-host.sh`, the backup/restore/drill trio and `doctor.sh` start containers, publish ports and trap signals, and are quoted verbatim in Chapters 5, 6 and 7. Shell is the right tool there. If you touch them, add `require_cmd` and source `scripts/lib.sh`; do not rewrite them in Python.
1. New rule ideas belong in `scripts/check_conventions.py`, not in a new shell script. The rule of thumb is written down in `scripts/README.md`: shell for orchestration, Python for text.
1. `check-licenses.sh` is still 153 lines of `jq` over `uv` metadata. It is a genuine pipeline over another tool's JSON, so it stays shell unless it grows a parsing rule.

## 5. Close out the small open defects

Two of the three are done: the `!!! danger "What a wrong run looks like"` block on `docs/2. Agents/2.1. First Agent.md` now asks the reader to check the ids (which `adk run` prints) rather than the tool call (which it does not), and `docs/3. Capabilities/3.6. A2A.md` promises a protocol walk-through with the hands-on approval round trip deferred to 5.3.

What is left is one judgement call, and it needs item 1 first:

1. Course-level time — `docs/index.md` and `docs/0. Overview/0.0. Course.md` now state both "12 to 19 hours to read and clear the checkpoints" and "about 29 hours to run every command", because the per-page **Time** lines sum to ~29 h. Recheck both against the clock once you have walked the course, and keep or collapse the two-column table as the evidence supports.
1. Once a live model is available, `docs/3. Capabilities/3.6. A2A.md` could carry the real two-request handshake against `http://localhost:8080/` instead of deferring it. Verify the payloads before publishing them.

## 6. Reconcile the open issues

There are 50 open issues (#46–#95). Two are affected by the smoothing pass; decide what to do with each from the current state of the repository:

1. [#48](https://github.com/MLOps-Courses/agentops-open-course/issues/48) "per-page learning objectives, time, prerequisites, and difficulty metadata" — delivered by the glance block, except for difficulty.
1. [#49](https://github.com/MLOps-Courses/agentops-open-course/issues/49) "Recap key-takeaways section" — partly delivered: seven pages carry `!!! success "Key takeaways"` and every page now ends with a `**You are done when:**` list.

Close, narrow, or keep them, and say which. While you are in there, check whether any other issue in #46–#95 was silently resolved by the smoothing pass.

## 7. Final sweep: green, clean, and good OSS practice

Do this last, once items 1–6 have settled.

### Everything green

```bash
mise run format
mise run check          # core + infra
mise run test           # 318 tests, 95% branch-coverage floor
mise run scan           # gitleaks history + Trivy
mise run build          # strict Zensical build
```

No warning suppressed, no test skipped, no assertion weakened to force a pass. If something is genuinely broken, fix the cause or report it — `AGENTS.md` → "Definition of done".

### Clean structure

1. `git status --short` is empty after `mise run format` on a clean clone.
1. No stray files at the repository root. **Delete this `TODO.md` when its items are done** — the repository deliberately carries no plan files, and `PLAN.md`/`TODO.md` were removed once before for that reason.
1. `AGENTS.md`, `CONTRIBUTING.md` and `scripts/README.md` still describe what the repository actually does after your changes, especially the page contract and the script-to-task map.

### Good OSS practice

The community-health baseline is already in place: `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `LICENSE` (dual CC-BY-4.0 / MIT), `CHANGELOG.md`, `CITATION.cff`, four issue forms and a PR template. What needs attention:

1. `README.md` was brought in line with `docs/index.md`: it now installs `mise` and Ollama before pulling a model, names the three seed ids as the first-run expectation, and describes the page frame. Re-read it after item 1 in case the walk changes the quickstart.
1. Keep the README a **front door, not a duplicate**: what the course is, who it is for, the shortest path to a running agent, where the real content lives, how to contribute, licence. Anything longer belongs in `docs/`.
1. Re-check the OSS boundary claim end to end after item 1 — the required path must still be genuinely account-free, with Gemini and GCP named as optional throughout. Using Vertex to _test_ must not leak into the course's promises.
1. Three tracked gaps are real OSS-maturity items if you want them: [#65](https://github.com/MLOps-Courses/agentops-open-course/issues/65) GOVERNANCE, [#67](https://github.com/MLOps-Courses/agentops-open-course/issues/67) ROADMAP, [#64](https://github.com/MLOps-Courses/agentops-open-course/issues/64) DOI/Zenodo.

## Working notes

- One convention at a time: `uv run python scripts/check_conventions.py docs` is seconds, where `mise run check:docs` also builds the site and `mise run check:core` takes about two minutes.
- `dprint` takes globs, not directories: `dprint fmt "docs/1. Setup/*.md"` works, `dprint fmt "docs/1. Setup"` silently formats nothing.
- Content indented inside `!!!` and `???` blocks is **not** reformatted by dprint. Tables moved into a collapsible keep whatever alignment they had.
- A focused `uv run pytest tests/test_x.py` exits non-zero even when every test passes, because `--cov-fail-under=95` is in `addopts`. Several pages state this caveat; keep saying it rather than adding `--no-cov`, which the course reserves for `redteam` and `eval:validate`.
- Every ordered list item is written `1.`, including in this file.
- When you fan work out to parallel agents, review the exemplar you wrote for them too. The reviewers in the last pass found two real regressions in the hand-written reference page that every other agent had been told to imitate.
