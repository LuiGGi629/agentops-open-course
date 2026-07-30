# Accessibility

The AgentOps Open Course should be usable without a mouse, without color perception, and without relying on a diagram renderer. Accessibility defects are course defects.

## Current support

- The rendered site uses semantic headings, labeled navigation and search, visible keyboard focus from the Material theme, and a skip-to-content link.
- The dependency-free web client labels its endpoint, message, approval rationale, and cancellation controls; streaming and terminal task states use a polite live region.
- Commands, expected output, warnings, and completion criteria are written as text. Color is never the only intended signal.
- Contributor policy requires every new or changed Mermaid diagram to have adjacent prose that communicates the same actors, relationships, and sequence.
- Unfamiliar terms are defined at first use and linked to the course glossary on dense pages.

## What was audited, and when?

On 30 July 2026, commit `5c8e083` was audited on Debian 12 x86_64 with Chrome 150 and Lighthouse 13.4.1. The rendered home page, A2A, security, and capstone pages plus the standalone web client each scored 100 in Lighthouse's accessibility category after every reported finding was corrected.

The audit also reviewed keyboard reachability, visible focus, skip navigation, search semantics, code-copy controls, task streaming, approval rationale, cancellation, 200% zoom/reflow, narrow mobile layout, reduced motion, forced-colors behavior, landmarks, table headers, form labels, status announcements, and the Markdown alternatives adjacent to diagrams. It is a WCAG-oriented product audit, not a formal conformance certification.

## Known limits

Chrome on Linux is the only release-gated browser and screen-reader accessibility tree. Firefox, Safari, VoiceOver, NVDA, and Orca remain best-effort because the project does not have a repeatable test environment for those combinations. Report barriers with the exact combination so the support matrix can grow from evidence.

The default indigo theme supplies the current color palette, but a theme or custom-style change still requires another contrast and keyboard audit. Mermaid support varies across screen readers, so diagrams never carry unique information.

PDF and offline ebook formats are not currently published. The repository Markdown remains the text-first fallback when the hosted interface creates a barrier.

## How to report a barrier

[Open an accessibility issue](https://github.com/MLOps-Courses/agentops-open-course/issues/new) with:

- the page URL or source path;
- the browser, operating system, and assistive technology involved;
- the action you attempted and what blocked it;
- a suggested correction, when you have one.

Do not include private or security-sensitive data. Report a vulnerability through [SECURITY.md](./SECURITY.md) instead of a public issue.
