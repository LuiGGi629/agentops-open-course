package platformdrill

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/buildinfo"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/domain"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/state"
)

var testSourceIdentity = strings.Repeat("a", 40)

func testBuild() buildinfo.Info {
	return buildinfo.Info{
		Timestamp:      time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC),
		Mode:           buildinfo.Development,
		Version:        buildinfo.DevelopmentVersion,
		SourceIdentity: testSourceIdentity,
		Revision:       testSourceIdentity,
		TreeDigest:     "sha256:" + strings.Repeat("b", 64),
	}
}

func TestSeedUsesProductionWriteBoundariesAndIsIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	seedA2AState(t, stateDir, "session-1", "task-1")
	options := SeedOptions{
		StateDir: stateDir,
		DataDir:  filepath.Clean(filepath.Join("..", "..", "data")),
		Marker:   "fixture-seed",
	}
	first, err := Seed(t.Context(), options)
	if err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	second, err := Seed(t.Context(), options)
	if err != nil {
		t.Fatalf("Seed() replay error = %v", err)
	}
	if first != second {
		t.Fatalf("Seed() replay = %+v, want %+v", second, first)
	}
	for _, check := range evidenceChecks(first) {
		count, queryErr := queryCount(t.Context(), filepath.Join(stateDir, check.database), check.query, check.args, false)
		if queryErr != nil {
			t.Fatalf("query seeded %s evidence: %v", check.database, queryErr)
		}
		if count != 1 {
			t.Errorf("seeded %s evidence rows = %d, want 1", check.database, count)
		}
	}
}

func TestSeedRecoversBeforeReadingOrPublishingDrillState(t *testing.T) {
	stateDir := t.TempDir()
	seedA2AState(t, stateDir, "session-1", "task-1")
	if mkdirErr := os.Mkdir(filepath.Join(stateDir, ".restore-staged.unexplained"), 0o750); mkdirErr != nil {
		t.Fatalf("plant restore residue: %v", mkdirErr)
	}
	_, err := Seed(t.Context(), SeedOptions{
		StateDir: stateDir,
		DataDir:  filepath.Clean(filepath.Join("..", "..", "data")),
		Marker:   "fixture-seed",
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "residue") {
		t.Fatalf("Seed() error = %v, want crash-recovery refusal", err)
	}
	for _, name := range []string{"memory.db", "incidents.db"} {
		if _, statErr := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s was published before crash recovery: %v", name, statErr)
		}
	}
}

func TestRestoreDrillDestroysThenRecoversExactGeneration(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	backupRoot := filepath.Join(root, "backups")
	if err := os.Mkdir(stateDir, 0o750); err != nil {
		t.Fatalf("create state fixture: %v", err)
	}
	evidence := fixtureEvidence()
	seedRestoreState(t, stateDir, evidence)
	snapshot, err := state.BackupState(t.Context(), stateDir, backupRoot, state.BackupOptions{
		Keep: 1, Timestamp: "20260808T120000Z", Build: testBuild(),
	})
	if err != nil {
		t.Fatalf("BackupState() error = %v", err)
	}
	if err := RestoreDrill(t.Context(), RestoreOptions{
		Snapshot: snapshot, StateDir: stateDir, ExpectedSourceIdentity: testSourceIdentity, Evidence: evidence,
	}); err != nil {
		t.Fatalf("RestoreDrill() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "obsolete.db")); !os.IsNotExist(err) {
		t.Fatalf("obsolete.db survived restore, stat error = %v", err)
	}
}

func TestRestoreDrillRefusesProvenanceMismatchBeforeMutation(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o750); err != nil {
		t.Fatalf("create state fixture: %v", err)
	}
	evidence := fixtureEvidence()
	seedRestoreState(t, stateDir, evidence)
	snapshot, err := state.BackupState(t.Context(), stateDir, filepath.Join(root, "backups"), state.BackupOptions{
		Keep: 1, Timestamp: "20260808T120000Z", Build: testBuild(),
	})
	if err != nil {
		t.Fatalf("BackupState() error = %v", err)
	}
	err = RestoreDrill(t.Context(), RestoreOptions{
		Snapshot: snapshot, StateDir: stateDir, ExpectedSourceIdentity: strings.Repeat("c", 40), Evidence: evidence,
	})
	if err == nil {
		t.Fatal("RestoreDrill() accepted a snapshot from another revision")
	}
	count, queryErr := queryCount(t.Context(), filepath.Join(stateDir, "runtime.db"),
		"SELECT COUNT(*) FROM sessions WHERE id = ?", []any{evidence.SessionID}, false)
	if queryErr != nil {
		t.Fatalf("query untouched state: %v", queryErr)
	}
	if count != 1 {
		t.Errorf("session evidence rows after refusal = %d, want 1", count)
	}
}

