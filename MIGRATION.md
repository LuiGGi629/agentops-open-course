# Zensical → Hugo migration report

This repository is the **Hugo build** of the AgentOps Open Course documentation. The course content, the agent, the infrastructure, and the learner-facing workflow are unchanged; only the documentation build system differs. This file records what was migrated, what differs in the rendered output, and what is still outstanding.

Status: **parallel evaluation build. Not deployed.** No CNAME, no Pages workflow, no DNS record ships from here. `baseURL` still names `https://agentops-open-course.fmind.dev/` so canonical and social metadata stay comparable with the Zensical build.

## 1. Corrections to the plan's inventory

`PLAN.md` §3 asked for its counts to be re-verified. Three of five were exact; two were not.

| Item             | Plan | Actual    | Note                                                                                                                       |
| ---------------- | ---- | --------- | -------------------------------------------------------------------------------------------------------------------------- |
| Markdown pages   | 89   | **76**    | The plan's own per-chapter column sums to 75, and chapters 5 and 6 list two chapter-1 pages as cross-chapter prerequisites |
| Admonitions      | 319  | **319** ✓ | 154 `!!!` plus 165 `???` collapsibles                                                                                      |
| Mermaid fences   | 133  | **133** ✓ |                                                                                                                            |
| Snippet includes | 38   | **34**    | The other 4 are prose _mentions_ of `--8<--` inside inline code, not includes                                              |
| Content tabs     | 0    | **0** ✓   | Feature dropped, as planned                                                                                                |

## 2. What the build is now

| Concern                      | Where it lives                                                                          |
| ---------------------------- | --------------------------------------------------------------------------------------- |
| Generator                    | Hugo **0.164.0** extended, pinned in `mise.toml`                                        |
| Theme                        | **Hextra v0.12.3**, a Hugo Module pinned in `go.mod`/`go.sum`                           |
| Site configuration           | `hugo.toml`                                                                             |
| Learning path                | `data/nav.yaml` + `layouts/_partials/sidebar.html`                                      |
| Source-quoting include       | `layouts/_shortcodes/include.html` → `_partials/include/{extract,region,language}.html` |
| Admonitions and collapsibles | `layouts/_shortcodes/{admonition,collapsible}.html` + `assets/css/custom.css`           |
| Mermaid and search bundles   | `assets/js/vendor/`, pinned by version and sha256 in `versions.json`                    |
| Search accessibility         | `assets/js/search-a11y.js`                                                              |
| Syntax migration tool        | `scripts/convert_material.py`                                                           |

Build time for the whole 198,000-word site: **under one second**.

## 3. Rendered-output parity

Both sites were rendered and compared as **text, not HTML**: the visible words inside the content container of all 76 page pairs, tokenized and diffed as sequences.

```text
zensical: 198,294 words
hugo:     198,280 words   (-14)
51 of 76 pages: rendered text identical
25 of 76 pages: at least one difference
```

Every difference was classified. There are **49** in total:

| Category                                            | Count | Verdict                            |
| --------------------------------------------------- | ----- | ---------------------------------- |
| Syntax-highlighter re-tokenization                  | 43    | Not a difference — same characters |
| Include now quotes newer source                     | 2     | Correct behaviour                  |
| Broken ordered-list item now renders as a list item | 2     | **Fix**                            |
| `...` rendered as a typographic ellipsis            | 2     | Cosmetic                           |

### 3.1 Highlighter re-tokenization (43)

Chroma and Pygments split identical code into different token spans — `tool.ruff` becomes `tool` `.` `ruff`, `3000:3000` stays one token instead of three. The characters on the page are byte-identical; only the `<span>` boundaries move. Detected by comparing the concatenation of each differing run, which matches exactly.

### 3.2 Includes quote current source (2)

On `1. Setup/1.1. Python.md` the Zensical build shows `cryptography>=48.0.1,<49` and `GHSA-537c-gmf6-5ccf`; the Hugo build shows `>=50.0.0,<51` and `PYSEC-2026-3552/3553/3554`. `agents/python/pyproject.toml` currently says 50.0.0, so the checked-in Zensical `site/` is stale and the Hugo build is right. This is the include contract working: quoted code tracks real source.

### 3.3 Ordered-list rendering (2 — an improvement)

On `3. Capabilities/3.6. A2A.md` and `7. Observability/7.5. Online Evaluation.md`, a list item following a fenced block indented by three spaces was mis-parsed by Python-Markdown: the marker leaked into the previous paragraph as literal text, so readers saw a stray `1.` mid-sentence. Goldmark closes the previous `<li>` and opens a new one correctly.

### 3.4 Typographic ellipsis (2)

Goldmark's typographer renders `...` as `&mldr;`; Python-Markdown left three periods. Affects `1. Setup/1.5. Workspace.md` and `7. Observability/_index.md`.

### 3.5 Non-text parity

- **All 34 includes** render byte-identical to their source regions (verified programmatically).
- **All 133 Mermaid diagrams** render, and the browser gate now asserts each becomes an SVG with external requests blocked and carries an accessible name.
- **All 76 pages** publish, with one `<h1>`, one `<main>`, and no unnamed links.
- **Dark mode, keyboard navigation, skip link, 404 recovery, search** all pass the browser gate.

## 4. Regressions, as shipped

`PLAN.md` §9 predicted ten. Restated against what actually happened:

