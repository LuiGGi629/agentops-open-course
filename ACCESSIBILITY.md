# Accessibility

The AgentOps Open Course should be usable without a mouse, without color perception, and without relying on a diagram renderer. Accessibility defects are course defects.

## Current support

- The rendered site uses semantic headings, labeled navigation and search, visible keyboard focus from the Material theme, and a skip-to-content link.
- Commands, expected output, warnings, and completion criteria are written as text. Color is never the only intended signal.
- Contributor policy requires every new or changed Mermaid diagram to have adjacent prose that communicates the same actors, relationships, and sequence.
- Unfamiliar terms are defined at first use and linked to the course glossary on dense pages.

## Known limits

The site has not completed a formal WCAG conformance audit. The default indigo theme supplies the current color palette, but a theme or custom-style change still requires a manual contrast and keyboard check. Mermaid support also varies across screen readers, so new and changed diagrams must not carry unique information.

Some existing diagrams still carry details that adjacent prose does not repeat. [Issue #68](https://github.com/MLOps-Courses/agentops-open-course/issues/68) tracks the remaining retrofits; until it closes, the no-renderer experience is incomplete.

PDF and offline ebook formats are not currently published. The repository Markdown remains the text-first fallback when the hosted interface creates a barrier.

## How to report a barrier

[Open an accessibility issue](https://github.com/MLOps-Courses/agentops-open-course/issues/new) with:

- the page URL or source path;
- the browser, operating system, and assistive technology involved;
- the action you attempted and what blocked it;
- a suggested correction, when you have one.

Do not include private or security-sensitive data. Report a vulnerability through [SECURITY.md](./SECURITY.md) instead of a public issue.
