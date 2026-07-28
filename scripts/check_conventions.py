"""Validate the repository's authoring conventions.

One checker for the three text-shaped gates: course pages, installable skills, and release
metadata. They were three shell scripts plus a Python helper; the helper existed because a regex
cannot safely parse YAML front matter, which is the whole argument for doing this in Python. Rules
here are about *text conventions*, not about running processes — anything that starts a container,
binds a port, or probes PATH stays in shell.

Usage:

    uv run python scripts/check_conventions.py docs
    uv run python scripts/check_conventions.py skills
    uv run python scripts/check_conventions.py release-metadata [expected-tag]

Every rule prints ``path: what is wrong`` on stderr and the command exits non-zero, so a failure
names the file and the fix without needing the source.
"""

import pathlib
import re
import sys
from collections.abc import Iterator
from typing import Final

ROOT: Final = pathlib.Path(__file__).resolve().parent.parent

# --- course pages -------------------------------------------------------------------------------

# The page frame. Every page opens with this block so a learner can decide in ten seconds whether
# to read it now, skim it, or come back later.
GLANCE_OPEN: Final = '!!! abstract "In one glance"'
GLANCE_FIELDS: Final = ("**You will:**", "**You need:**", "**Time:**")

# One closing landmark, spelled the same way everywhere, so "am I done?" is answered in the same
# place on every page. `docs/index.md` is the site landing page and carries neither frame rule.
CLOSING_HEADINGS: Final = (
    "## What proves this page worked?",
    "## What proves this chapter worked?",
    "## How should you use this page later?",
)
LANDING_PAGE: Final = "docs/index.md"

# The kind word in the Time line, which the chapter index repeats next to the page link.
KINDS: Final = ("concept", "hands-on", "reference", "orientation", "lookup")

# Depth a first-time reader can skip lives behind a collapsible. Three is the
# documented ceiling; more turns the page into a wall of triangles.
MAX_COLLAPSIBLES: Final = 3

FRONT_MATTER: Final = re.compile(r"\A---\n(.*?)\n---\n", re.DOTALL)
MACHINE_PATH: Final = re.compile(r"/home/[^ /]+|file:///|k3d-registry\.localhost")
COLLAPSIBLE: Final = re.compile(r'^\s*\?\?\? \w+ "([^"]*)"')
BARE_NUMBER_LABEL: Final = re.compile(r"\[(\d+(?:\.\d+)*)\]\(")
INDEX_KIND_LABEL: Final = re.compile(r"\[(\d+\.\d+\.[^\]]+)\][^\n]*?_\(([a-z-]+)[^)]*\)_")
TIME_LINE: Final = re.compile(r"^ {4}- \*\*Time:\*\* (.+)$", re.MULTILINE)

Problem = tuple[str, str]


def outside_fences(text: str) -> Iterator[tuple[int, str]]:
    """Yield (line number, line) for every line that is not inside a fenced code block."""
    fenced = False
    for number, line in enumerate(text.splitlines(), start=1):
        if line.lstrip().startswith(("```", "~~~")):
            fenced = not fenced
            continue
        if not fenced:
            yield number, line


def declared_kind(text: str) -> str | None:
    """Return the single page-kind word in the Time line, or None when it is absent or ambiguous."""
    match = TIME_LINE.search(text)
    if match is None:
        return None
    found = [kind for kind in KINDS if re.search(rf"\b{re.escape(kind)}\b", match.group(1))]
    return found[0] if len(found) == 1 else None


def parse_front_matter(text: str) -> tuple[dict[str, object] | None, str | None]:
    """Return (mapping, error) for a file's YAML front matter.

    PyYAML is imported here rather than at module scope so the release-metadata check stays
    dependency-free: it runs in a CI job that has only a checkout and a system Python.
    """
    import yaml

    match = FRONT_MATTER.match(text)
    if match is None:
        return None, "expected YAML front matter at the start of the file"
    try:
        meta = yaml.safe_load(match.group(1))
    except yaml.YAMLError as error:
        return None, f"front matter is not valid YAML ({str(error).splitlines()[0]}); quote values containing ': '"
    if not isinstance(meta, dict):
        return None, "front matter must be a YAML mapping"
    return meta, None


