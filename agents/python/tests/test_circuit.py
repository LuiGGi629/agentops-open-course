"""Unit tests for the circuit breaker and its wiring into ``with_resilience``.

The breaker's clock is injectable, so every open/half-open/close transition is
exercised deterministically with a fake monotonic clock — no real time passes.
"""

import asyncio
import time
from concurrent.futures import ThreadPoolExecutor
from threading import Barrier, Lock
from typing import Any

import pytest
from hypothesis import settings as hypothesis_settings
from hypothesis import strategies as st
from hypothesis.stateful import RuleBasedStateMachine, invariant, precondition, rule

from agent import circuit, resilience
from agent.circuit import CircuitBreaker, CircuitOpenError, CircuitPermit, CircuitState, get_breaker, reset_breakers
from agent.resilience import with_resilience


class _FakeClock:
    """A monotonic clock the test advances explicitly."""

    def __init__(self) -> None:
        self.now = 0.0

    def __call__(self) -> float:
        return self.now


class CircuitProtocolMachine(RuleBasedStateMachine):
    """Exercise arbitrary permit outcomes across breaker generations."""

    def __init__(self) -> None:
        super().__init__()
        self.clock = _FakeClock()
        self.breaker = CircuitBreaker(name="stateful", failure_threshold=2, reset_timeout_s=5.0, clock=self.clock)
        self.permits: list[CircuitPermit] = []

    @rule()
    def request_permit(self) -> None:
        before = self.breaker.state
        permit = self.breaker.allow()
        if before is CircuitState.HALF_OPEN or (
            before is CircuitState.OPEN and self.clock.now - self.breaker.opened_at < 5.0
        ):
            assert permit is None
        if permit is not None:
            self.permits.append(permit)

    @rule(seconds=st.floats(min_value=0.0, max_value=10.0, allow_nan=False, allow_infinity=False))
    def advance_clock(self, seconds: float) -> None:
        self.clock.now += seconds

    @precondition(lambda self: bool(self.permits))
    @rule(index=st.integers(min_value=0, max_value=1000))
    def record_success(self, index: int) -> None:
        permit = self.permits[index % len(self.permits)]
        stale = permit.generation != self.breaker._generation or self.breaker.state is CircuitState.OPEN  # noqa: SLF001
        before = (self.breaker.state, self.breaker.failures, self.breaker.opened_at)
        self.breaker.record_success(permit)
        if stale:
            assert (self.breaker.state, self.breaker.failures, self.breaker.opened_at) == before

    @precondition(lambda self: bool(self.permits))
    @rule(index=st.integers(min_value=0, max_value=1000))
    def record_failure(self, index: int) -> None:
        permit = self.permits[index % len(self.permits)]
        stale = self.breaker.state is CircuitState.OPEN or permit.generation != self.breaker._generation  # noqa: SLF001
        before = (self.breaker.state, self.breaker.failures, self.breaker.opened_at)
        self.breaker.record_failure(permit)
        if stale:
            assert (self.breaker.state, self.breaker.failures, self.breaker.opened_at) == before

    @precondition(lambda self: bool(self.permits))
    @rule(index=st.integers(min_value=0, max_value=1000))
    def abandon(self, index: int) -> None:
        self.breaker.record_abandoned(self.permits[index % len(self.permits)])

    @invariant()
    def permits_never_come_from_a_future_generation(self) -> None:
        assert all(permit.generation <= self.breaker._generation for permit in self.permits)  # noqa: SLF001
        assert self.breaker.failures >= 0


TestCircuitProtocol = CircuitProtocolMachine.TestCase
TestCircuitProtocol.settings = hypothesis_settings(
    database=None,
    deadline=None,
    derandomize=True,
    max_examples=50,
    stateful_step_count=30,
)


@pytest.fixture(autouse=True)
def _isolate_breakers() -> Any:
    """Each test starts with an empty breaker registry."""
    reset_breakers()
    yield
    reset_breakers()


def _admit(breaker: CircuitBreaker) -> CircuitPermit:
    permit = breaker.allow()
    assert permit is not None
    return permit


def _fail(breaker: CircuitBreaker) -> None:
    breaker.record_failure(_admit(breaker))


def test_opens_after_consecutive_failures() -> None:
    breaker = CircuitBreaker(name="reads", failure_threshold=3, reset_timeout_s=30.0, clock=_FakeClock())
    for _ in range(2):
        _fail(breaker)
    assert breaker.state is CircuitState.CLOSED
    breaker.record_failure(_admit(breaker))  # third failure trips it
    assert breaker.state is CircuitState.OPEN
    assert breaker.allow() is None  # fails fast while open


