package a2aserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/domain"
)

// SessionDatabaseName is ADK's session store inside AGENT_STATE_DIR. It shares
// its name with the Python track's file so a learner comparing the two tracks
// finds the same artifact in the same place.
const SessionDatabaseName = "runtime.db"

// sessionStoreColumns is the shape ADK Go's session service actually creates.
//
// # This is not the Python contract, and it cannot be
//
// The Python server checked six tables against ADK Python's SQLAlchemy schema,
// including an `adk_internal_metadata` table carrying a schema_version and an
// `events.event_data` blob. ADK Go's session/database package is a different
// implementation with a different gorm schema: there is no metadata table and
// no version marker anywhere, and an event's payload lives in `content` beside
// a dozen sibling columns rather than in one `event_data` document. Porting the
// Python column list verbatim would reject every database ADK Go creates.
//
// So this list is derived from ADK Go's own storage models
// (session/database/storage_session.go) and narrowed to the columns whose
// absence would actually break a session: the composite keys, the payloads and
// the ordering timestamps. TestRequiredSessionColumnsMatchADK pins it against a
// freshly migrated database, so an upstream rename is a failing test rather
// than a server that refuses to start.
var sessionStoreColumns = map[string][]string{
	"sessions":    {"app_name", "user_id", "id", "state", "create_time", "update_time"},
	"events":      {"id", "app_name", "user_id", "session_id", "invocation_id", "author", "timestamp", "content"},
	"app_states":  {"app_name", "state", "update_time"},
	"user_states": {"app_name", "user_id", "state", "update_time"},
}

// snapshotGuidance is the operator instruction both incompatibility messages
// end with, spelled once so the two cannot drift apart.
const (
	upgradeGuidance  = "Upgrade the application or select a compatible snapshot."
	selectSnapshot   = "Select a compatible snapshot before startup."
	incompleteSchema = "has an incomplete current schema"
)

// preflightStateStores rejects incompatible runtime state before startup writes
// anything.
//
// # Why this runs before publication
//
// The A2A process is the one writer that publishes first-boot state. If it
// published the incident database and migrated ADK's schema and only then
// discovered that the session or task store came from an incompatible
// generation, the operator would be left with a half-migrated directory that no
// snapshot matches. Refusing first leaves every byte exactly as it was found,
// which is what makes "select a compatible snapshot" an instruction the
// operator can actually follow.
//
// An absent file is not a problem: a fresh deployment has no state yet, and the
// stores create their own schemas during startup.
func preflightStateStores(ctx context.Context, stateDir string) error {
	if err := preflightSessionStore(ctx, filepath.Join(stateDir, SessionDatabaseName)); err != nil {
		return err
	}
	return preflightTaskStore(ctx, filepath.Join(stateDir, TaskDatabaseName))
}

// preflightSessionStore inspects ADK's session database read-only.
func preflightSessionStore(ctx context.Context, path string) error {
	return withReadOnlyDatabase(ctx, path, func(pool *sql.DB) error {
		if err := quickCheck(ctx, pool, SessionDatabaseName); err != nil {
			return fmt.Errorf("%s before startup: %w", SessionDatabaseName, err)
		}
		tables, err := presentTables(ctx, pool)
		if err != nil {
			return err
		}
		if len(tables) == 0 {
			// A file with no user tables is what an interrupted first boot or an
			// empty-file placeholder leaves behind. ADK's migration will fill it.
			return nil
		}
		return checkSessionSchema(ctx, pool, tables, selectSnapshot)
	})
}

// preflightTaskStore inspects this package's own task database read-only.
//
// This is where the version check the Python track got from ADK Python's
// metadata table is re-earned: `store_metadata.schema_version` is the marker,
// and a value this binary does not write is refused with the same guidance.
func preflightTaskStore(ctx context.Context, path string) error {
	return withReadOnlyDatabase(ctx, path, func(pool *sql.DB) error {
		if err := quickCheck(ctx, pool, TaskDatabaseName); err != nil {
			return fmt.Errorf("%s before startup: %w", TaskDatabaseName, err)
		}
		tables, err := presentTables(ctx, pool)
		if err != nil {
			return err
		}
		if len(tables) == 0 {
			return nil
		}
		return checkTaskStoreVersion(ctx, pool, tables)
	})
}

// checkSessionSchema reports the tables and columns ADK's session store is
// missing, in the order an operator can act on.
func checkSessionSchema(ctx context.Context, queryer rowQueryer, tables map[string]struct{}, guidance string) error {
	var missingTables []string
	for table := range sessionStoreColumns {
		if _, ok := tables[table]; !ok {
			missingTables = append(missingTables, table)
		}
	}
	if len(missingTables) > 0 {
		sort.Strings(missingTables)
		return fmt.Errorf("%s %s and is missing tables: %s. %s",
			SessionDatabaseName, incompleteSchema, strings.Join(missingTables, ", "), guidance)
	}
	for _, table := range sortedTables(sessionStoreColumns) {
		columns, err := tableColumns(ctx, queryer, table)
		if err != nil {
			return err
		}
		var missing []string
		for _, column := range sessionStoreColumns[table] {
			if _, ok := columns[column]; !ok {
				missing = append(missing, column)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("%s %s: table %q is missing columns %s. %s",
				SessionDatabaseName, incompleteSchema, table, strings.Join(missing, ", "), guidance)
		}
	}
	return nil
}

// checkTaskStoreVersion reads and judges the task store's schema marker.
func checkTaskStoreVersion(ctx context.Context, queryer rowQueryer, tables map[string]struct{}) error {
	if _, ok := tables[storeMetadataTable]; !ok {
		return fmt.Errorf("%s has an unsupported legacy or malformed schema. %s",
			TaskDatabaseName, upgradeGuidance)
	}
	var version string
	err := queryer.QueryRowContext(ctx,
		`SELECT value FROM store_metadata WHERE key = ?`, schemaVersionKey).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read the %s schema version: %w", TaskDatabaseName, err)
	}
	if version != domain.CurrentRuntimeSchemaVersion {
		return fmt.Errorf("%s has runtime schema %q, but this application supports %q. %s",
			TaskDatabaseName, version, domain.CurrentRuntimeSchemaVersion, upgradeGuidance)
	}
	return checkTaskTables(ctx, queryer, tables)
}

