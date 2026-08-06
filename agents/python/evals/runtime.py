"""Shared isolation and attribution guards for model-backed evaluations."""

from __future__ import annotations

import re
import tempfile
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from agent.config import settings


def require_single_model() -> None:
    """Reject fallback routing so every observed answer has one attributable model."""
    if settings.model_fallback:
        raise SystemExit(
            "AGENT_MODEL_FALLBACK makes evaluation attribution ambiguous. "
            "Unset it and evaluate each model separately with AGENT_MODEL."
        )


def immutable_prompt_uri(prompt_uri: str | None) -> str | None:
    """Return an immutable numeric prompt URI or fail before model work starts."""
    if prompt_uri is None:
        return None
    if not re.fullmatch(r"prompts:/[^/@]+/[1-9]\d*", prompt_uri):
        raise SystemExit(
            "AGENT_PROMPT_URI must select an immutable numeric prompt version "
            "(for example prompts:/agentops-agent-instruction/3) during evaluation."
        )
    return prompt_uri


def require_attributable_runtime() -> None:
    """Reject routing that could change model or prompt identity mid-run."""
    require_single_model()
    immutable_prompt_uri(settings.prompt_uri)


@contextmanager
def isolated_state(prefix: str) -> Iterator[Path]:
    """Give one evaluation a temporary writable state boundary, then restore it."""
    original_state_dir = settings.state_dir
    with tempfile.TemporaryDirectory(prefix=prefix) as state_dir:
        isolated_dir = Path(state_dir)
        settings.state_dir = isolated_dir
        try:
            yield isolated_dir
        finally:
            settings.state_dir = original_state_dir