def check_front_matter(page: pathlib.Path, text: str) -> list[Problem]:
    """Front matter must be real YAML with a non-empty description.

    An unquoted ``key: value`` containing a colon-space looks fine but is invalid YAML. The
    renderer then leaves the block unparsed, Markdown reads ``text`` + ``---`` as a setext heading,
    and the description is published as the page's <h2>. Only a parser catches that.
    """
    name = page.as_posix()
    meta, error = parse_front_matter(text)
    if meta is None:
        return [(name, error or "unreadable front matter")]
    description = meta.get("description")
    if not isinstance(description, str) or not description.strip():
        return [(name, "front matter must define a non-empty description")]
    return []


def check_headings(page: pathlib.Path, text: str) -> list[Problem]:
    """Every page is an FAQ: at least one H2, and every H2 phrased as a question."""
    name = page.as_posix()
    problems = [
        (name, f"FAQ heading must end with ?: {line}")
        for _, line in outside_fences(text)
        if line.startswith("## ") and not line.endswith("?")
    ]
    if not any(line.startswith("## ") for _, line in outside_fences(text)):
        problems.append((name, "expected at least one FAQ question heading"))
    return problems


def check_glance(page: pathlib.Path, text: str) -> list[Problem]:
    """The orientation block sits between the H1 and the first H2, with all three fields."""
    name = page.as_posix()
    inside = False
    seen: set[str] = set()
    for _, line in outside_fences(text):
        if line.startswith("## "):
            break
        if line == GLANCE_OPEN:
            inside = True
            continue
        if inside:
            seen.update(field for field in GLANCE_FIELDS if line.startswith(f"    - {field}"))
    if not inside or len(seen) != len(GLANCE_FIELDS):
        missing = ", ".join(field for field in GLANCE_FIELDS if field not in seen)
        return [
            (name, f'expected an "In one glance" abstract block between the H1 and the first H2 (missing: {missing})')
        ]
    return []


def check_closing(page: pathlib.Path, text: str) -> list[Problem]:
    """The last H2 is the page's exit, spelled one of three fixed ways."""
    name = page.as_posix()
    headings = [line for _, line in outside_fences(text) if line.startswith("## ")]
    if headings and headings[-1] not in CLOSING_HEADINGS:
        allowed = " | ".join(f'"{heading.removeprefix("## ")}"' for heading in CLOSING_HEADINGS)
        return [(name, f"last H2 must be one of {allowed}, not: {headings[-1]}")]
    return []


def check_kind(page: pathlib.Path, text: str) -> list[Problem]:
    """The Time line names exactly one page kind, so the chapter index can repeat it."""
    if declared_kind(text) is not None:
        return []
    return [(page.as_posix(), f"the Time line must name exactly one page kind: {', '.join(KINDS)}")]


def check_collapsibles(page: pathlib.Path, text: str) -> list[Problem]:
    """Collapsibles are titled consistently and stay rare enough to read past."""
    name = page.as_posix()
    summaries = [match.group(1) for line in text.splitlines() if (match := COLLAPSIBLE.match(line))]
    problems = [
        (name, f'collapsible summary must start with "Deeper: ": {summary}')
        for summary in summaries
        if not summary.startswith("Deeper: ")
    ]
    if len(summaries) > MAX_COLLAPSIBLES:
        problems.append((name, f"{len(summaries)} collapsibles; at most {MAX_COLLAPSIBLES} keep a page readable"))
    return problems


