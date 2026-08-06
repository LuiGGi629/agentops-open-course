"""Unit tests for Agent Skills (Ch. 3.2)."""

import asyncio

from google.adk.skills import list_skills_in_dir

from agent import skills


def test_skills_are_discovered() -> None:
    found = list_skills_in_dir(skills.skills_dir())
    assert {"incident-triage", "remediation"} <= set(found)


def test_skill_toolset_builds() -> None:
    toolset = skills.skill_toolset()
    registered = asyncio.run(toolset.get_tools())
    assert {tool.name for tool in registered} == {"list_skills", "load_skill"}


def test_remediation_skill_uses_the_runtime_confirmation_boundary() -> None:
    body = (skills.skills_dir() / "remediation" / "SKILL.md").read_text(encoding="utf-8")
    assert "then call the guarded tool" in body
    assert "ADK pauses before execution" in body
    assert "initiating message is not approval" in body
