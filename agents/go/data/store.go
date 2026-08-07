// Package data is the access layer over the committed AgentOps dataset: the
// SQLite incident database and the runbook and log corpus beside it.
//
// Two directories, two contracts, and the separation is the whole point:
//
//   - DataDir holds the committed dataset. It is immutable input. Nothing here
//     ever opens incidents.db read-write from that directory, because the
//     repository gate rebuilds it with a pinned SQLite and compares it byte for
//     byte — one stray write there breaks the build for everyone.
//   - StateDir holds disposable runtime state. The first *write* publishes a
//     copy of the seed into it, and every mutation lands there.
//
// Reads fall back to the seed when no state has been published, so the entire
// read surface works on a fresh checkout with an empty state directory. Only a
// write creates state.
package data

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// databaseName is the file name the committed seed and its runtime copy share.
const databaseName = "incidents.db"

// stateDirPerm is the mode of the runtime state directory. The runtime database
// itself inherits 0600 from the staging file whose inode it links to.
const stateDirPerm = 0o750

// ErrDataAccess marks every failure this package reports and is the Go
// counterpart of Python's DataAccessError. Callers that need to tell "the
// dataset boundary rejected this" apart from any other failure test it with
// errors.Is; the cause wrapped underneath keeps the driver's own message.
var ErrDataAccess = errors.New("data access failed")

// Config is the slice of agent configuration this package needs.
//
// It is a value the caller passes in, never a package-level singleton. The
// Python module read a mutable `settings` global that its tests monkeypatched;
// reproducing that in Go would make every test order-dependent and would make
// two [Store] values pointed at different directories impossible.
type Config struct {
	// DataDir holds the committed dataset — incidents.db, runbooks/, logs/.
	DataDir string
	// StateDir holds disposable runtime state. It is created on first write.
	StateDir string
}

// Store reads the committed dataset and mutates runtime state.
//
// The zero value is not usable; construct one with [New].
type Store struct {
	// The two seams come first only because the fieldalignment analyzer wants
	// the pointer-sized fields at the front of the struct.

	// linkFile publishes runtime state, and is os.Link everywhere but one test.
	//
	// It is a field because the two failures it guards against cannot be
	// provoked otherwise: the window between "the destination does not exist"
	// and "link it into place" is microseconds wide, and a link that fails for
	// filesystem reasons needs a filesystem that refuses to link. Python's
	// tests monkeypatched os.link for exactly these two cases.
	linkFile func(oldname, newname string) error

	// onRestartContext runs inside the restart transaction, immediately after
	// the decision context is read.
	//
	// It exists for one regression test: the guarantee that the context is read
	// *after* BEGIN IMMEDIATE, not before. A competing writer has no other
	// observation point inside another connection's transaction, and the
	// difference is invisible from the outside because prepareRuntimeDatabase's
	// own transaction serializes the two. Production leaves it nil.
	onRestartContext func()

	dataDir  string
	stateDir string
}

// New returns a Store bound to cfg's directories.
func New(cfg Config) *Store {
	return &Store{dataDir: cfg.DataDir, stateDir: cfg.StateDir, linkFile: os.Link}
}

// DataDir returns the directory holding the committed, immutable dataset.
func (s *Store) DataDir() string { return s.dataDir }

// StateDir returns the directory holding disposable runtime state.
func (s *Store) StateDir() string { return s.stateDir }

// SeedPath returns the committed seed database. It is read-only input; use
// [Store.DBPath] for anything that writes.
func (s *Store) SeedPath() string { return filepath.Join(s.dataDir, databaseName) }

// RuntimePath returns where published runtime state lives, whether or not it
// has been published yet.
func (s *Store) RuntimePath() string { return filepath.Join(s.stateDir, databaseName) }

// DBPath returns a writable runtime copy of the committed SQLite seed,
// publishing it on first use.
//
// This is the only function that creates state, which is why every read path
// deliberately avoids it.
func (s *Store) DBPath() (string, error) {
	destination := s.RuntimePath()
	if _, err := os.Stat(destination); err == nil {
		// Already published, and deliberately unverified: this is the cheap
		// path every write takes, and prepareRuntimeDatabase is where the schema
		// is actually checked. A stat error of any kind falls through to
		// publication, which then reports the real problem.
		return destination, nil
	}
	source := s.SeedPath()
	if info, err := os.Stat(source); err != nil || !info.Mode().IsRegular() {
		// The full path, not the base name: a missing seed is almost always a
		// misconfigured AGENT_DATA_DIR, and the operator needs to see where we
		// actually looked.
		return "", fmt.Errorf("%w: Seed database is missing: %s", ErrDataAccess, source)
	}
	if err := os.MkdirAll(s.stateDir, stateDirPerm); err != nil {
		return "", fmt.Errorf("%w: Could not create the runtime state directory %s: %w", ErrDataAccess, s.stateDir, err)
	}
	if err := s.publish(source, destination); err != nil {
		return "", fmt.Errorf("%w: Could not initialize runtime database: %s: %w", ErrDataAccess, destination, err)
	}
	return destination, nil
}

// publish copies the seed into a staging file inside the state directory and
// hard-links it into place.
//
// The hard link is what makes publication safe under a race, because it is an
// exclusive create: two workers starting at once each stage a complete copy,
// exactly one link succeeds, and the loser's EEXIST is a *success* — the
// winner's file is a byte-identical copy of the same seed. os.Rename would be
// wrong here; it would clobber live state the winner may already be writing to.
func (s *Store) publish(source, destination string) (err error) {
	staging, err := os.CreateTemp(s.stateDir, ".incidents-*.tmp")
	if err != nil {
		return fmt.Errorf("stage the seed copy: %w", err)
	}
	name := staging.Name()
	defer func() {
		// The staging file goes away on every path, including the successful
		// one where the state directory now holds a second name for the same
		// inode. A staging file left behind is the failure mode the Python
		// tests assert against explicitly.
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove the staging file %s: %w", name, removeErr))
		}
	}()
	if err := copyInto(staging, source); err != nil {
		_ = staging.Close()
		return err
	}
	// fsync before linking: the link makes the copy visible to every other
	// worker, so the bytes must be durable before the name exists.
	if err := staging.Sync(); err != nil {
		_ = staging.Close()
		return fmt.Errorf("flush the seed copy to disk: %w", err)
	}
	if err := staging.Close(); err != nil {
		return fmt.Errorf("close the seed copy: %w", err)
	}
	if err := s.linkFile(name, destination); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("link the runtime database into place: %w", err)
	}
	return nil
}

// copyInto streams the seed into an already-open staging file.
func copyInto(staging *os.File, source string) error {
	seed, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open the seed database %s: %w", source, err)
	}
	// A read-only handle: a close failure carries no information the copy did
	// not already report.
	defer func() { _ = seed.Close() }()
	if _, err := io.Copy(staging, seed); err != nil {
		return fmt.Errorf("copy the seed database %s: %w", source, err)
	}
	return nil
}
