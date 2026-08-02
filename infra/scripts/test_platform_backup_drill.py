"""Offline tests for the platform backup drill's destructive and verification programs."""

from __future__ import annotations

import hashlib
import json
import pathlib
import shutil
import sqlite3
import sys
import tempfile
import unittest
from contextlib import closing

SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR))

import platform_backup_mutate  # noqa: E402  # ty: ignore[unresolved-import]
import platform_backup_verify  # noqa: E402  # ty: ignore[unresolved-import]


class PlatformBackupDrillTests(unittest.TestCase):
    def test_mutation_and_restored_snapshot_contract(self) -> None:
        evidence = {
            "audit_action": "restart_service",
            "audit_invocation_id": "fixture",
            "audit_target": "inventory",
            "memory_incident_id": "INC-002",
            "memory_note": "fixture-note",
            "memory_user_id": "platform-ci",
            "session_id": "session-1",
            "task_id": "task-1",
        }
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            state = root / "state"
            snapshot = root / "snapshot"
            state.mkdir()
            snapshot.mkdir()
            self.seed_databases(state, evidence)
            databases = []
            for database in sorted(state.glob("*.db")):
                target = snapshot / database.name
                shutil.copy2(database, target)
                databases.append({"filename": database.name, "sha256": hashlib.sha256(target.read_bytes()).hexdigest()})
            (snapshot / "manifest.json").write_text(
                json.dumps({"source": {"commit": "abc123"}, "databases": databases}),
                encoding="utf-8",
            )

            platform_backup_mutate.mutate(state, evidence)
            for database in snapshot.glob("*.db"):
                shutil.copy2(database, state / database.name)
            (state / "obsolete.db").unlink()
            platform_backup_verify.verify(snapshot, state, "abc123", evidence)

    @staticmethod
    def seed_databases(state: pathlib.Path, evidence: dict[str, str]) -> None:
        """Create the minimal relational shapes used by the drill."""
        with closing(sqlite3.connect(state / "runtime.db")) as connection, connection:
            connection.execute("CREATE TABLE sessions (id TEXT PRIMARY KEY)")
            connection.execute("CREATE TABLE tasks (id TEXT PRIMARY KEY)")
            connection.execute("INSERT INTO sessions VALUES (?)", (evidence["session_id"],))
            connection.execute("INSERT INTO tasks VALUES (?)", (evidence["task_id"],))
        with closing(sqlite3.connect(state / "memory.db")) as connection, connection:
            connection.execute("CREATE TABLE incident_notes (user_id TEXT, incident_id TEXT, note TEXT)")
            connection.execute(
                "INSERT INTO incident_notes VALUES (?, ?, ?)",
                (evidence["memory_user_id"], evidence["memory_incident_id"], evidence["memory_note"]),
            )
        with closing(sqlite3.connect(state / "incidents.db")) as connection, connection:
            connection.execute("CREATE TABLE audit_log (invocation_id TEXT, action TEXT, target TEXT)")
            connection.execute(
                "CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'no'); END"
            )
            connection.execute(
                "INSERT INTO audit_log VALUES (?, ?, ?)",
                (evidence["audit_invocation_id"], evidence["audit_action"], evidence["audit_target"]),
            )
