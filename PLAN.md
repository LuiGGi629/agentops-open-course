# PLAN — Migrate the AgentOps Open Course documentation from Zensical to Hugo

This file is a **prompt**. Read it end to end, confirm the open decisions in §10, then execute the phases in order. Everything below was verified against the source repository on 2026-08-06; re-verify every count in §3 before you rely on it.

## 1. Mission

Stand up this repository (`agentops-open-course-go`) as the **Hugo build of the AgentOps Open Course documentation**. The course content, the Python agent, the infrastructure, and the learner-facing workflow stay exactly as they are. Only the documentation **build system** changes: Zensical + Material for MkDocs → Hugo.

The "-go" suffix names the **generator** (Hugo is a Go binary), not a Go rewrite of the course.

### In scope

1. The 89 Markdown pages under `docs/`, their assets, and the site they produce.
1. `mkdocs.yml` → Hugo configuration, theme, layouts, and shortcodes.
1. Every repository gate that reads or builds the docs: `build:docs`, `serve`, `check:docs`, `check:accessibility`, `check:links`, plus the parts of `scripts/check_conventions.py` and `scripts/docs_routes.py` that encode Material/MkDocs semantics.
1. The published URL contract in `docs/released-urls.json`.

### Out of scope — do not touch

1. The agent (`agents/`), infrastructure (`infra/`), skills (`skills/`), load tests (`load/`), web client (`clients/`), and every non-docs script.
1. Course prose. You are migrating **syntax and build**, not rewriting sentences. If a page's meaning would change, stop and report it instead of editing it.
1. Any Go rewrite of the course material.

## 2. Ground rules

1. **The source repository is read-only.** `../agentops-open-course` is the reference. Never write to it, never run a task that mutates it (`mise run format` inside it writes files). Copy what you need into this repository first, then work here.
1. Follow the standards in `~/.agents/skills/` — `mise` for the task vocabulary, `lefthook` for hooks, `dprint` for Markdown/JSON/YAML/TOML formatting, `github-actions` for CI.
1. Preserve the source repo's authoring invariants (§3.3). They are deliberate, and several gates enforce them. If Hugo cannot express one, say so in the report rather than dropping it.
1. No technical debt, no `# TODO`, no gate weakened to go green. If a check cannot be ported honestly, leave it failing and report why.
1. Conventional Commits. No attribution or co-author trailers.

## 3. Source inventory — what you are migrating

### 3.1 Content

| Chapter          | Pages                          | Words        |
| ---------------- | ------------------------------ | ------------ |
| 0. Overview      | 9                              | 19,610       |
| 1. Setup         | 7                              | 13,916       |
| 2. Agents        | 7                              | 17,858       |
| 3. Capabilities  | 9                              | 27,253       |
| 4. Quality       | 8                              | 26,782       |
| 5. Gateway       | 8                              | 22,806       |
| 6. Platform      | 9                              | 22,491       |
| 7. Observability | 9                              | 25,646       |
| 8. Community     | 9                              | 22,652       |
| **Total**        | **89** (incl. `docs/index.md`) | **~200,000** |

Filenames carry the numbering and contain spaces and dots: `docs/2. Agents/2.1. First Agent.md`. Hugo derives URLs from filenames, so this is the single highest-risk detail in the migration (§9.4).

### 3.2 Material-specific constructs to convert

| Construct                        | Count | Notes                                                                                |
| -------------------------------- | ----- | ------------------------------------------------------------------------------------ |
| Admonitions (`!!! …` / `???`)    | 319   | A fixed semantic vocabulary; `check_admonitions` enforces it                         |
| Mermaid fences                   | 133   | Via `pymdownx.superfences` custom fence                                              |
| Snippet includes (`--8<--`)      | 38    | Named `[start:x]`/`[end:x]` regions, `check_paths: true`, `dedent_subsections: true` |
| `attr_list` / `md_in_html` usage | 5     | Low volume, hand-convert                                                             |
| Content tabs (`=== "…"`)         | 0     | Declared in `mkdocs.yml` but unused — drop the feature                               |

Also present: `pymdownx.details` (collapsibles), `pymdownx.highlight` + `pymdownx.inlinehilite`, `overrides/main.html` and `overrides/404.html`, `docs/javascripts/accessibility.js`, `docs/stylesheets/extra.css`, self-hosted Inter/Outfit variable fonts, and a social card under `docs/assets/`.

