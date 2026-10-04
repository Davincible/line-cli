package session

import (
	"os"
	"path/filepath"
)

func platformSessionLock() (func(), error) { return namedLock("session.lock") }

// Fork: Application Support, not Caches. macOS may purge Caches, and a lock
// file deleted under a running watcher lets a second watcher lock a new one.
func sessionLockDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "line-cli"), nil
}
