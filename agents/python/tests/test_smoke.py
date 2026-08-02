"""Smoke test: the reference agent is importable and defined."""

import hashlib

from google.adk import Agent

from agent import composition
from agent.composition import app, root_agent
from agent.config import AgentEntrypoint, settings
from agent.delegation import coordinator_agent
from agent.governance import AgentOpsPolicyPlugin, build_app
from agent.workflow import triage_workflow


def test_root_agent_defined() -> None:
    assert isinstance(root_agent, Agent)
    assert root_agent.name == "agentops_agent"
    assert root_agent.model


def test_app_factory_attaches_exactly_one_policy_plugin() -> None:
    candidate = build_app(root_agent)
    assert candidate.root_agent is root_agent
    assert len(candidate.plugins) == 1
    assert isinstance(candidate.plugins[0], AgentOpsPolicyPlugin)
    assert app.root_agent is root_agent
    assert len(app.plugins) == 1
    assert isinstance(app.plugins[0], AgentOpsPolicyPlugin)


def test_composition_selector_exposes_each_validated_entrypoint(monkeypatch) -> None:
    calls = 0

    def build_default():
        nonlocal calls
        calls += 1
        return root_agent

    monkeypatch.setattr(composition, "build_conversational_agent", build_default)
    monkeypatch.setattr(settings, "entrypoint", AgentEntrypoint.WORKFLOW)
    assert composition._select_root_agent() is triage_workflow  # noqa: SLF001 - composition contract
    monkeypatch.setattr(settings, "entrypoint", AgentEntrypoint.COORDINATOR)
    assert composition._select_root_agent() is coordinator_agent  # noqa: SLF001 - composition contract
    assert calls == 0
    monkeypatch.setattr(settings, "entrypoint", AgentEntrypoint.AGENT)
    assert composition._select_root_agent() is root_agent  # noqa: SLF001 - composition contract
    assert calls == 1


def test_root_instruction_prompt_contract() -> None:
    assert isinstance(root_agent, Agent)
    instruction = str(root_agent.instruction)
    digest = hashlib.sha256(instruction.encode()).hexdigest()
    assert digest == "7f2f16e2c2a31e92891ea89dcaefc2427a32f729af4ecc68f7c8a179c29f6c75", (
        "prompt changed — re-run `mise run eval` and update the hash after reviewing"
    )
    # The strict evalset depends on these ordering and confirmation semantics,
    # so keep them legible instead of hiding every requirement behind the hash.
    assert "call `recall_incident_context` and wait before" in instruction
    assert instruction.index("call `recall_incident_context`") < instruction.index("call `get_incident`")
    assert "then `load_skill`" in instruction
    assert "your next model output must be the matching guarded tool call" in instruction
    assert "re-read the incident and affected service" in instruction


def test_instruction_defaults_to_the_committed_text(monkeypatch) -> None:
    from agent import agent as agent_module

    monkeypatch.setattr(agent_module.settings, "prompt_uri", None)
    assert agent_module._instruction() == agent_module.INSTRUCTION  # noqa: SLF001


def test_instruction_loads_a_pinned_registry_version(monkeypatch) -> None:
    import sys
    from types import SimpleNamespace

    from agent import agent as agent_module

    loaded: list[str] = []

    def fake_load_prompt(uri: str):
        loaded.append(uri)
        return SimpleNamespace(template="registry instruction v2")

    fake_genai = SimpleNamespace(load_prompt=fake_load_prompt)
    monkeypatch.setitem(sys.modules, "mlflow", SimpleNamespace(genai=fake_genai))
    monkeypatch.setitem(sys.modules, "mlflow.genai", fake_genai)
    monkeypatch.setattr(agent_module.settings, "prompt_uri", "prompts:/agentops-agent-instruction/2")
    assert agent_module._instruction() == "registry instruction v2"  # noqa: SLF001
    assert loaded == ["prompts:/agentops-agent-instruction/2"]
