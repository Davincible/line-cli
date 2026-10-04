package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kongesque/line-cli/pkg/line"
)

type flakyAPI struct {
	API
	failures int
	calls    int
	err      error
}

func (f *flakyAPI) GetRecentMessagesV2(string, int) ([]*line.Message, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, f.err
	}
	return nil, nil
}

func retryManager(f *flakyAPI) (*Manager, *[]time.Duration) {
	var waits []time.Duration
	m := NewManager(&memoryStore{state: &State{Version: 1, MID: "u-self", AccessToken: "token"}})
	m.NewClient = func(string) API { return f }
	m.Wait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	return m, &waits
}

func TestReadsRetryTransientErrors(t *testing.T) {
	f := &flakyAPI{failures: 2, err: &line.ResponseError{Status: 503}}
	m, waits := retryManager(f)
	err := m.Do(func(api API) error { _, err := api.GetRecentMessagesV2("c", 1); return err })
	if err != nil || f.calls != 3 || len(*waits) != 2 || (*waits)[0] != time.Second || (*waits)[1] != 4*time.Second {
		t.Fatalf("err=%v calls=%d waits=%v", err, f.calls, *waits)
	}
}

func TestReadsGiveUpAfterThreeAttempts(t *testing.T) {
	f := &flakyAPI{failures: 5, err: &line.ResponseError{Status: 429}}
	m, _ := retryManager(f)
	if err := m.Do(func(api API) error { _, err := api.GetRecentMessagesV2("c", 1); return err }); err == nil || f.calls != 3 {
		t.Fatalf("err=%v calls=%d", err, f.calls)
	}
}

func TestPermanentErrorsAndMutationsAreNotRetried(t *testing.T) {
	f := &flakyAPI{failures: 5, err: errors.New("getRecentMessagesV2 failed: bad request")}
	m, _ := retryManager(f)
	_ = m.Do(func(api API) error { _, err := api.GetRecentMessagesV2("c", 1); return err })
	if f.calls != 1 {
		t.Fatalf("permanent error retried: %d calls", f.calls)
	}
	f = &flakyAPI{failures: 5, err: &line.ResponseError{Status: 503}}
	m, _ = retryManager(f)
	_ = m.Mutate(func(api API) error { _, err := api.GetRecentMessagesV2("c", 1); return err })
	if f.calls != 1 {
		t.Fatalf("mutation replayed: %d calls", f.calls)
	}
}
