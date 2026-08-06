#!/usr/bin/env python3
"""Rewrite the course Markdown from the Material/Zensical dialect into the Hugo one.

This is a migration tool, not a one-off: 319 admonitions, 165 collapsibles, 38 snippet
includes, and 1001 internal links across 76 pages are far too many to hand-edit reliably,
and keeping the transformation in a script makes it auditable and re-runnable if the source
repository advances before the migration lands.

It changes **syntax only**. Prose, headings, ordering, and code are never rewritten. The one
structural change is the H1: Hextra renders ``.Title`` as the page heading, so leaving the
Markdown H1 in the body would publish two <h1> elements on every page. The H1 text moves
verbatim into ``title`` front matter.

Run it from the repository root::

    python3 scripts/convert_material.py            # convert content/ in place
    python3 scripts/convert_material.py --check    # report what would change, write nothing

Re-running is a no-op: every rule matches Material syntax that the first run removed.
"""

from __future__ import annotations

import argparse
import pathlib
import re
import sys
from typing import Final
from urllib.parse import unquote

ROOT: Final = pathlib.Path(__file__).resolve().parent.parent
CONTENT: Final = ROOT / "content"

FRONT_MATTER: Final = re.compile(r"\A---\n(.*?\n)---\n", re.DOTALL)
FENCE: Final = re.compile(r"^(\s*)(`{3,}|~{3,})\s*(\S*)")
ADMONITION: Final = re.compile(r'^(!!!|\?\?\?)\s+([a-z-]+)(?:\s+"([^"]*)")?\s*$')
SNIPPET: Final = re.compile(r'^--8<--\s+"([^":]+):([^"]+)"\s*$')
MARKDOWN_LINK: Final = re.compile(r"\]\(([^)\s]+?)\)")

# Material indents an admonition body by exactly four spaces. Goldmark would read that same
# indentation as a code block, so the body is dedented by exactly four — never by its own
# minimum indent, which would also flatten deliberately nested code inside the body.
BODY_INDENT: Final = 4


def slugify(name: str) -> str:
    """Return the URL segment for one chapter directory or page file name.

    ``0.7. Glossary`` becomes ``0-7-glossary``. Hugo's own urlize keeps the dots and would
    publish ``0.7.-glossary``, so every page declares an explicit ``url`` instead.
    """
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")


def page_url(page: pathlib.Path) -> str:
    """Return the published URL for one content file."""
    relative = page.relative_to(CONTENT)
    chapter = f"{slugify(relative.parent.name)}/" if relative.parent != pathlib.Path(".") else ""
    if page.name == "_index.md":
        return f"/{chapter}" if chapter else "/"
    return f"/{chapter}{slugify(page.stem)}/"


def fence_state(lines: list[str]) -> list[bool]:
    """Return, per line, whether that line sits inside a fenced code block.

    Every rule below is fence-aware: a ``!!!`` or a ``# heading`` inside a shell or Python
    block is content the course deliberately shows, not markup to convert.
    """
    inside: list[bool] = []
    marker: str | None = None
    for line in lines:
        match = FENCE.match(line)
        if marker is None and match is not None:
            inside.append(False)
            marker = match.group(2)[0] * 3
            continue
        if marker is not None and match is not None and match.group(2).startswith(marker) and not match.group(3):
            inside.append(True)
            marker = None
            continue
        inside.append(marker is not None)
    return inside


def split_front_matter(text: str) -> tuple[str, str]:
    """Return (front matter block including delimiters, body)."""
    match = FRONT_MATTER.match(text)
    if match is None:
        raise ValueError("expected YAML front matter at the start of the file")
    return match.group(0), text[match.end() :]


def take_title(body: str) -> tuple[str, str]:
    """Lift the H1 out of the body and return (title, body without it)."""
    lines = body.splitlines()
    inside = fence_state(lines)
    for index, line in enumerate(lines):
        if inside[index] or not line.startswith("# "):
            continue
        title = line.removeprefix("# ").strip()
        rest = lines[index + 1 :]
        while rest and not rest[0].strip():
            rest.pop(0)
        return title, "\n".join(lines[:index] + rest)
    raise ValueError("expected a single H1 heading")


def convert_admonitions(body: str) -> str:
    """Rewrite ``!!!``/``???`` blocks as `admonition`/`collapsible` shortcodes.

    The seven-type Material vocabulary is preserved exactly. Hextra's own callout offers
    only three types, and collapsing seven semantic kinds into three would quietly weaken
    ``check_admonitions``, which exists to keep that vocabulary small *and* meaningful.

    The ``{{%`` delimiter is required, not stylistic: the bodies contain lists, links, tables,
    fences, and nested includes, and only ``{{% %}}`` renders shortcode inner content as
    Markdown.
    """
    lines = body.splitlines()
    inside = fence_state(lines)
    output: list[str] = []
    index = 0
    while index < len(lines):
        match = None if inside[index] else ADMONITION.match(lines[index])
        if match is None:
            output.append(lines[index])
            index += 1
            continue

        marker, kind, title = match.group(1), match.group(2), match.group(3) or ""
        shortcode = "admonition" if marker == "!!!" else "collapsible"
        index += 1

        # Collect the indented body: blank lines belong to it only when an indented line
        # follows, otherwise a trailing blank would swallow the paragraph after the block.
        block: list[str] = []
        pending: list[str] = []
        while index < len(lines):
            line = lines[index]
            if not line.strip():
                pending.append("")
                index += 1
                continue
            if not line.startswith(" " * BODY_INDENT):
                break
            block.extend(pending)
            pending = []
            block.append(line[BODY_INDENT:])
            index += 1

        opener = f'{{{{% {shortcode} {kind} "{title}" %}}}}' if title else f"{{{{% {shortcode} {kind} %}}}}"
        output.append(opener)
        output.extend(block)
        output.append(f"{{{{% /{shortcode} %}}}}")
        output.extend(pending)
    return "\n".join(output)


