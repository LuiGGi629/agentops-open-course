"""Unit tests for token budgeting and per-session cost attribution."""

from concurrent.futures import ThreadPoolExecutor
from threading import Event, Lock
from typing import Any, cast

import pytest
from google.adk.agents.callback_context import CallbackContext
from google.adk.models.llm_request import LlmRequest
from google.adk.models.llm_response import LlmResponse
from google.genai import types

from agent import budget


class _FakeSession:
    """Stable logical identity exposed by ADK's CallbackContext."""

    app_name = "agentops-agent"
    user_id = "test-user"

    def __init__(self, session_id: str) -> None:
        self.id = session_id


class _FakeContext:
    """Duck-typed CallbackContext with the state and session identity callbacks use."""

    def __init__(self, state: dict[str, Any] | None = None, session_id: str = "test-session") -> None:
        self.state = state if state is not None else {}
        self.session = _FakeSession(session_id)


class _FakeSpan:
    def __init__(self) -> None:
        self.attributes: dict[str, Any] = {}

    def set_attribute(self, key: str, value: Any) -> None:
        self.attributes[key] = value


def _context(state: dict[str, Any] | None = None) -> CallbackContext:
    return cast("CallbackContext", _FakeContext(state))


def _response(prompt_tokens: int = 100, completion_tokens: int = 40) -> LlmResponse:
    return LlmResponse(
        usage_metadata=types.GenerateContentResponseUsageMetadata(
            prompt_token_count=prompt_tokens,
            candidates_token_count=completion_tokens,
            total_token_count=prompt_tokens + completion_tokens,
        )
    )


def test_record_accumulates_usage_across_turns() -> None:
    context = _context()
    budget.record_token_usage(context, _response(100, 40))
    budget.record_token_usage(context, _response(50, 10))
    assert context.state[budget.INPUT_TOKENS_KEY] == 150
    assert context.state[budget.OUTPUT_TOKENS_KEY] == 50


def test_record_counts_tool_prompt_reasoning_and_total_only_usage() -> None:
    context = _context()
    budget.record_token_usage(
        context,
        LlmResponse(
            usage_metadata=types.GenerateContentResponseUsageMetadata(
                prompt_token_count=100,
                tool_use_prompt_token_count=25,
                candidates_token_count=40,
                thoughts_token_count=10,
                total_token_count=175,
            )
        ),
    )
    budget.record_token_usage(
        context,
        LlmResponse(
            usage_metadata=types.GenerateContentResponseUsageMetadata(
                total_token_count=75,
            )
        ),
    )
    assert context.state[budget.INPUT_TOKENS_KEY] == 125
    assert context.state[budget.OUTPUT_TOKENS_KEY] == 125


def test_record_serializes_interleaved_updates(monkeypatch: pytest.MonkeyPatch) -> None:
    state: dict[str, Any] = {}
    first_context = _context(state)
    second_context = _context(state)
    first_snapshot_read = Event()
    release_first_snapshot = Event()
    call_guard = Lock()
    call_count = 0
    original_session_usage = budget.session_usage

    def interleaved_session_usage(callback_context: CallbackContext) -> tuple[int, int]:
        nonlocal call_count
        usage = original_session_usage(callback_context)
        with call_guard:
            call_count += 1
            current_call = call_count
        if current_call == 1:
            first_snapshot_read.set()
            # Without the per-session lock, the second callback reaches this
            # helper and both callbacks retain the same pre-update snapshot.
            release_first_snapshot.wait(timeout=0.5)
        else:
            release_first_snapshot.set()
        return usage

    monkeypatch.setattr(budget, "session_usage", interleaved_session_usage)
    with ThreadPoolExecutor(max_workers=2) as executor:
        first = executor.submit(budget.record_token_usage, first_context, _response(100, 40))
        assert first_snapshot_read.wait(timeout=1)
        second = executor.submit(budget.record_token_usage, second_context, _response(50, 10))
        first.result(timeout=2)
        second.result(timeout=2)

    assert state[budget.INPUT_TOKENS_KEY] == 150
    assert state[budget.OUTPUT_TOKENS_KEY] == 50


def test_record_ignores_responses_without_usage() -> None:
    context = _context()
    budget.record_token_usage(context, LlmResponse())
    assert budget.INPUT_TOKENS_KEY not in context.state


def test_record_emits_span_attributes(monkeypatch) -> None:
    span = _FakeSpan()
    monkeypatch.setattr(budget.trace, "get_current_span", lambda: span)
    monkeypatch.setattr(budget.settings, "input_price_per_1k", 0.5)
    monkeypatch.setattr(budget.settings, "output_price_per_1k", 1.0)
    budget.record_token_usage(_context(), _response(1000, 500))
    assert span.attributes["agentops.tokens.session.input"] == 1000
    assert span.attributes["agentops.tokens.session.output"] == 500
    assert span.attributes["agentops.tokens.session.total"] == 1500
    assert span.attributes["agentops.cost.session.estimate"] == pytest.approx(1.0)


def test_budget_disabled_by_default(monkeypatch) -> None:
    monkeypatch.setattr(budget.settings, "max_tokens_per_session", None)
    state = {budget.INPUT_TOKENS_KEY: 10**9}
    assert budget.enforce_token_budget(_context(state), cast("LlmRequest", None)) is None


def test_budget_allows_work_under_the_limit(monkeypatch) -> None:
    monkeypatch.setattr(budget.settings, "max_tokens_per_session", 1000)
    state = {budget.INPUT_TOKENS_KEY: 500, budget.OUTPUT_TOKENS_KEY: 499}
    assert budget.enforce_token_budget(_context(state), cast("LlmRequest", None)) is None


def test_budget_blocks_work_with_actionable_message(monkeypatch) -> None:
    monkeypatch.setattr(budget.settings, "max_tokens_per_session", 1000)
    state = {budget.INPUT_TOKENS_KEY: 800, budget.OUTPUT_TOKENS_KEY: 200}
    refusal = budget.enforce_token_budget(_context(state), cast("LlmRequest", None))
    assert refusal is not None
    assert refusal.error_code == "TOKEN_BUDGET_EXHAUSTED"
    assert refusal.content is not None
    assert refusal.content.parts
    text = refusal.content.parts[0].text
    assert text is not None
    assert "1000 of 1000 tokens used" in text
    assert "AGENT_MAX_TOKENS_PER_SESSION" in text


def test_cost_estimate_uses_configured_prices(monkeypatch) -> None:
    monkeypatch.setattr(budget.settings, "input_price_per_1k", 0.25)
    monkeypatch.setattr(budget.settings, "output_price_per_1k", 2.0)
    assert budget.estimate_cost(4000, 1000) == pytest.approx(0.25 * 4 + 2.0)


def test_cost_is_zero_for_the_local_path() -> None:
    assert budget.estimate_cost(10_000, 10_000) == 0.0
