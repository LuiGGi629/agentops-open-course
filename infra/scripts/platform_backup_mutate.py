"""Corrupt a disposable state copy before the platform restore drill."""

from __future__ import annotations

import json
import pathlib
import sqlite3
import sys
from contextlib import closing


def evidence_checks(evidence: dict[str, str]) -> tuple[tuple[str, str, tuple[str, ...]], ...]:
    """Return the four persistent rows the drill follows across restore."""
    return (
        ("runtime.db", "SELECT COUNT(*) FROM sessions WHERE id = ?", (evidence["session_id"],)),
        ("runtime.db", "SELECT COUNT(*) FROM tasks WHERE id = ?", (evidence["task_id"],)),
        (
            "memory.db",
            "SELECT COUNT(*) FROM incident_notes WHERE user_id = ? AND incident_id = ? AND note = ?",
            (evidence["memory_user_id"], evidence["memory_incident_id"], evidence["memory_note"]),
        ),
        (
            "incidents.db",
            "SELECT COUNT(*) FROM audit_log WHERE invocation_id = ? AND action = ? AND target = ?",
            (evidence["audit_invocation_id"], evidence["audit_action"], evidence["audit_target"]),
        ),
    )


def mutate(state: pathlib.Path, evidence: dict[str, str]) -> None:
    """Delete all evidence, add sentinels, and prove the destructive fixture took effect."""
    mutations = {
        "runtime.db": (
            ("DELETE FROM sessions WHERE id = ?", (evidence["session_id"],)),
            ("DELETE FROM tasks WHERE id = ?", (evidence["task_id"],)),
        ),
        "memory.db": (
            (
                "DELETE FROM incident_notes WHERE user_id = ? AND incident_id = ? AND note = ?",
                (evidence["memory_user_id"], evidence["memory_incident_id"], evidence["memory_note"]),
            ),
        ),
        "incidents.db": (
            ("DROP TRIGGER audit_log_no_delete", ()),
            (
                "DELETE FROM audit_log WHERE invocation_id = ? AND action = ? AND target = ?",
                (evidence["audit_invocation_id"], evidence["audit_action"], evidence["audit_target"]),
            ),
        ),
    }
    for name, statements in mutations.items():
        with closing(sqlite3.connect(state / name)) as connection, connection:
            for statement, parameters in statements:
                connection.execute(statement, parameters)
            connection.execute("CREATE TABLE ci_restore_sentinel (value TEXT)")
            connection.execute("INSERT INTO ci_restore_sentinel VALUES ('destroy me')")
    with closing(sqlite3.connect(state / "obsolete.db")) as connection, connection:
        connection.execute("CREATE TABLE ci_restore_sentinel (value TEXT)")
        connection.execute("INSERT INTO ci_restore_sentinel VALUES ('destroy me')")

    for name, statement, parameters in evidence_checks(evidence):
        with closing(sqlite3.connect(state / name)) as connection:
            if connection.execute(statement, parameters).fetchone() != (0,):
                raise RuntimeError(f"destructive fixture did not remove {name} evidence")


def main() -> None:
    """Load explicit state and evidence paths from the restore Job."""
    state = pathlib.Path(sys.argv[1])
    evidence = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
    mutate(state, evidence)


if __name__ == "__main__":
    main()