func TestRestoreDrillRecoversBeforeDestructiveMutation(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0o750); err != nil {
		t.Fatalf("create state fixture: %v", err)
	}
	evidence := fixtureEvidence()
	seedRestoreState(t, stateDir, evidence)
	snapshot, err := state.BackupState(t.Context(), stateDir, filepath.Join(root, "backups"), state.BackupOptions{
		Keep: 1, Timestamp: "20260808T120000Z", Build: testBuild(),
	})
	if err != nil {
		t.Fatalf("BackupState() error = %v", err)
	}
	if mkdirErr := os.Mkdir(filepath.Join(stateDir, ".restore-staged.unexplained"), 0o750); mkdirErr != nil {
		t.Fatalf("plant restore residue: %v", mkdirErr)
	}
	err = RestoreDrill(t.Context(), RestoreOptions{
		Snapshot: snapshot, StateDir: stateDir, ExpectedSourceIdentity: testSourceIdentity, Evidence: evidence,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "residue") {
		t.Fatalf("RestoreDrill() error = %v, want crash-recovery refusal", err)
	}
	count, queryErr := queryCount(t.Context(), filepath.Join(stateDir, "runtime.db"),
		"SELECT COUNT(*) FROM sessions WHERE id = ?", []any{evidence.SessionID}, false)
	if queryErr != nil {
		t.Fatalf("query untouched state: %v", queryErr)
	}
	if count != 1 {
		t.Errorf("session evidence rows after recovery refusal = %d, want 1", count)
	}
}

func TestCLIRejectsUnknownEvidenceFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	reference := domain.Reference()
	payload := map[string]any{
		"audit_action": "restart_service", "audit_invocation_id": "fixture", "audit_target": reference.Services.Inventory,
		"memory_incident_id": reference.Incidents.InventoryDown, "memory_note": "fixture-note", "memory_user_id": "platform-ci",
		"session_id": "session-1", "task_id": "task-1", "prompt": "must not be accepted",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := readEvidence(path); err == nil {
		t.Fatal("readEvidence() accepted an unknown field")
	}
}

func fixtureEvidence() Evidence {
	reference := domain.Reference()
	return Evidence{
		AuditAction:      "restart_service",
		AuditInvocation:  "fixture",
		AuditTarget:      reference.Services.Inventory,
		MemoryIncidentID: reference.Incidents.InventoryDown,
		MemoryNote:       "fixture-note",
		MemoryUserID:     "platform-ci",
		SessionID:        "session-1",
		TaskID:           "task-1",
	}
}

func seedA2AState(t *testing.T, stateDir, sessionID, taskID string) {
	t.Helper()
	execFixture(t, filepath.Join(stateDir, "runtime.db"),
		"CREATE TABLE sessions (id TEXT PRIMARY KEY, update_time TEXT NOT NULL)", nil)
	execFixture(t, filepath.Join(stateDir, "runtime.db"),
		"INSERT INTO sessions VALUES (?, ?)", []any{sessionID, "2026-08-08T12:00:00Z"})
	execFixture(t, filepath.Join(stateDir, "tasks.db"),
		"CREATE TABLE tasks (id TEXT PRIMARY KEY, context_id TEXT NOT NULL, last_updated_ns INTEGER NOT NULL)", nil)
	execFixture(t, filepath.Join(stateDir, "tasks.db"),
		"INSERT INTO tasks VALUES (?, ?, ?)", []any{taskID, sessionID, 1})
}

func seedRestoreState(t *testing.T, stateDir string, evidence Evidence) {
	t.Helper()
	execFixture(t, filepath.Join(stateDir, "runtime.db"), "CREATE TABLE sessions (id TEXT PRIMARY KEY)", nil)
	execFixture(t, filepath.Join(stateDir, "runtime.db"), "INSERT INTO sessions VALUES (?)", []any{evidence.SessionID})
	execFixture(t, filepath.Join(stateDir, "tasks.db"), "CREATE TABLE tasks (id TEXT PRIMARY KEY)", nil)
	execFixture(t, filepath.Join(stateDir, "tasks.db"), "INSERT INTO tasks VALUES (?)", []any{evidence.TaskID})
	execFixture(t, filepath.Join(stateDir, "memory.db"),
		"CREATE TABLE incident_notes (user_id TEXT, incident_id TEXT, note TEXT)", nil)
	execFixture(t, filepath.Join(stateDir, "memory.db"), "INSERT INTO incident_notes VALUES (?, ?, ?)",
		[]any{evidence.MemoryUserID, evidence.MemoryIncidentID, evidence.MemoryNote})
	execFixture(t, filepath.Join(stateDir, "incidents.db"),
		"CREATE TABLE audit_log (invocation_id TEXT, action TEXT, target TEXT)", nil)
	execFixture(t, filepath.Join(stateDir, "incidents.db"),
		"CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'no'); END", nil)
	execFixture(t, filepath.Join(stateDir, "incidents.db"), "INSERT INTO audit_log VALUES (?, ?, ?)",
		[]any{evidence.AuditInvocation, evidence.AuditAction, evidence.AuditTarget})
}

func execFixture(t *testing.T, path, query string, args []any) {
	t.Helper()
	db, err := sql.Open(sqliteDriver, "file:"+path)
	if err != nil {
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		_ = db.Close()
		t.Fatalf("execute %s fixture: %v", filepath.Base(path), err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close %s: %v", filepath.Base(path), err)
	}
}
