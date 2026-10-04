//go:build darwin || linux

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

// Lock serializes CLI processes, including login and logout, so a concurrent
// command cannot overwrite rotated tokens or resurrect a deleted session.
var ErrBusy = errors.New("another line command is using the session; retry when it finishes")

func Lock() (func(), error) { return platformSessionLock() }

// WatchLock prevents two consumers from advancing the same event cursor.
func WatchLock() (func(), error) {
	unlock, err := namedLock("watch.lock")
	if errors.Is(err, ErrBusy) {
		return nil, errors.New("another line watch is already running")
	}
	if err == nil {
		heldWatchLock.path, heldWatchLock.id, err = watchLockIdentity()
		if err != nil {
			unlock()
			return nil, err
		}
	}
	return unlock, err
}

// heldWatchLock records which file this process locked, so a long-running
// watcher can notice the file being deleted or replaced under it (fork).
var heldWatchLock struct {
	path string
	id   [2]uint64
}

func watchLockIdentity() (string, [2]uint64, error) {
	dir, err := sessionLockDir()
	if err != nil {
		return "", [2]uint64{}, err
	}
	path := filepath.Join(dir, "watch.lock")
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return path, [2]uint64{}, fmt.Errorf("check watch lock: %w", err)
	}
	return path, [2]uint64{uint64(st.Dev), uint64(st.Ino)}, nil
}

// WatchLockIntact reports an error if the watch lock this process holds is no
// longer the file at its path. A watcher must then stop: another process could
// lock the new file and run a second watcher on the same cursor.
func WatchLockIntact() error {
	if heldWatchLock.path == "" {
		return nil
	}
	_, id, err := watchLockIdentity()
	if err != nil {
		return errors.New("the watch lock file disappeared; stopping so only one watcher can run")
	}
	if id != heldWatchLock.id {
		return errors.New("the watch lock file was replaced; stopping so only one watcher can run")
	}
	return nil
}

func namedLock(name string) (func(), error) {
	dir, err := sessionLockDir()
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "linux" {
		files, err := openSessionFiles(dir, true)
		if err != nil {
			return nil, err
		}
		defer files.root.Close()
		f, err := openPrivateFile(files.root, name, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		return lockOpenedFile(f)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return lockFile(filepath.Join(dir, name))
}

func lockFile(path string) (func(), error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open session lock: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err == nil {
		err = privateFileInfo(info, false)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return lockOpenedFile(f)
}

func lockOpenedFile(f *os.File) (func(), error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("acquire session lock: %w", err)
	}
	return func() { f.Close() }, nil
}
