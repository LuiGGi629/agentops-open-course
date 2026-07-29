"""Circuit breaker — stop hammering a dependency that is already down (Chapter 4.5).

Bounded retries (``resilience.py``) give one flaky call a second chance. They do
the wrong thing when a dependency is *persistently* down: every turn pays the
full retry budget before failing, so a dead gateway turns into a pile of slow,
doomed calls. A circuit breaker is the complementary lever — after a run of
failures it *opens* and fails fast, sheds load off the struggling dependency,
then after a cooldown lets a single trial call test whether it recovered.

The breaker is deterministic and injectable (the clock is a constructor
argument) so the offline test gate exercises every transition without sleeping.
It is **opt-in**: ``AGENT_CIRCUIT_BREAKER_ENABLED`` defaults to ``false`` so the
default behavior — retry only — is unchanged until a builder turns it on. Like
every guarded write, it only ever wraps idempotent reads (``with_resilience``);
a breaker must never sit in front of a non-idempotent action.
"""

from __future__ import annotations

import time
from collections.abc import Callable
from dataclasses import dataclass, field
from enum import StrEnum
from threading import Lock

from opentelemetry import metrics

from .config import settings

# A counter (not a gauge) so Prometheus can alert on an opening *rate*: a breaker
# that flaps open repeatedly is a louder signal than one that is momentarily open.
_OPENED_COUNTER = metrics.get_meter("agentops.agent").create_counter(
    "agentops.circuit.opened_total",
    unit="1",
    description="Times a circuit breaker opened, by resource",
)


class CircuitState(StrEnum):
    """The three states of a standard circuit breaker."""

    CLOSED = "closed"  # calls flow; failures are counted
    OPEN = "open"  # calls fail fast until the cooldown elapses
    HALF_OPEN = "half_open"  # one trial call is allowed to test recovery


class CircuitOpenError(RuntimeError):
    """Raised instead of calling a dependency whose breaker is open."""


@dataclass(frozen=True, slots=True)
class CircuitPermit:
    """The breaker generation in which one dependency call was admitted."""

    generation: int


@dataclass(slots=True)
class CircuitBreaker:
    """A per-resource breaker with an injectable monotonic clock.

    ``allow`` returns an admission permit before work starts. Outcome methods
    accept only permits from the current breaker generation, so a slow call
    admitted before a newer failure opened the breaker cannot later close or
    re-open it. Keeping the clock injectable makes every transition testable
    without real time passing.
    """

    name: str
    failure_threshold: int
    reset_timeout_s: float
    clock: Callable[[], float] = time.monotonic
    state: CircuitState = CircuitState.CLOSED
    failures: int = 0
    opened_at: float = field(default=0.0)
    _lock: Lock = field(default_factory=Lock, init=False, repr=False)
    _half_open_probe_active: bool = field(default=False, init=False, repr=False)
    _generation: int = field(default=0, init=False, repr=False)

    def allow(self) -> CircuitPermit | None:
        """Admit one call, advancing OPEN → HALF_OPEN after the cooldown.

        Closed allows concurrent calls. Open allows exactly one call once the
        reset timeout has elapsed; concurrent callers fail fast until that
        half-open probe records success or failure. ``None`` means the caller
        must fail fast.
        """
        with self._lock:
            if self.state is CircuitState.OPEN:
                if self.clock() - self.opened_at < self.reset_timeout_s:
                    return None
                self.state = CircuitState.HALF_OPEN
                self._generation += 1
                self._half_open_probe_active = True
                return CircuitPermit(self._generation)
            if self.state is CircuitState.HALF_OPEN:
                if self._half_open_probe_active:
                    return None
                self._half_open_probe_active = True
            return CircuitPermit(self._generation)

    def record_success(self, permit: CircuitPermit) -> None:
        """Record a current-generation success, ignoring stale call outcomes."""
        with self._lock:
            if permit.generation != self._generation or self.state is CircuitState.OPEN:
                return
            if self.state is CircuitState.HALF_OPEN:
                self.state = CircuitState.CLOSED
                self._generation += 1
            self.failures = 0
            self._half_open_probe_active = False

    def record_abandoned(self, permit: CircuitPermit) -> None:
        """Release a cancelled half-open probe without counting a failure.

        A cancelled caller did not prove that the dependency is unhealthy, but
        leaving its permit active would strand the breaker in half-open forever.
        Re-open from now so a later caller gets a fresh, exclusive recovery probe.
        """
        with self._lock:
            if (
                permit.generation != self._generation
                or self.state is not CircuitState.HALF_OPEN
                or not self._half_open_probe_active
            ):
                return
            _OPENED_COUNTER.add(1, {"resource": self.name})
            self.state = CircuitState.OPEN
            self.opened_at = self.clock()
            self._half_open_probe_active = False
            self._generation += 1

    # --8<-- [start:record-failure]
    def record_failure(self, permit: CircuitPermit) -> None:
        """Record a current-generation failure and trip at the threshold.

        A failure while half-open re-opens immediately: the trial proved the
        dependency is still unhealthy, so start a fresh cooldown. Stale outcomes
        and failures while already open are no-ops: they must not change the
        current failure count or cooldown.
        """
        with self._lock:
            if self.state is CircuitState.OPEN or permit.generation != self._generation:
                return
            self.failures += 1
            if self.state is CircuitState.HALF_OPEN or self.failures >= self.failure_threshold:
                _OPENED_COUNTER.add(1, {"resource": self.name})
                self.state = CircuitState.OPEN
                self.opened_at = self.clock()
                self._half_open_probe_active = False
                self._generation += 1

    # --8<-- [end:record-failure]


# One breaker per wrapped resource (tool name), created lazily from settings.
_BREAKERS: dict[str, CircuitBreaker] = {}
_BREAKERS_LOCK = Lock()


def get_breaker(name: str) -> CircuitBreaker:
    """Return the named breaker, creating it from the current settings once."""
    with _BREAKERS_LOCK:
        breaker = _BREAKERS.get(name)
        if breaker is None:
            breaker = CircuitBreaker(
                name=name,
                failure_threshold=settings.circuit_failure_threshold,
                reset_timeout_s=settings.circuit_reset_timeout_s,
            )
            _BREAKERS[name] = breaker
        return breaker


def reset_breakers() -> None:
    """Clear the breaker registry (test isolation between cases)."""
    with _BREAKERS_LOCK:
        _BREAKERS.clear()