def convert_includes(body: str) -> str:
    """Rewrite fenced ``--8<--`` snippet includes as the `include` shortcode.

    The include always occupies a fence on its own, because ``check_snippets`` requires it:
    outside a fence, a leading ``#`` comment in the quoted region would render as an <h1>.
    The fence's language is carried through as ``lang`` so the Chroma lexer stays the one the
    author chose rather than one inferred from the file extension.
    """
    lines = body.splitlines()
    output: list[str] = []
    index = 0
    while index < len(lines):
        opening = FENCE.match(lines[index])
        snippet = SNIPPET.match(lines[index + 1].strip()) if opening and index + 2 < len(lines) else None
        closing = FENCE.match(lines[index + 2]) if snippet else None
        if opening and snippet and closing and not closing.group(3):
            path, region = snippet.group(1), snippet.group(2)
            language = opening.group(3)
            attribute = f' lang="{language}"' if language else ""
            output.append(f'{{{{< include "{path}" "{region}"{attribute} >}}}}')
            index += 3
            continue
        output.append(lines[index])
        index += 1
    return "\n".join(output)


def resolve_link(page: pathlib.Path, target: str) -> str | None:
    """Return the content-root path a relative Markdown link points at, or None.

    Only links to course pages are rewritten. External URLs, in-page anchors, and links to
    non-page files (the licence, the social card) are left exactly as the author wrote them.
    """
    if target.startswith(("http://", "https://", "mailto:", "#", "/")):
        return None
    path, _, anchor = unquote(target).partition("#")
    if not path:
        return None

    resolved = (page.parent / path).resolve()
    if resolved.is_dir():
        resolved = resolved / "_index.md"
    if resolved.suffix != ".md" or not resolved.is_file():
        return None
    try:
        relative = resolved.relative_to(CONTENT)
    except ValueError:
        return None
    reference = f"/{relative.as_posix()}"
    return f"{reference}#{anchor}" if anchor else reference


def convert_links(page: pathlib.Path, body: str) -> tuple[str, list[str]]:
    """Rewrite internal page links as `relref`, and report the ones that did not resolve.

    ``relref`` is what buys the build-time link validation MkDocs never had: with
    ``refLinksErrorLevel = "ERROR"`` a link to a page that no longer exists fails the build
    instead of waiting for `lychee`, or for a reader.
    """
    lines = body.splitlines()
    inside = fence_state(lines)
    unresolved: list[str] = []

    def rewrite(match: re.Match[str]) -> str:
        target = match.group(1)
        reference = resolve_link(page, target)
        if reference is None:
            if target.endswith(".md") or ".md#" in target:
                unresolved.append(target)
            return match.group(0)
        return f']({{{{< relref "{reference}" >}}}})'

    converted = [line if inside[index] else MARKDOWN_LINK.sub(rewrite, line) for index, line in enumerate(lines)]
    return "\n".join(converted), unresolved


def convert(page: pathlib.Path) -> tuple[str, list[str]]:
    """Return the converted text for one page and any links that could not be resolved."""
    original = page.read_text(encoding="utf-8")
    front, body = split_front_matter(original)
    title, body = take_title(body)
    body = convert_admonitions(body)
    body = convert_includes(body)
    body, unresolved = convert_links(page, body)

    # Inserted around the existing block rather than re-serialized, so the hand-written
    # `description` keeps its exact quoting; check_front_matter parses it as real YAML.
    inner = front[len("---\n") : -len("---\n")]
    front = f'---\ntitle: "{title}"\n{inner}url: "{page_url(page)}"\n---\n'
    return front + body.rstrip("\n") + "\n", unresolved


def main(argv: list[str] | None = None) -> int:
    """Convert every page under content/, or report what conversion would change."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="report changes without writing")
    arguments = parser.parse_args(argv)

    changed, problems = 0, 0
    for page in sorted(CONTENT.rglob("*.md")):
        try:
            converted, unresolved = convert(page)
        except ValueError as error:
            sys.stderr.write(f"{page.relative_to(ROOT)}: {error}\n")
            problems += 1
            continue
        for target in unresolved:
            sys.stderr.write(f"{page.relative_to(ROOT)}: unresolved page link: {target}\n")
            problems += 1
        if converted != page.read_text(encoding="utf-8"):
            changed += 1
            if not arguments.check:
                page.write_text(converted, encoding="utf-8")

    verb = "would change" if arguments.check else "converted"
    sys.stdout.write(f"convert_material: {verb} {changed} page(s), {problems} problem(s)\n")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
