package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"

	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/a2aserver"
	"github.com/MLOps-Courses/agentops-open-course-go/agents/go/config"
)

// sessionStore makes database ownership explicit around ADK v2.1.0's session
// service. The upstream concrete service owns a GORM handle but exposes no
// Close, so the repository supplies and retains the database/sql pool itself.
type sessionStore struct {
	session.Service

	underlying session.Service
	pool       *sql.DB
	closeErr   error
	closeOnce  sync.Once
}

// openSessionStore opens the persistent session store without migrating it.
// A recoveredState token is required because GORM connects eagerly while it
// builds the service, and that connection must never resolve an unrecovered
// runtime.db pathname.
func openSessionStore(cfg config.Config, recovered recoveredState) (*sessionStore, error) {
	if err := recovered.require(cfg.StateDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating the state directory %s: %w", cfg.StateDir, err)
	}

	pool, err := sql.Open(sqlite.DriverName, a2aserver.SessionDataSourceName(cfg.StateDir))
	if err != nil {
		return nil, fmt.Errorf("opening the session database pool in %s: %w", cfg.StateDir, err)
	}
	// ADK's transactions read app/user state before writing session state. One
	// connection plus BEGIN IMMEDIATE is the bounded serialization contract
	// documented by SessionDataSourceName.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)

	underlying, err := database.NewSessionService(&sqlite.Dialector{Conn: pool})
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("opening the session store in %s: %w", cfg.StateDir, err),
			pool.Close(),
		)
	}
	return &sessionStore{Service: underlying, underlying: underlying, pool: pool}, nil
}

// migrate creates ADK's schema through the concrete service it returned. The
// upstream AutoMigrate intentionally rejects interface wrappers, which is why
// the wrapper retains both the delegated interface and the concrete value.
func (s *sessionStore) migrate(stateDir string) error {
	if err := database.AutoMigrate(s.underlying); err != nil {
		return fmt.Errorf(
			"migrating the session store %s: %w",
			filepath.Join(stateDir, a2aserver.SessionDatabaseName), err,
		)
	}
	return nil
}

// Close releases the exact pool supplied to GORM. It is idempotent so either a
// normal shutdown or an error-unwind can safely converge on the same owner.
func (s *sessionStore) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.pool.Close() })
	return s.closeErr
}

// newSessionService opens and migrates the launcher path's session store.
func newSessionService(cfg config.Config, recovered recoveredState) (*sessionStore, error) {
	sessions, err := openSessionStore(cfg, recovered)
	if err != nil {
		return nil, err
	}
	if err := sessions.migrate(cfg.StateDir); err != nil {
		return nil, errors.Join(err, sessions.Close())
	}
	return sessions, nil
}