def check_link_labels(page: pathlib.Path, text: str) -> list[Problem]:
    """A link label is a page name. The audience is exactly the reader who does not know what 5.2 is."""
    return [
        (page.as_posix(), f"link label must be the page name, not a bare number: [{label}]")
        for label in BARE_NUMBER_LABEL.findall(text)
    ]


def check_snippets(page: pathlib.Path, text: str) -> list[Problem]:
    """A snippet include belongs inside a fence.

    Outside one it is rendered as Markdown, so a leading ``#`` comment in the included region
    becomes an <h1> on the published page.
    """
    return [
        (page.as_posix(), f"line {number}: snippet include must sit inside a fenced code block")
        for number, line in outside_fences(text)
        if line.strip().startswith("--8<--")
    ]


def check_machine_paths(page: pathlib.Path, text: str) -> list[Problem]:
    """No machine-specific path, local file URL, or obsolete registry hostname."""
    return [
        (page.as_posix(), f"line {number}: found a machine-specific path or obsolete registry hostname")
        for number, line in enumerate(text.splitlines(), start=1)
        if MACHINE_PATH.search(line)
    ]


def check_index_kinds(pages: dict[pathlib.Path, str]) -> list[Problem]:
    """A chapter index labels each sub-page with the kind that page declares for itself."""
    kinds = {page: declared_kind(text) for page, text in pages.items()}
    problems: list[Problem] = []
    for index, text in pages.items():
        if index.name != "index.md":
            continue
        for label, labelled in INDEX_KIND_LABEL.findall(text):
            target = index.parent / f"{label}.md"
            declared = kinds.get(target)
            if labelled in KINDS and declared is not None and labelled != declared:
                problems.append(
                    (index.as_posix(), f"labels {label} as ({labelled}) but that page declares ({declared})")
                )
    return problems


def check_docs() -> list[Problem]:
    """Run every page rule over docs/."""
    pages = {page: page.read_text(encoding="utf-8") for page in sorted(ROOT.joinpath("docs").rglob("*.md"))}
    if not pages:
        return [("docs", "no Markdown pages found")]
    problems: list[Problem] = []
    for page, text in pages.items():
        relative = page.relative_to(ROOT)
        problems += check_front_matter(relative, text)
        problems += check_headings(relative, text)
        problems += check_glance(relative, text)
        problems += check_collapsibles(relative, text)
        problems += check_link_labels(relative, text)
        problems += check_snippets(relative, text)
        problems += check_machine_paths(relative, text)
        if relative.as_posix() != LANDING_PAGE:
            problems += check_closing(relative, text)
            problems += check_kind(relative, text)
    problems += [
        (page.relative_to(ROOT).as_posix(), message)
        for page, message in [(ROOT / path, message) for path, message in check_index_kinds(pages)]
    ]
    return problems


# --- installable skills -------------------------------------------------------------------------


def check_skills() -> list[Problem]:
    """Every skill under skills/ is portable and resolvable by `npx skills add --skill <dir>`."""
    files = sorted(ROOT.joinpath("skills").glob("*/SKILL.md"))
    if not files:
        return [("skills", "no SKILL.md found under skills/")]
    problems: list[Problem] = []
    for skill in files:
        name = skill.relative_to(ROOT).as_posix()
        text = skill.read_text(encoding="utf-8")
        meta, error = parse_front_matter(text)
        if meta is None:
            problems.append((name, error or "must start with YAML front matter (---)"))
            continue
        declared, directory = meta.get("name"), skill.parent.name
        if not declared:
            problems.append((name, "front matter is missing a name"))
        elif declared != directory:
            problems.append((name, f"name {declared!r} must match its directory {directory!r}"))
        if not meta.get("description"):
            problems.append((name, "front matter is missing a description"))
        if not any(line.startswith("# ") for line in text.splitlines()):
            problems.append((name, "expected an H1 title"))
        if MACHINE_PATH.search(text):
            problems.append((name, "found a machine-specific path (skills must be portable)"))
    return problems


