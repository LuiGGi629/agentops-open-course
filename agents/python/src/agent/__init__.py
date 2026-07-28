"""The AgentOps Open Course reference package.

Pure data and MCP modules import without initializing ADK. CLI discovery loads
the composition only when it asks this package for ``root_agent``.
"""

from __future__ import annotations

from importlib import import_module
from types import ModuleType
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from google.adk.agents.base_agent import BaseAgent
    from google.adk.workflow import Workflow

    from . import composition as agent

    root_agent: BaseAgent | Workflow

__all__ = ["agent", "root_agent"]


def __getattr__(name: str) -> Any:
    """Load the selected ADK composition only when discovery requests it."""
    if name not in __all__:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    module: ModuleType = import_module(f"{__name__}.composition")
    globals()["agent"] = module
    globals()["root_agent"] = module.root_agent
    return globals()[name]


def __dir__() -> list[str]:
    """Advertise the lazy public attribute to discovery and interactive tools."""
    return sorted({*globals(), *__all__})