`mkdocs.yml` sets `strict: true` and `use_directory_urls: false` — the second one is why published URLs end in `.html`, and it is baked into `docs/released-urls.json` and into `check_conventions.deployed_path()`.

### 3.3 Authoring contracts that must survive

`scripts/check_conventions.py` enforces roughly twenty page contracts. The ones coupled to the build system are:

1. `deployed_path()` — derives the published path from the source path under `use_directory_urls=false`. Hugo's URL model differs; this function must be rewritten.
1. `check_admonitions` — the allowed admonition vocabulary, matched on `!!!`/`???` syntax.
1. `check_collapsibles` — collapsible titling and a density cap, matched on `???`.
1. `check_snippets` / `check_snippet_targets` / `check_source_snippet_coverage` — an include must sit inside a fence and must resolve to one bounded repository region. This is the guarantee that keeps quoted code identical to real source; it is the most valuable thing in the file and the hardest to reproduce (§9.1).
1. `check_front_matter`, `check_headings` (every H2 a question), `check_glance`, `check_closing`, `check_kind`, `check_link_labels`, `check_page_link_targets`, `check_ordered_lists`, `check_hands_on_action` — generator-independent. They should keep passing untouched; treat any new failure as a migration defect.

Explicit navigation is an invariant: `mkdocs.yml` orders every page by hand so adding a page cannot silently reorder the learning path. Preserve that property (§4.4).

### 3.4 Tasks and gates to port

| Task                  | Today                                                    | After                                                                                                                         |
| --------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `serve`               | `uv run zensical serve --dev-addr 127.0.0.1:8003`        | `hugo server --port 8003` — **keep :8003**, the port contract in `AGENTS.md` is enforced by `check_conventions.PORT_CONTRACT` |
| `build:docs`          | `zensical build` + `scripts/docs_routes.py site`         | `hugo --minify` + alias handling (§8.4)                                                                                       |
| `check:docs`          | authoring tests + site build + rendered-route validation | same, re-pointed at Hugo output                                                                                               |
| `check:accessibility` | `build:docs` + `scripts/accessibility_browser.py`        | same script, re-pointed at Hugo's DOM (§9.6)                                                                                  |
| `check:links`         | `lychee --offline` over Markdown                         | unchanged, plus Hugo's own ref validation (§8.3)                                                                              |

## 4. Target design

### 4.1 Toolchain

1. `hugo` **v0.164.0** (extended edition), pinned in `mise.toml` under `[tools]`. Verify it is still the latest stable before pinning; do not use a release candidate.
1. Hugo replaces `zensical` in `pyproject.toml`. Keep `uv` only if the remaining docs gates still need Python — they do today (`check_conventions.py`, `accessibility_browser.py`), so do not promise a Python-free repo.
1. Everything else — `dprint`, `lefthook`, `lychee`, `gitleaks`, `actionlint`, `trivy` — is unchanged.

### 4.2 Theme

Default recommendation: **Hextra** (`github.com/imfing/hextra`), consumed as a Hugo Module so its version is pinned in `go.mod` alongside everything else. It is the closest match to what the course already uses: built-in offline full-text search (FlexSearch), callouts, tabs, steps, filetree, cards, GitHub-style alerts (v0.9.0+), dark mode, automatic TOC, breadcrumbs, and sidebar navigation — plus Mermaid and LaTeX wiring.

Confirm this in §10 before building on it. The alternative is a bare custom theme: total control over the DOM (which helps the accessibility gate) at the cost of writing search, navigation, and dark mode by hand.

### 4.3 Repository layout

```text
agentops-open-course-go/
├── hugo.toml                 // site config, params, menus, markup, outputs
├── content/                  // the 89 pages, ported from docs/
├── assets/                   // fonts, CSS, JS — processed and fingerprinted by Hugo
├── static/                   // files served verbatim (social card, robots.txt)
├── data/                     // JSON/YAML that generates tables (§8.6)
├── layouts/
│   ├── _markup/              // render hooks: codeblock-mermaid, link, blockquote
│   ├── shortcodes/           // include.html (§4.5), callout wrappers
│   └── partials/             // head, footer, accessibility hooks
├── scripts/                  // ported gates
└── mise.toml                 // install, format, check, test, build, serve/watch
```

### 4.4 Navigation

