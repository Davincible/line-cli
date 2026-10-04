//go:build !darwin && !linux && !windows

package session

import "errors"

var ErrBusy = errors.New("session is busy")

func WatchLock() (func(), error) { return nil, errKeychainUnsupported }

func Lock() (func(), error) { return nil, errKeychainUnsupported }

// WatchLockIntact is a no-op where the fork has not added lock-file checks.
func WatchLockIntact() error { return nil }
