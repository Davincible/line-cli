package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kongesque/line-cli/internal/events"
	"github.com/kongesque/line-cli/internal/session"
)

func (a *App) watchCommand(args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() {
		fmt.Fprintln(a.Err, "Usage: line watch [--json] [--from-now] [--limit N] [--timeout DURATION] [--log [DIR] --retain DURATION]\nWrites one JSON event per line. Starts now on first use; resumes on later runs.\nWith --log it runs as the hub: events go to daily files that any number of\nline events readers can follow. Only one watcher can run per account.")
		fs.PrintDefaults()
	}
	jsonOutput := fs.Bool("json", true, "write newline-delimited JSON (the watch output format)")
	fromNow := fs.Bool("from-now", false, "discard the saved resume position and start at the current revision")
	limit := fs.Int("limit", 0, "stop after N emitted events; 0 watches continuously")
	timeout := fs.Duration("timeout", 0, "stop after this duration, e.g. 30s; 0 watches continuously")
	logDir := fs.String("log", "", "hub mode: append events to daily files in this directory (\"default\" for the standard location)")
	retain := fs.Duration("retain", 14*24*time.Hour, "hub mode: delete daily files older than this")
	direct := fs.Bool("direct", false, "run even though a hub owns this account's event stream (its log will miss what this consumes)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid options; run line watch --help")
	}
	if fs.NArg() != 0 || *limit < 0 || *timeout < 0 || !*jsonOutput {
		return errors.New("invalid watch arguments; run line watch --help")
	}
	if err := checkHubOwnership(*logDir, *direct); err != nil {
		return err
	}
	parent := a.Context
	if parent == nil {
		parent = context.Background()
	}
	// The hub cancels with a cause when its lock file is lost; Run then stops.
	ctx, cancelCause := context.WithCancelCause(parent)
	defer cancelCause(nil)
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	lock := a.WatchLock
	if lock == nil {
		lock = session.WatchLock
	}
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	fmt.Fprintln(a.Err, "Watching LINE events. Press Ctrl-C to stop.")
	out := a.Out
	var stopHub func(error)
	if *logDir != "" {
		var hubErr error
		out, stopHub, hubErr = a.startHub(ctx, cancelCause, *logDir, *retain)
		if hubErr != nil {
			return hubErr
		}
	}
	w := &events.Watcher{Manager: a.Manager, Lock: a.Lock, Out: out, Err: a.Err, FromNow: *fromNow, Limit: *limit}
	err = w.Run(ctx)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
		err = cause // the hub stopped itself; report why, and exit non-zero so launchd restarts it
	} else if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		err = nil
	}
	if stopHub != nil {
		stopHub(err)
	}
	return err
}

// startHub opens the daily log and starts the heartbeat. stop records why the
// hub ended in hub.json, so readers can tell a crash from a clean stop.
func (a *App) startHub(ctx context.Context, cancel context.CancelCauseFunc, dir string, retain time.Duration) (io.Writer, func(error), error) {
	dir, err := resolveLogDir(dir)
	if err != nil {
		return nil, nil, err
	}
	if err := events.EnsurePrivateDir(dir); err != nil {
		return nil, nil, err
	}
	log := &events.DailyLog{Dir: dir, Retain: retain}
	host, _ := os.Hostname()
	hb := events.Heartbeat{PID: os.Getpid(), Host: host, StartedAt: time.Now().UTC()}
	beat := func() error {
		hb.HeartbeatAt = time.Now().UTC()
		hb.LastEventAt, hb.LastRevision = log.Snapshot()
		return events.WriteHeartbeat(dir, hb)
	}
	if err := beat(); err != nil {
		return nil, nil, fmt.Errorf("write hub heartbeat: %w", err)
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(events.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if err := session.WatchLockIntact(); err != nil {
					cancel(err)
					return
				}
				if err := beat(); err != nil {
					fmt.Fprintln(a.Err, "hub heartbeat failed:", err)
				}
			}
		}
	}()
	fmt.Fprintln(a.Err, "Hub writing events to "+dir)
	stop := func(err error) {
		close(done)
		hb.Stopped = true
		hb.StopReason = "stopped"
		if err != nil {
			hb.StopReason = err.Error()
			notifyHubStopped(err)
		}
		_ = beat()
		_ = log.Close()
	}
	return log, stop, nil
}

func resolveLogDir(dir string) (string, error) {
	if dir == "default" {
		return events.DefaultLogDir()
	}
	return filepath.Abs(dir)
}

// checkHubOwnership refuses any watcher other than the hub once a hub has run
// on this machine (fork). LINE keeps one resume position per account: a plain
// `line watch`, or a hub logging elsewhere, run while the hub is down would
// consume events the hub's log then never sees.
func checkHubOwnership(logDir string, direct bool) error {
	if direct {
		return nil
	}
	hubDir, err := events.DefaultLogDir()
	if err != nil {
		return nil
	}
	h, err := events.ReadHeartbeat(hubDir)
	if err != nil || h == nil {
		return nil // no hub has run here; plain watch keeps upstream behaviour
	}
	if logDir != "" {
		if dir, err := resolveLogDir(logDir); err == nil && dir == hubDir {
			return nil
		}
	}
	return errors.New("a hub owns this account's event stream (" + hubDir + "). Read it with line events --follow; " +
		"another watcher would consume events the hub's log never sees. Pass --direct to run one anyway")
}

// notifyHubStopped raises a macOS notification when the hub ends with an error,
// so a dead hub does not go unnoticed. It names the reason only, never content.
func notifyHubStopped(err error) {
	if runtime.GOOS != "darwin" || os.Getenv("LINE_CLI_NO_NOTIFY") == "1" {
		return
	}
	reason := strings.NewReplacer(`"`, "'", "\\", "/").Replace(err.Error())
	if len(reason) > 180 {
		reason = reason[:180] + "…"
	}
	script := `display notification "` + reason + `" with title "LINE hub stopped" subtitle "launchd restarts it within a minute"`
	_ = exec.Command("/usr/bin/osascript", "-e", script).Run()
}