1. **No `pymdownx.snippets` equivalent — resolved, and it is now the best-tested part.** `layouts/_partials/include/{extract,region}.html` reproduce named regions, path checking, and subsection dedent, and fail the build on a missing file, a missing region, an ambiguous region, or a path escape. Reading through `assets/source/**` module mounts (not `os.ReadFile`) makes `hugo server` rebuild a page when the code it quotes changes — verified by editing a quoted source file against a running dev server.
1. **319 admonitions and 165 collapsibles rewritten.** Done by script, not by hand. The seven-type Material vocabulary is preserved exactly rather than folded into Hextra's three-type callout, so `check_admonitions` still enforces the same semantics.
1. **Mermaid is not built in — resolved without a CDN.** The bundle is vendored, pinned by sha256, and loaded locally. The theme's light/dark re-initialisation is inherited from Hextra.
1. **Published URLs changed — accepted deliberately.** This build publishes clean directory URLs (`/0-overview/0-0-course/`) instead of `0. Overview/0.0. Course.html`. No aliases were generated: `docs/released-urls.json` and `scripts/docs_routes.py` were **removed**, because a repository that publishes nothing should not assert a URL contract it does not own. If this build is ever promoted to replace the original, that contract must be re-established first — this is the single largest outstanding item.
1. **Search is different.** Hextra's FlexSearch replaces Material's `search.highlight`, `search.share`, and `search.suggest`. Result highlighting and shareable search URLs are gone. Combobox semantics were re-added by `assets/js/search-a11y.js`, which is the direct replacement for the Zensical search shim.
1. **The accessibility surface was re-earned.** `scripts/accessibility_browser.py` is repointed at Hextra's DOM and passes. Two checks got _stronger_: diagrams must actually render to SVG offline, and every diagram must carry an accessible name.
1. **Customization is more expensive.** True. The sidebar, admonitions, collapsibles, include, 404, and social metadata are now Go templates rather than two files in `overrides/`.
1. **`strict: true` has no single switch.** Assembled from `--panicOnWarning`, `refLinksErrorLevel = "ERROR"`, and `check_navigation` over `data/nav.yaml`. All three are required; removing any one silently loosens the build.
1. **Material ecosystem features are gone.** Social-card _generation_, tags, `mike` versioning, instant navigation, and code annotations have no equivalent. The social card itself is still served as a static asset. Content tabs were unused, so that one cost nothing.
1. **The escape hatch is closed.** The syntax has diverged; `mkdocs-material` can no longer build this content. Migrating was a one-way door and it has been walked through.

## 5. Gains, verified

- **Build-time link validation.** `refLinksErrorLevel = "ERROR"` plus 996 `relref` links means a broken internal link fails the build instead of waiting for `lychee` or for a reader.
- **A bespoke script retired.** `scripts/docs_routes.py` and its tests are gone.
- **A mature dependency.** Hugo 0.164.0 replaces Zensical `0.0.52`, removing the standing "alpha: pin exactly and re-verify the generated site on every upgrade" obligation and the pending `mkdocs.yml` → `zensical.toml` migration.
- **Speed.** Sub-second full builds; `check:docs` and `check:accessibility` both got cheaper.
- **Python thinned, not removed.** `zensical` is gone from `pyproject.toml`, whose runtime dependencies are now empty, but `check_conventions.py` and `accessibility_browser.py` still need Python.

## 6. Outstanding — needs an author decision

These are **course prose**, which this migration deliberately did not rewrite. 60 lines across 13 pages still describe the Zensical build and are now factually wrong:

| Page                                                                                           | Lines  | What it says                                                                                                                |
| ---------------------------------------------------------------------------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------- |
| `8. Community/8.4. Documentation.md`                                                           | 23     | Teaches the whole Zensical/`mkdocs.yml`/`pymdownx.snippets`/`released-urls.json` build. Needs rewriting end to end for Hugo |
| `8. Community/8.1. License.md`                                                                 | 9      | Licence boundaries stated in terms of `docs/`, `docs/CNAME`, `docs/stylesheets/`                                            |
| `8. Community/8.0. Repository.md`                                                              | 6      | Repository tree and the `--8<--` snippet explanation                                                                        |
| `1. Setup/1.0. System.md`                                                                      | 4      | "`uv sync` installs Zensical, the static-site generator"                                                                    |
| `1. Setup/1.5. Workspace.md`                                                                   | 4      | `.gitignore` walkthrough naming the Zensical `site/` output                                                                 |
| `4. Quality/4.1. Linting.md`                                                                   | 3      | "runs before Zensical builds"                                                                                               |
| `8. Community/8.5. Contributions.md`                                                           | 3      | CI description and an exercise scoped to `docs/`                                                                            |
| `0. Overview/0.6. Troubleshooting.md`                                                          | 2      | `:8003` described as the Zensical preview                                                                                   |
| `8. Community/8.3. Templates.md`                                                               | 2      | Site identity via `docs/CNAME`                                                                                              |
| `0. Overview/0.3. Ecosystem.md`, `0.5. Resources.md`, `1. Setup/_index.md`, `8.7. Capstone.md` | 1 each | Incidental `docs/` and Zensical references                                                                                  |

Rewriting these changes what the course _teaches_, not how it is _built_, so it was left to the author rather than done as part of a build-system swap.

## 7. Reproducing this

```bash
mise run install      # pinned tools, the Hextra module, and the Python gate environment
mise run serve        # preview on http://127.0.0.1:8003
mise run build:docs   # strict build into site/
mise run check:docs   # authoring contracts + strict build + rendered-route validation
mise run check:accessibility
```

`scripts/convert_material.py` is kept rather than discarded: if the source repository advances, re-running it over a fresh copy of `docs/` reproduces this conversion exactly.
