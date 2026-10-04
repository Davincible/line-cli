package session

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitingLockQueuesUntilFree(t *testing.T) {
	busyFor := 3
	try := func() (func(), error) {
		if busyFor > 0 {
			busyFor--
			return nil, ErrBusy
		}
		return func() {}, nil
	}
	unlock, err := WaitingLock(context.Background(), try, 5*time.Second, nil)()
	if err != nil || unlock == nil || busyFor != 0 {
		t.Fatalf("err %v, remaining %d", err, busyFor)
	}
}

func TestWaitingLockGivesUpAndSaysHowLong(t *testing.T) {
	var notice bytes.Buffer
	try := func() (func(), error) { return nil, ErrBusy }
	start := time.Now()
	_, err := WaitingLock(context.Background(), try, 1200*time.Millisecond, &notice)()
	if !errors.Is(err, ErrBusy) || time.Since(start) < time.Second || time.Since(start) > 3*time.Second {
		t.Fatalf("err %v after %s", err, time.Since(start))
	}
	if notice.Len() == 0 {
		t.Fatal("a queued command must say it is waiting")
	}
}

func TestWaitingLockZeroIsUpstreamBehaviour(t *testing.T) {
	calls := 0
	try := func() (func(), error) { calls++; return nil, ErrBusy }
	if _, err := WaitingLock(context.Background(), try, 0, nil)(); !errors.Is(err, ErrBusy) || calls != 1 {
		t.Fatalf("err %v calls %d", err, calls)
	}
}

func TestWaitingLockStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	try := func() (func(), error) { return nil, ErrBusy }
	if _, err := WaitingLock(ctx, try, time.Minute, nil)(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestWaitingLockPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("storage unavailable")
	try := func() (func(), error) { return nil, boom }
	if _, err := WaitingLock(context.Background(), try, time.Minute, nil)(); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
}