Do **not** scatter ordering into 89 `weight:` front-matter fields — that discards the explicit-nav invariant. Define the learning path once, in `hugo.toml` under `[menus]` (or a single `data/nav.yaml` consumed by a partial), mirroring the current `nav:` block including the cross-chapter entry (`Prerequisite — 1.3. Kubernetes` appears inside chapter 6) and the nested `Optional OSS maintenance` group in chapter 8.

Add a gate that fails when a page under `content/` is absent from the menu, reproducing what MkDocs `strict: true` gives for free.

### 4.5 The include shortcode — the critical piece

Hugo has no `pymdownx.snippets`. Write `layouts/shortcodes/include.html` that:

1. Takes a repository-relative path plus an optional section name.
1. Reads the file, extracts the region between the existing `--8<-- [start:name]` / `--8<-- [end:name]` markers (**keep the markers as they are** — the source files being quoted are out of scope and must not be edited), dedents it, and emits it through `transform.Highlight` with the right language.
1. Calls `errorf` when the file or the named region is missing, so the build **fails** rather than rendering an empty block. This is non-negotiable: it is the current `check_paths: true` guarantee, and losing it means shipping learners code that no longer exists.

Prefer reading through Hugo's asset/mount pipeline (`resources.Get` over a mounted source directory) rather than `os.ReadFile`, so the dev server reliably rebuilds when a quoted source file changes. Verify this behavior explicitly in Phase 1 — it is not documented, and a silent stale-include in `hugo server` would be a bad regression.

### 4.6 Feature mapping

