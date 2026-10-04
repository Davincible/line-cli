package events

// Fork: the hub. LINE allows one `line watch` per account, and its resume
// position is a single value in the session, so a second listener cannot run.
// `line watch --log DIR` turns the one watcher into a hub: every event is
// appended to a daily file, and any number of readers follow those files with
// `line events`, without touching LINE, the session or its locks.
//
// Files hold decrypted messages. The directory is 0700 and every file 0600,
// matching how the session itself is stored; old days are deleted after the
// retention window.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	logPrefix     = "events-"
	logSuffix     = ".jsonl"
	heartbeatFile = "hub.json"
	// HeartbeatEvery is how often the hub proves it is alive. Readers treat a
	// heartbeat older than StaleAfter as a dead hub.
	HeartbeatEvery = 15 * time.Second
	StaleAfter     = 90 * time.Second
)

// DefaultLogDir is ~/Library/Application Support/line-cli/events on macOS and
// $XDG_CONFIG_HOME/line-cli/events on Linux.
func DefaultLogDir() (string, error) {
	if dir := os.Getenv("LINE_CLI_EVENTS_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "line-cli", "events"), nil
}

// EnsurePrivateDir creates dir 0700 and refuses one that others can read.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by other users; run chmod 700 on it", dir)
	}
	return nil
}

func dayFile(dir string, t time.Time) string {
	return filepath.Join(dir, logPrefix+t.Format("2006-01-02")+logSuffix)
}

// LogFiles lists the daily files oldest first.
func LogFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(name, logPrefix) && strings.HasSuffix(name, logSuffix) {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files, nil
}

// DailyLog is the io.Writer the watcher's JSON encoder writes to. Each Write is
// one event line; it is appended to today's file and synced before Write
// returns, because the watcher checkpoints right after. A crash between the
// two replays at most the last event, which readers deduplicate by revision.
type DailyLog struct {
	Dir    string
	Retain time.Duration
	Now    func() time.Time

	mu      sync.Mutex
	day     string
	file    *os.File
	lastRev string
	lastAt  time.Time
}

func (l *DailyLog) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *DailyLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	day := now.Format("2006-01-02")
	if l.file == nil || day != l.day {
		if l.file != nil {
			l.file.Close()
		}
		f, err := os.OpenFile(dayFile(l.Dir, now), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			l.file = nil
			return 0, err
		}
		l.file, l.day = f, day
		l.prune(now)
	}
	n, err := l.file.Write(p)
	if err == nil {
		err = l.file.Sync()
	}
	if err == nil {
		var head struct {
			Revision string `json:"revision"`
		}
		if json.Unmarshal(p, &head) == nil {
			l.lastRev = head.Revision
		}
		l.lastAt = now
	}
	return n, err
}

// prune deletes daily files older than the retention window. Errors are
// ignored: a file that cannot be deleted now is tried again tomorrow.
func (l *DailyLog) prune(now time.Time) {
	if l.Retain <= 0 {
		return
	}
	cutoff := dayFile(l.Dir, now.Add(-l.Retain))
	files, err := LogFiles(l.Dir)
	if err != nil {
		return
	}
	for _, f := range files {
		if f < cutoff {
			os.Remove(f)
		}
	}
}

func (l *DailyLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Heartbeat is hub.json: proof the hub is alive, and where it has got to.
type Heartbeat struct {
	PID          int       `json:"pid"`
	Host         string    `json:"host"`
	StartedAt    time.Time `json:"started_at"`
	HeartbeatAt  time.Time `json:"heartbeat_at"`
	LastEventAt  time.Time `json:"last_event_at,omitzero"`
	LastRevision string    `json:"last_revision,omitempty"`
	Stopped      bool      `json:"stopped,omitempty"`
	StopReason   string    `json:"stop_reason,omitempty"`
}

// Stale reports whether the hub should be treated as not running.
func (h *Heartbeat) Stale(now time.Time) bool {
	return h == nil || h.Stopped || now.Sub(h.HeartbeatAt) > StaleAfter
}

// WriteHeartbeat replaces hub.json atomically, so a reader never sees half.
func WriteHeartbeat(dir string, h Heartbeat) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hub-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, heartbeatFile))
}

func ReadHeartbeat(dir string) (*Heartbeat, error) {
	data, err := os.ReadFile(filepath.Join(dir, heartbeatFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h Heartbeat
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Snapshot returns what the heartbeat should report about the log.
func (l *DailyLog) Snapshot() (time.Time, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastAt, l.lastRev
}
