package events

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func ev(rev int, chat string) []byte {
	return []byte(fmt.Sprintf(`{"event":"message","revision":"%d","type":26,"chat_id":"%s","message":{"id":"%d"}}`+"\n", rev, chat, rev))
}

func TestDailyLogIsPrivateRollsOverAndPrunes(t *testing.T) {
	dir := t.TempDir()
	if err := EnsurePrivateDir(filepath.Join(dir, "events")); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(dir, "events")
	now := time.Date(2026, 10, 1, 23, 59, 0, 0, time.Local)
	old := dayFile(dir, now.AddDate(0, 0, -20))
	os.WriteFile(old, ev(1, "Cx"), 0o600)
	l := &DailyLog{Dir: dir, Retain: 14 * 24 * time.Hour, Now: func() time.Time { return now }}
	l.Write(ev(2, "Cx"))
	now = now.Add(2 * time.Minute) // next day
	l.Write(ev(3, "Cx"))
	l.Close()
	files, _ := LogFiles(dir)
	if len(files) != 2 || !strings.HasSuffix(files[0], "2026-10-01.jsonl") || !strings.HasSuffix(files[1], "2026-10-02.jsonl") {
		t.Fatalf("files %v", files)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("a file past retention must be pruned")
	}
	for _, f := range files {
		if info, _ := os.Stat(f); info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v", f, info.Mode().Perm())
		}
	}
	if at, rev := l.Snapshot(); rev != "3" || at.IsZero() {
		t.Fatalf("snapshot %v %s", at, rev)
	}
}

func TestEnsurePrivateDirRefusesAReadableDirectory(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	if err := EnsurePrivateDir(dir); err == nil {
		t.Fatal("must refuse a directory other users can read")
	}
}

func TestReaderReplaysInOrderDedupesAndFilters(t *testing.T) {
	dir := t.TempDir()
	day1 := dayFile(dir, time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local))
	day2 := dayFile(dir, time.Date(2026, 10, 2, 0, 0, 0, 0, time.Local))
	os.WriteFile(day1, append(append(ev(10, "Ca"), ev(11, "Cb")...), ev(11, "Cb")...), 0o600) // crash replay of 11
	os.WriteFile(day2, append(append(ev(12, "Ca"), []byte(`{"event":"resync_required","revision":"13"}`+"\n")...), []byte(`{"event":"mess`)...), 0o600)
	var all, chat bytes.Buffer
	(&Reader{Dir: dir, Out: &all}).Run(context.Background())
	(&Reader{Dir: dir, Chat: "Ca", Out: &chat}).Run(context.Background())
	if got := strings.Count(all.String(), "\n"); got != 4 || strings.Contains(all.String(), `"mess`+"\n") {
		t.Fatalf("all: %d lines\n%s", got, all.String())
	}
	if got := chat.String(); strings.Count(got, "\n") != 3 || strings.Contains(got, `"Cb"`) {
		t.Fatalf("chat filter: %s", got)
	}
	var after bytes.Buffer
	(&Reader{Dir: dir, FromRevision: 11, Out: &after}).Run(context.Background())
	if strings.Count(after.String(), "\n") != 2 {
		t.Fatalf("from-revision: %s", after.String())
	}
}

// Several followers on one live log each see every event exactly once, while
// the hub writes, including across a day rollover and a half-written line.
func TestManyFollowersSeeEveryEventOnce(t *testing.T) {
	dir := t.TempDir()
	WriteHeartbeat(dir, Heartbeat{HeartbeatAt: time.Now()})
	now := time.Date(2026, 10, 1, 23, 59, 59, 0, time.Local)
	var mu sync.Mutex
	l := &DailyLog{Dir: dir, Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return now }}
	ctx, cancel := context.WithCancel(context.Background())
	const readers, events = 5, 40
	outs := make([]*syncBuf, readers)
	var wg sync.WaitGroup
	for i := range outs {
		outs[i] = &syncBuf{}
		r := &Reader{Dir: dir, Follow: true, Poll: 5 * time.Millisecond, Out: outs[i]}
		wg.Add(1)
		go func() { defer wg.Done(); r.Run(ctx) }()
	}
	for i := 1; i <= events; i++ {
		if i == events/2 {
			mu.Lock()
			now = now.Add(2 * time.Second) // midnight
			mu.Unlock()
		}
		line := ev(i, "Ca")
		if i%7 == 0 { // a line that lands in two writes, as a slow disk might show it
			f, _ := os.OpenFile(dayFile(dir, l.Now()), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			f.Write(line[:10])
			time.Sleep(15 * time.Millisecond)
			f.Write(line[10:])
			f.Close()
		} else {
			l.Write(line)
		}
		time.Sleep(2 * time.Millisecond)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		for _, o := range outs {
			if o.lines() < events {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	for i, o := range outs {
		if o.lines() != events {
			t.Fatalf("reader %d saw %d of %d events", i, o.lines(), events)
		}
	}
}

func TestFollowerWarnsOnceWhenTheHubStops(t *testing.T) {
	dir := t.TempDir()
	WriteHeartbeat(dir, Heartbeat{HeartbeatAt: time.Now().Add(-time.Hour), Stopped: true, StopReason: "LINE login changed; restart line watch"})
	warned := 0
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	(&Reader{Dir: dir, Follow: true, Poll: 5 * time.Millisecond, Out: &bytes.Buffer{}, Stale: func(*Heartbeat) { warned++ }}).Run(ctx)
	if warned != 1 {
		t.Fatalf("warned %d times", warned)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) lines() int                  { s.mu.Lock(); defer s.mu.Unlock(); return strings.Count(s.b.String(), "\n") }