def test_half_open_after_cooldown_then_closes_on_success() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=30.0, clock=clock)
    _fail(breaker)
    assert breaker.allow() is None
    clock.now = 29.0
    assert breaker.allow() is None  # cooldown not elapsed
    clock.now = 30.0
    permit = _admit(breaker)  # one trial call permitted
    assert breaker.state is CircuitState.HALF_OPEN
    breaker.record_success(permit)
    assert breaker.state is CircuitState.CLOSED
    assert breaker.failures == 0


def test_half_open_allows_only_one_concurrent_probe() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=30.0, clock=clock)
    _fail(breaker)
    clock.now = 30.0
    start = Barrier(8)

    def attempt() -> CircuitPermit | None:
        start.wait()
        return breaker.allow()

    with ThreadPoolExecutor(max_workers=8) as executor:
        permits = list(executor.map(lambda _: attempt(), range(8)))

    assert sum(permit is not None for permit in permits) == 1
    assert breaker.state is CircuitState.HALF_OPEN


def test_repeated_failure_while_open_keeps_one_opened_transition() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=30.0, clock=clock)
    stale_permit = _admit(breaker)
    breaker.record_failure(stale_permit)  # opens
    assert breaker.state is CircuitState.OPEN
    opened_at = breaker.opened_at
    clock.now = 5.0
    breaker.record_failure(stale_permit)  # already open: ignore the stale outcome
    assert breaker.state is CircuitState.OPEN
    assert breaker.failures == 1
    assert breaker.opened_at == opened_at


def test_half_open_failure_reopens_with_fresh_cooldown() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=10.0, clock=clock)
    _fail(breaker)
    clock.now = 10.0
    permit = _admit(breaker)  # half-open trial
    clock.now = 12.0
    breaker.record_failure(permit)  # trial failed: reopen from now
    assert breaker.state is CircuitState.OPEN
    assert breaker.opened_at == 12.0
    assert breaker.allow() is None


def test_cancelled_half_open_probe_reopens_without_counting_a_failure(monkeypatch) -> None:
    monkeypatch.setattr(resilience.settings, "circuit_breaker_enabled", True)
    monkeypatch.setattr(resilience.settings, "circuit_failure_threshold", 1)
    monkeypatch.setattr(resilience.settings, "circuit_reset_timeout_s", 30.0)
    monkeypatch.setattr(resilience.settings, "max_retries", 0)
    probe_started = asyncio.Event()
    calls = 0

    async def controlled_to_thread(func, **kwargs):
        nonlocal calls
        del func, kwargs
        calls += 1
        if calls == 1:
            raise ConnectionError("dependency down")
        probe_started.set()
        await asyncio.Future()

    monkeypatch.setattr(resilience.asyncio, "to_thread", controlled_to_thread)
    wrapped = with_resilience(lambda: {"ok": True})

    async def exercise() -> None:
        with pytest.raises(RuntimeError, match="failed after 1 attempt"):
            await wrapped()
        breaker = get_breaker("<lambda>")
        failure_count = breaker.failures
        breaker.opened_at = breaker.clock() - breaker.reset_timeout_s

        probe = asyncio.create_task(wrapped())
        await probe_started.wait()
        assert breaker.state is CircuitState.HALF_OPEN
        probe.cancel()
        with pytest.raises(asyncio.CancelledError):
            await probe

        assert breaker.state is CircuitState.OPEN
        assert breaker.failures == failure_count
        assert breaker.allow() is None

    asyncio.run(exercise())


def test_abandoning_a_closed_or_stale_permit_is_a_noop() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=10.0, clock=clock)
    closed_permit = _admit(breaker)
    breaker.record_abandoned(closed_permit)
    assert breaker.state is CircuitState.CLOSED

    breaker.record_failure(closed_permit)
    clock.now = 10.0
    half_open_permit = _admit(breaker)
    breaker.record_success(half_open_permit)
    breaker.record_abandoned(half_open_permit)
    assert breaker.state is CircuitState.CLOSED


def test_stale_success_cannot_close_a_breaker_opened_by_a_newer_outcome() -> None:
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=30.0)
    stale_success = _admit(breaker)
    newer_failure = _admit(breaker)

    breaker.record_failure(newer_failure)
    breaker.record_success(stale_success)

    assert breaker.state is CircuitState.OPEN
    assert breaker.failures == 1


def test_stale_failure_cannot_reopen_a_recovered_breaker_generation() -> None:
    clock = _FakeClock()
    breaker = CircuitBreaker(name="reads", failure_threshold=1, reset_timeout_s=10.0, clock=clock)
    stale_failure = _admit(breaker)
    newer_failure = _admit(breaker)
    breaker.record_failure(newer_failure)
    clock.now = 10.0
    recovery_probe = _admit(breaker)
    breaker.record_success(recovery_probe)

    breaker.record_failure(stale_failure)

    assert breaker.state is CircuitState.CLOSED
    assert breaker.failures == 0