// probeStateStores validates the session and task stores through dedicated
// read-only connections.
//
// # Why its own connections
//
// Readiness must not borrow the traffic pool. Both stores hold a single
// connection on purpose, so a readiness check that queued behind a live
// transaction would report a busy server as unready — turning load into a
// removal from the load balancer, which makes the load worse. Its own read-only
// handle answers the question readiness actually asks: is the state on disk
// usable?
//
// Unlike the preflight, this runs after startup, so an absent or empty database
// is a failure rather than a fresh boot.
func probeStateStores(ctx context.Context, stateDir string) error {
	sessionPath := filepath.Join(stateDir, SessionDatabaseName)
	if err := probeDatabase(ctx, sessionPath, SessionDatabaseName, func(pool *sql.DB) error {
		tables, err := presentTables(ctx, pool)
		if err != nil {
			return err
		}
		return checkSessionSchema(ctx, pool, tables, selectSnapshot)
	}); err != nil {
		return err
	}
	return probeDatabase(ctx, filepath.Join(stateDir, TaskDatabaseName), TaskDatabaseName,
		func(pool *sql.DB) error {
			tables, err := presentTables(ctx, pool)
			if err != nil {
				return err
			}
			return checkTaskStoreVersion(ctx, pool, tables)
		})
}

// probeDatabase opens one database read-only, verifies its integrity, and runs
// the caller's schema check.
func probeDatabase(ctx context.Context, path, name string, check func(pool *sql.DB) error) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not initialized: %s", name, path)
	}
	pool, err := openSQLite(ctx, path, true)
	if err != nil {
		return fmt.Errorf("open %s read-only: %w", name, err)
	}
	return closeWith(pool, name, errors.Join(quickCheck(ctx, pool, name), check(pool)))
}

// withReadOnlyDatabase opens path read-only and runs inspect, treating an
// absent file as nothing to inspect.
func withReadOnlyDatabase(ctx context.Context, path string, inspect func(pool *sql.DB) error) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	pool, err := openSQLite(ctx, path, true)
	if err != nil {
		// The Python gate collapsed every sqlite3.Error into one message here on
		// purpose: at this point the only useful statement is that the file
		// could not be inspected read-only at all.
		return fmt.Errorf("%s could not be inspected read-only before startup: %w",
			filepath.Base(path), err)
	}
	return closeWith(pool, filepath.Base(path), inspect(pool))
}

// rowQueryer is the read surface the schema checks need. It is an interface so
// one check can run against a pool (readiness) or inside a transaction
// (the task store's own open path).
type rowQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sortedTables returns the table names of a schema description in a stable
// order, so two runs report the same missing column first.
func sortedTables(schema map[string][]string) []string {
	names := make([]string, 0, len(schema))
	for name := range schema {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// stateDirectoryIsWritable reports whether the state directory can still be
// written to.
//
// Go's standard library has no access(2), and inspecting the mode bits would
// lie under an ACL, a read-only mount, or a full filesystem. Creating and
// removing a probe file is the only test that answers the question actually
// being asked. The name is dot-prefixed so it can never be mistaken for state:
// the snapshot tooling publishes visible *.db files and skips dot entries.
func stateDirectoryIsWritable(stateDir string) error {
	probe, err := os.CreateTemp(stateDir, ".readiness-*")
	if err != nil {
		return fmt.Errorf("state directory is not writable: %s", stateDir)
	}
	name := probe.Name()
	if err := errors.Join(probe.Close(), os.Remove(name)); err != nil {
		// Same sentence for every failure mode: the operator's next action is
		// identical whether the create, the close or the unlink is what failed.
		return fmt.Errorf("state directory is not writable: %s", stateDir)
	}
	return nil
}

// SessionDataSourceName builds the DSN ADK's session store must be opened with.
//
// # Both parameters are load-bearing under concurrency
//
// ADK's session service wraps its writes in a transaction that reads first
// (the app and user state rows) and writes after. gorm issues a deferred BEGIN,
// so two concurrent turns both take a read lock and then both try to upgrade —
// which SQLite refuses immediately with SQLITE_BUSY rather than waiting, because
// waiting would deadlock. A busy timeout alone does not help: there is nothing
// to wait for.
//
// _txlock=immediate turns that deferred BEGIN into BEGIN IMMEDIATE, so the write
// lock is taken up front and the second transaction queues instead of failing;
// busy_timeout is what makes it queue rather than give up. Together they are the
// Go equivalent of the Python track's single-connection pool, and without them a
// deployment loses turns as soon as two callers overlap.
func SessionDataSourceName(stateDir string) string {
	return "file:" + filepath.Join(stateDir, SessionDatabaseName) +
		"?_txlock=immediate&_pragma=busy_timeout(5000)"
}
