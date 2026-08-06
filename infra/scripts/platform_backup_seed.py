"""Seed persistent platform evidence for the scheduled backup/restore drill."""

from __future__ import annotations

import json
import sqlite3
import sys
from contextlib import closing
from types import SimpleNamespace
from typing import cast

from google.adk.tools.tool_context import ToolContext

from agent import actions, longterm
from agent.config import settings


def main(marker: str) -> None:
    """Persist one session-linked action and one long-term note, then print their identities."""
    with closing(sqlite3.connect(settings.state_dir / "runtime.db")) as connection:
        row = connection.execute(
            """
            SELECT sessions.id, tasks.id
            FROM sessions
            JOIN tasks ON tasks.context_id = sessions.id
            ORDER BY sessions.update_time DESC
            LIMIT 1
            """
        ).fetchone()
    if row is None:
        raise RuntimeError("deterministic A2A traffic did not persist a session and task")
    session_id, task_id = row

    note = f"{marker}-long-term-note"
    note_context = cast(ToolContext, SimpleNamespace(user_id="platform-ci"))
    note_result = longterm.save_incident_note("INC-002", note, note_context)
    if "saved" not in note_result:
        raise RuntimeError(f"could not seed long-term evidence: {note_result}")

    action_result = actions.restart_service(
        "inventory",
        cast(
            ToolContext,
            SimpleNamespace(
                user_id="platform-ci",
                session=SimpleNamespace(id=session_id),
                invocation_id=marker,
                tool_confirmation=SimpleNamespace(
                    confirmed=True,
                    payload={
                        "rationale": (
                            "Automated backup fixture for the disposable local platform; the target is a mock service."
                        )
                    },
                ),
            ),
        ),
    )
    if "audit" not in action_result:
        raise RuntimeError(f"could not seed audit evidence: {action_result}")

    sys.stdout.write(
        json.dumps(
            {
                "audit_action": action_result["audit"]["action"],
                "audit_invocation_id": marker,
                "audit_target": action_result["audit"]["target"],
                "memory_incident_id": "INC-002",
                "memory_note": note,
                "memory_user_id": "platform-ci",
                "session_id": session_id,
                "task_id": task_id,
            },
            sort_keys=True,
        )
        + "\n"
    )


if __name__ == "__main__":
    main(sys.argv[1])