| Material / Zensical        | Hugo                                                                                      |
| -------------------------- | ----------------------------------------------------------------------------------------- |
| `!!! note "Title"`         | Hextra `{{< callout >}}` or a blockquote render hook over GitHub alerts                   |
| `??? note` (collapsible)   | Hextra `{{< details >}}`                                                                  |
| `` ```mermaid `` fence     | `layouts/_markup/render-codeblock-mermaid.html` + a **self-hosted** Mermaid bundle (§9.3) |
| `--8<--` include           | `{{< include >}}` shortcode (§4.5)                                                        |
| `pymdownx.highlight`       | Chroma (built in), `[markup.highlight]` in `hugo.toml`                                    |
| `attr_list` / `md_in_html` | Goldmark attributes; enable `markup.goldmark.parser.attribute`                            |
| `overrides/main.html`      | `layouts/partials/head.html` + baseof override                                            |
| `overrides/404.html`       | `layouts/404.html`                                                                        |
| `theme.palette` (indigo)   | Theme params + `assets/css/extra.css`                                                     |
| `search.*` features        | Hextra FlexSearch (not a 1:1 mapping — §9.5)                                              |
| `strict: true`             | `hugo --panicOnWarning` + `refLinksErrorLevel = "ERROR"` + the nav gate (§4.4)            |
| `scripts/docs_routes.py`   | Hugo `aliases` front matter (§8.4)                                                        |

## 5. Phase 0 — spike before you commit (half a day, throwaway)

Do not start the bulk migration until these four unknowns are resolved. Build a throwaway site with **five representative pages** — pick `docs/index.md`, one chapter index, one hands-on page with includes, one page dense with Mermaid, and `docs/0. Overview/0.7. Glossary.md`.

1. **URLs.** Can Hugo reproduce or alias every path in `docs/released-urls.json`? Prove it for at least one page whose name contains both spaces and dots.
1. **Includes.** Does the shortcode from §4.5 extract a real region, fail loudly on a bad path, and live-reload under `hugo server`?
1. **Mermaid.** Does a self-hosted bundle render all diagram types used in the course, in both light and dark themes?
1. **Accessibility.** Does `scripts/accessibility_browser.py` pass against the theme's DOM unmodified, or how much rework does it need?

Report the four answers before proceeding. If (1) or (2) cannot be met, stop — those are the migration's load-bearing guarantees.

## 6. Phases

### Phase 1 — Scaffold

1. `git init`, `mise.toml` with the standard vocabulary, `lefthook.yml`, `dprint.json`, `.gitignore`, LICENSE, `hugo.toml`, and the theme as a Hugo Module.
1. Port `layouts/`, the include shortcode, the Mermaid render hook, fonts, CSS, and JS.
1. Acceptance: `mise run install` and `mise run serve` work on a five-page site.

### Phase 2 — Mechanical content conversion

1. Copy `docs/` → `content/` verbatim. Commit that as its own commit, so every later diff is pure syntax conversion and reviewable.
1. Write a **conversion script** (kept in `scripts/`, not a one-off) that rewrites admonitions, collapsibles, and includes. 319 + 38 constructs is too many to hand-edit reliably, and a script makes the transformation auditable and re-runnable if the source repo advances.
1. Add front matter Hugo needs (`title`, `aliases`) without disturbing the existing `description` field the authoring gate requires.
1. Acceptance: the site builds with zero warnings; a diff of rendered text (not HTML) against the Zensical build shows only markup differences.

### Phase 3 — Gates

1. Rewrite `deployed_path()` and the admonition/collapsible/snippet matchers in `check_conventions.py` for Hugo syntax. Keep every other contract byte-identical.
1. Re-point `check:accessibility` and `check:links`. Add the nav-completeness gate.
1. Port the `docs/released-urls.json` contract to `aliases`, and keep a test that every released URL still resolves to a live page.
1. Acceptance: `mise run check` and `mise run test` pass with zero warnings.

### Phase 4 — Parity review

1. Render both sites and compare page-by-page: headings, code blocks, diagrams, tables, search, dark mode, keyboard navigation, 404.
1. Confirm every one of the 38 includes shows the same source region as before.
1. Acceptance: a written parity report listing every intentional difference.

### Phase 5 — CI and delivery

1. GitHub Actions running the same `mise run` tasks. Keep artifact retention at 7 days.
1. `README.md` and `AGENTS.md` describing the Hugo build, per the `readme-agents` skill.
1. Acceptance: green CI on a clean clone.

## 7. What Hugo does not change

Being explicit, so nobody re-litigates these: course prose, page structure, the FAQ frame, chapter numbering, the port contract, the agent, the infrastructure, the skills, and the release process are all untouched. This is a build-system swap.

## 8. Enhancements Hugo brings

Verify each before claiming it in the report — several are theme-dependent.

1. **Maturity.** Hugo v0.164.0 against Zensical `0.0.52`. The current pin carries an explicit comment in `pyproject.toml` — "alpha: pin exactly and re-verify the generated site on every upgrade" — and `mkdocs.yml` notes the config must migrate to `zensical.toml` at 1.0. Hugo removes both of those standing obligations.
1. **Speed.** Sub-second incremental rebuilds; a 200,000-word site builds in well under a second. The authoring loop tightens, and `check:docs` / `check:accessibility` — which both build the site — get cheaper on every CI run.
1. **Build-time link validation.** `{{< relref >}}` with `refLinksErrorLevel = "ERROR"` fails the build on a broken internal link. Today that class of breakage is caught by `lychee` in a separate gate, or not until a reader hits it. This is a genuine correctness gain for a course whose pages cross-reference heavily (`check_page_link_targets` exists precisely because of it).
1. **Aliases replace a bespoke script.** `scripts/docs_routes.py` exists to materialize historical URLs from `docs/released-urls.json` after the build. Hugo generates redirect pages natively from `aliases` front matter — one custom script and its tests retire.
1. **A real asset pipeline.** Fingerprinting with subresource integrity, JS bundling via `js.Build`, image processing for the social card, and font handling — all first-class. Today the fonts, CSS, and `accessibility.js` are hand-vendored under `docs/`.
1. **Pages generated from data.** `data/` files plus templates let hand-maintained tables become generated ones. The repo already has the inputs: the port inventory duplicated between `AGENTS.md`, `check_conventions.PORT_CONTRACT`, and `docs/0. Overview/0.3. Ecosystem.md`, and `scripts/container-matrix.json`. Generating the docs table from one source kills a documented four-place update ritual. **This is the biggest long-term win** and the one most worth scoping into Phase 5.
1. **Additional output formats from the same content.** A JSON index, per-chapter feeds, or an `llms.txt` for the agent audience, without a plugin.
1. **Versioned, module-pinned theme.** Hugo Modules version the theme in `go.mod`, so a theme bump is a reviewable lockfile diff instead of a pinned alpha package.
1. **Multilingual support** is first-class if the course is ever translated.
1. **Single static binary.** `hugo` installs from `mise` with no interpreter, no virtualenv, no 350 MB dependency chain. Note the honest caveat: the docs gates still use Python, so this thins the toolchain — it does not remove Python from the repository.

## 9. Regressions Hugo brings

State these plainly in the final report; do not let any of them be discovered by a learner.

1. **No `pymdownx.snippets` equivalent.** The 38 includes rely on named regions, path checking, and subsection dedent. All three must be hand-built (§4.5) and, critically, so must the _build-fails-on-missing-source_ guarantee. Hugo also may not watch files read outside the asset pipeline, risking stale includes in the dev server. This is the single largest risk in the migration.
1. **319 admonitions and every collapsible get rewritten.** Mechanical, but it touches nearly every page and it invalidates the matchers in `check_conventions.py`. Any conversion bug is a content bug across 200,000 words.
1. **Mermaid is not built in.** Hugo requires a custom codeblock render hook, and the documented approach loads Mermaid from a CDN — unacceptable here, since the course self-hosts its assets. Expect to vendor and bundle Mermaid yourself, and to rebuild the light/dark theme-aware diagram colors that Material provided for free across all 133 diagrams.
1. **Published URLs change.** Material with `use_directory_urls: false` publishes `0. Overview/0.0. Course.html`; Hugo slugifies filenames into directory URLs. Every released URL in `docs/released-urls.json`, every external link, bookmark, and shared social card is affected. Hugo `aliases` can cover it, but the coverage must be complete and tested, and the canonical URLs in the site config and social metadata must be re-checked.
1. **Search is different, not equivalent.** `mkdocs.yml` enables `search.highlight`, `search.share`, and `search.suggest` — mature, multi-language, and tuned. Hextra's FlexSearch is good but does not map feature-for-feature. Expect to lose or re-implement some of it.
1. **The accessibility surface must be re-earned.** `ACCESSIBILITY.md`, `overrides/`, `docs/javascripts/accessibility.js`, and the `check:accessibility` gate were all built against Material's DOM. Every one of them must be re-validated against the new theme. A theme's claim of "automated accessibility checks" does not discharge this repository's own gate.
1. **Customization gets more expensive.** Material customization is `mkdocs.yml` plus two files in `overrides/`. Hugo customization is Go templates, layout lookup order, and theme overrides — more powerful, and more to learn and maintain.
1. **`strict: true` has no single switch.** MkDocs fails the build on any warning, including pages missing from the nav. In Hugo that strictness must be assembled from `--panicOnWarning`, `refLinksErrorLevel`, and a nav-completeness gate you write yourself.
1. **Material ecosystem features are gone.** Social-card generation, tags, `mike` versioning, instant navigation, and code annotations have no drop-in Hugo equivalent. Content tabs are declared but unused (0 occurrences), so that one is free.
1. **The free escape hatch closes.** Today, if Zensical's alpha breaks, `mkdocs-material` builds the identical content with zero edits. After the Hugo migration, that fallback no longer exists — the syntax has diverged. Migrating is a one-way door.

**Net assessment.** Hugo is a defensible choice: it trades an alpha dependency for a mature one, retires a bespoke redirect script, and opens the door to generating duplicated tables from data. The cost is concentrated and knowable — the include shortcode, the Mermaid pipeline, the URL contract, and the accessibility gate. Three of those four are one-time engineering; the fourth (includes) is the one that can silently degrade the course's core promise that quoted code is real code. Build that first, gate it hardest.

## 10. Decisions to confirm before Phase 2

Ask, do not assume:

1. **Theme** — Hextra (fast, batteries-included, less DOM control) or a custom minimal theme (full control for the accessibility gate, much more work)?
1. **URL strategy** — keep `.html`-style URLs to preserve the released contract exactly, or move to clean directory URLs and cover history with aliases?
1. **Repository relationship** — does this repo carry a full copy of the course (agent, infra, scripts) so its gates run, or only `content/` plus the docs gates, consuming the rest from the source repo? This decides whether the 38 includes can resolve at all.
1. **Fate of the source repo** — is this a replacement, or a parallel evaluation to be compared before switching? It changes how much CI and release wiring Phase 5 needs.

## 11. Definition of done

1. `mise run install`, `format`, `check`, `test`, `build` all pass warning-free on a clean clone.
1. All 89 pages render; all 38 includes resolve to the correct source regions; all 133 diagrams render in both themes.
1. Every URL in `docs/released-urls.json` resolves.
1. `check_conventions.py` passes with every non-build contract unchanged.
1. `check:accessibility` passes against the new DOM.
1. A parity report (§Phase 4) and an honest restatement of §9 as shipped, not as predicted.
