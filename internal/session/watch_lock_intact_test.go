//go:build darwin || linux

package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWatchLockNoticesItsFileBeingDeletedOrReplaced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	unlock, err := WatchLock()
	if err != nil {
		t.Skipf("lock dir unavailable in this environment: %v", err)
	}
	defer unlock()
	defer func() { heldWatchLock.path = "" }()
	if err := WatchLockIntact(); err != nil {
		t.Fatalf("fresh lock reported broken: %v", err)
	}
	if _, err := WatchLock(); err == nil {
		t.Fatal("a second watcher must be refused while the first holds the lock")
	}
	os.Remove(heldWatchLock.path)
	if err := WatchLockIntact(); err == nil {
		t.Fatal("a deleted lock file must be noticed")
	}
	os.WriteFile(heldWatchLock.path, nil, 0o600) // a new file at the same path
	if err := WatchLockIntact(); err == nil {
		t.Fatal("a replaced lock file must be noticed")
	}
}
