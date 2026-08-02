"""Expose the structured report through ADK's governed ``App`` contract."""

from agent.governance import build_app
from agent.report import triage_report_agent as root_agent

app = build_app(root_agent)

__all__ = ["app", "root_agent"]