# --- release metadata ---------------------------------------------------------------------------

SEMVER: Final = re.compile(r"^\d+\.\d+\.\d+$")
ISO_DATE: Final = re.compile(r"^\d{4}-\d{2}-\d{2}$")
RELEASE_HEADING: Final = re.compile(r"^## \[\d+\.\d+\.\d+\] - ", re.MULTILINE)


def project_version(manifest: pathlib.Path) -> str | None:
    """Return the [project] version of a pyproject.toml, without importing a TOML parser."""
    in_project = False
    for line in manifest.read_text(encoding="utf-8").splitlines():
        if line.strip() == "[project]":
            in_project = True
        elif in_project and line.startswith("["):
            break
        elif in_project and (match := re.fullmatch(r'version = "([^"]+)"', line.strip())):
            return match.group(1)
    return None


def citation_value(key: str) -> str | None:
    """Return a top-level scalar from CITATION.cff, without importing a YAML parser.

    This check runs in the release workflow, which has a checkout and nothing else, so it must not
    depend on the project's virtualenv.
    """
    for line in ROOT.joinpath("CITATION.cff").read_text(encoding="utf-8").splitlines():
        if line.startswith(f"{key}:"):
            return line.removeprefix(f"{key}:").strip().strip('"').strip("'")
    return None


def check_release_metadata(expected_tag: str = "") -> list[Problem]:
    """Package, citation, changelog, and optional tag versions must agree."""
    values = {
        "root version": project_version(ROOT / "pyproject.toml"),
        "agent version": project_version(ROOT / "agents/python/pyproject.toml"),
        "citation version": citation_value("version"),
        "citation date": citation_value("date-released"),
    }
    missing = [key for key, value in values.items() if not value]
    if missing:
        return [("release metadata", f"could not read {', '.join(missing)}")]

    root, agent, cited, dated = (
        values["root version"],
        values["agent version"],
        values["citation version"],
        values["citation date"],
    )
    problems: list[Problem] = []
    if not SEMVER.match(root or ""):
        return [("release metadata", f"root version is not stable SemVer: {root}")]
    if agent != root or cited != root:
        problems.append(("release metadata", f"version mismatch (root={root} agent={agent} citation={cited})"))
    if not ISO_DATE.match(dated or ""):
        problems.append(("release metadata", f"CITATION.cff date-released is not YYYY-MM-DD: {dated}"))

    changelog = ROOT.joinpath("CHANGELOG.md").read_text(encoding="utf-8")
    expected_heading = f"## [{root}] - {dated}"
    newest = next((line for line in changelog.splitlines() if RELEASE_HEADING.match(line)), "")
    if newest != expected_heading:
        problems.append(("release metadata", f"newest changelog heading must be {expected_heading!r}, got {newest!r}"))
    expected_link = f"[{root}]: https://github.com/MLOps-Courses/agentops-open-course/releases/tag/v{root}"
    if expected_link not in changelog.splitlines():
        problems.append(("release metadata", f"CHANGELOG.md is missing {expected_link}"))
    if expected_tag and expected_tag != f"v{root}":
        problems.append(("release metadata", f"tag {expected_tag} does not match source version v{root}"))

    if not problems:
        sys.stdout.write(f"release metadata: v{root} ({dated}) is consistent\n")
    return problems


# --- entry point --------------------------------------------------------------------------------


def main(argv: list[str]) -> int:
    """Run one named check and report every problem it found."""
    check = argv[1] if len(argv) > 1 else ""
    if check == "docs":
        problems = check_docs()
    elif check == "skills":
        problems = check_skills()
    elif check == "release-metadata":
        problems = check_release_metadata(argv[2] if len(argv) > 2 else "")
    else:
        sys.stderr.write("usage: check_conventions.py <docs|skills|release-metadata> [expected-tag]\n")
        return 2
    for where, message in problems:
        sys.stderr.write(f"{where}: {message}\n")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
