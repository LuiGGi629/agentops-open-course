"""Offline tests for shared model-backed evaluation safety boundaries."""

import pytest

from evals import runtime


def test_runtime_identity_rejects_fallbacks_and_mutable_prompt_aliases(monkeypatch) -> None:
    monkeypatch.setattr(runtime.settings, "model_fallback", "qwen3:1.7b")
    with pytest.raises(SystemExit, match="evaluate each model separately"):
        runtime.require_attributable_runtime()

    monkeypatch.setattr(runtime.settings, "model_fallback", None)
    monkeypatch.setattr(runtime.settings, "prompt_uri", "prompts:/agentops-agent-instruction@latest")
    with pytest.raises(SystemExit, match="immutable numeric prompt version"):
        runtime.require_attributable_runtime()

    monkeypatch.setattr(runtime.settings, "prompt_uri", "prompts:/agentops-agent-instruction/3")
    runtime.require_attributable_runtime()


def test_isolated_state_restores_and_removes_the_temporary_directory(monkeypatch, tmp_path) -> None:
    original = tmp_path / "original"
    monkeypatch.setattr(runtime.settings, "state_dir", original)

    with runtime.isolated_state("agentops-test-") as isolated:
        assert runtime.settings.state_dir == isolated
        assert isolated.is_dir()

    assert runtime.settings.state_dir == original
    assert not isolated.exists()