# These scheduler-driven tests can expose lost updates and duplicate construction,
# but no finite thread interleaving proves the absence of every possible race.
def test_concurrent_failures_are_not_lost() -> None:
    class _SlowInt(int):
        def __add__(self, other: int) -> "_SlowInt":
            time.sleep(0.005)
            return _SlowInt(int(self) + other)

    breaker = CircuitBreaker(name="reads", failure_threshold=100, reset_timeout_s=30.0)
    breaker.failures = _SlowInt(0)
    start = Barrier(16)

    def fail() -> None:
        start.wait()
        breaker.record_failure(_admit(breaker))

    with ThreadPoolExecutor(max_workers=16) as executor:
        list(executor.map(lambda _: fail(), range(16)))

    assert breaker.failures == 16
    assert breaker.state is CircuitState.CLOSED


def test_concurrent_registry_access_creates_one_breaker(monkeypatch) -> None:
    original = circuit.CircuitBreaker
    constructor_calls = 0
    calls_lock = Lock()
    start = Barrier(16)

    def slow_constructor(*args: Any, **kwargs: Any) -> CircuitBreaker:
        nonlocal constructor_calls
        with calls_lock:
            constructor_calls += 1
        time.sleep(0.005)
        return original(*args, **kwargs)

    monkeypatch.setattr(circuit, "CircuitBreaker", slow_constructor)

    def resolve() -> CircuitBreaker:
        start.wait()
        return get_breaker("shared")

    with ThreadPoolExecutor(max_workers=16) as executor:
        breakers = list(executor.map(lambda _: resolve(), range(16)))

    assert constructor_calls == 1
    assert all(breaker is breakers[0] for breaker in breakers)


def test_with_resilience_fails_fast_when_circuit_open(monkeypatch) -> None:
    monkeypatch.setattr(resilience.settings, "circuit_breaker_enabled", True)
    monkeypatch.setattr(resilience.settings, "circuit_failure_threshold", 1)
    monkeypatch.setattr(resilience.settings, "circuit_reset_timeout_s", 30.0)
    monkeypatch.setattr(resilience.settings, "max_retries", 0)
    calls = {"count": 0}

    def broken() -> dict[str, Any]:
        calls["count"] += 1
        raise ConnectionError("dependency down")

    wrapped = with_resilience(broken)
    with pytest.raises(RuntimeError, match="failed after 1 attempt"):
        asyncio.run(wrapped())  # first failure opens the breaker
    assert get_breaker("broken").state is CircuitState.OPEN
    with pytest.raises(CircuitOpenError, match="circuit is open"):
        asyncio.run(wrapped())  # second call is refused without invoking the function
    assert calls["count"] == 1  # the open circuit shed the second call


def test_success_keeps_circuit_closed(monkeypatch) -> None:
    monkeypatch.setattr(resilience.settings, "circuit_breaker_enabled", True)
    monkeypatch.setattr(resilience.settings, "circuit_failure_threshold", 2)
    monkeypatch.setattr(resilience.settings, "max_retries", 0)

    def healthy() -> dict[str, Any]:
        return {"ok": True}

    assert asyncio.run(with_resilience(healthy)()) == {"ok": True}
    assert get_breaker("healthy").state is CircuitState.CLOSED


def test_deadline_counts_as_a_breaker_failure(monkeypatch) -> None:
    import time

    monkeypatch.setattr(resilience.settings, "circuit_breaker_enabled", True)
    monkeypatch.setattr(resilience.settings, "circuit_failure_threshold", 1)
    monkeypatch.setattr(resilience.settings, "tool_timeout_s", 0.05)
    monkeypatch.setattr(resilience.settings, "max_retries", 3)

    def hanging() -> dict[str, Any]:
        time.sleep(1)
        return {}

    with pytest.raises(resilience.ToolDeadlineError):
        asyncio.run(with_resilience(hanging)())
    assert get_breaker("hanging").state is CircuitState.OPEN  # a persistent timeout trips the breaker


def test_disabled_circuit_leaves_retry_only_behavior(monkeypatch) -> None:
    monkeypatch.setattr(resilience.settings, "circuit_breaker_enabled", False)
    monkeypatch.setattr(resilience.settings, "max_retries", 0)
    calls = {"count": 0}

    def broken() -> dict[str, Any]:
        calls["count"] += 1
        raise ConnectionError("dependency down")

    wrapped = with_resilience(broken)
    for _ in range(3):
        with pytest.raises(RuntimeError, match="failed after 1 attempt"):
            asyncio.run(wrapped())
    assert calls["count"] == 3  # no breaker: every call still reaches the function
    assert circuit._BREAKERS == {}  # noqa: SLF001 — asserts no breaker was created when disabled
