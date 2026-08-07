package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stagingFiles lists the leftover staging files in the state directory. Every
// publication path must leave none behind.
func stagingFiles(t *testing.T, store *Store) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(store.StateDir(), ".incidents-*.tmp"))
	if err != nil {
		t.Fatalf("glob the staging files: %v", err)
	}
	return matches
}

func TestDBPathFailsWhenSeedIsMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := New(Config{
		DataDir:  filepath.Join(root, "missing"),
		StateDir: filepath.Join(root, "state"),
	})

	_, err := store.DBPath()
	if !errors.Is(err, ErrDataAccess) {
		t.Fatalf("expected an ErrDataAccess failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "Seed database is missing") {
		t.Errorf("error does not name the missing seed: %v", err)
	}
	// The full path is in the message on purpose: a missing seed is almost
	// always a misconfigured data directory.
	if !strings.Contains(err.Error(), store.SeedPath()) {
		t.Errorf("error does not carry the seed path %s: %v", store.SeedPath(), err)
	}
}

func TestDBPathKeepsTheWinnerOfAnInitializationRace(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	// A competing worker that publishes first and makes our link fail the way a
	// real lost race does: the destination already exists.
	store.linkFile = func(_, newname string) error {
		if err := os.WriteFile(newname, []byte("winner"), 0o600); err != nil {
			return err
		}
		return &os.LinkError{Op: "link", Err: fs.ErrExist}
	}

	path, err := store.DBPath()
	if err != nil {
		t.Fatalf("a lost race must still return the published path: %v", err)
	}
	if path != store.RuntimePath() {
		t.Errorf("published path = %q, want %q", path, store.RuntimePath())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the published state: %v", err)
	}
	if string(content) != "winner" {
		t.Errorf("the winner's bytes were overwritten: got %q", content)
	}
	if leftovers := stagingFiles(t, store); len(leftovers) > 0 {
		t.Errorf("staging files left behind: %v", leftovers)
	}
}

func TestDBPathWrapsFilesystemFailures(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	store.linkFile = func(_, _ string) error {
		return &os.LinkError{Op: "link", Err: fs.ErrPermission}
	}

	_, err := store.DBPath()
	if !errors.Is(err, ErrDataAccess) {
		t.Fatalf("expected an ErrDataAccess failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "Could not initialize runtime database") {
		t.Errorf("error does not describe the failed initialization: %v", err)
	}
	// The cause survives the wrap, so an operator can tell a permission problem
	// from a full disk.
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("the underlying filesystem error was swallowed: %v", err)
	}
	if leftovers := stagingFiles(t, store); len(leftovers) > 0 {
		t.Errorf("staging files left behind after a failure: %v", leftovers)
	}
}

func TestDBPathIsIdempotentAndCopiesTheSeedFaithfully(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	seed := takeSnapshot(t, store.SeedPath())

	first, err := store.DBPath()
	if err != nil {
		t.Fatalf("publish runtime state: %v", err)
	}
	published := takeSnapshot(t, first)
	if string(published.bytes) != string(seed.bytes) {
		t.Errorf("published state is not a faithful copy of the seed")
	}

	second, err := store.DBPath()
	if err != nil {
		t.Fatalf("republish runtime state: %v", err)
	}
	if second != first {
		t.Errorf("second publication returned %q, want %q", second, first)
	}
	// A second call must not re-copy: the runtime database is live state by
	// then, and overwriting it would discard every audited mutation.
	published.assertUnchanged(t, first)
	seed.assertUnchanged(t, store.SeedPath())
	if leftovers := stagingFiles(t, store); len(leftovers) > 0 {
		t.Errorf("staging files left behind: %v", leftovers)
	}
}
