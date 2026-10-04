package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// DefaultLockWait is how long a command queues for the session lock before
// giving up. Fork: upstream failed at once with ErrBusy, so two agents reading
// at the same moment, or a read during the watcher's short lock, errored.
const DefaultLockWait = 60 * time.Second

// LockWaitFromEnv reads LINE_CLI_LOCK_WAIT (a Go duration; "0" restores
// upstream's fail-at-once behaviour). Invalid values fall back to the default.
func LockWaitFromEnv() time.Duration {
	if v := os.Getenv("LINE_CLI_LOCK_WAIT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return DefaultLockWait
}

// WaitingLock wraps a non-blocking lock so callers queue for it. It polls,
// because flock(LOCK_EX) without LOCK_NB cannot be cancelled by ctx. It says
// once on notice that it is waiting, so a queued command never looks hung.
func WaitingLock(ctx context.Context, try func() (func(), error), wait time.Duration, notice io.Writer) func() (func(), error) {
	return func() (func(), error) {
		deadline := time.Now().Add(wait)
		delay := 50 * time.Millisecond
		announced := false
		start := time.Now()
		for {
			unlock, err := try()
			if !errors.Is(err, ErrBusy) {
				return unlock, err
			}
			if !time.Now().Before(deadline) {
				return nil, fmt.Errorf("%w (waited %s)", ErrBusy, wait.Round(time.Second))
			}
			if !announced && notice != nil && time.Since(start) >= 500*time.Millisecond {
				fmt.Fprintln(notice, "Waiting for another line command to finish…")
				announced = true
			}
			timer := time.NewTimer(min(delay, time.Until(deadline)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			delay = min(delay*2, 500*time.Millisecond)
		}
	}
}
