package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/kongesque/line-cli/internal/events"
	"github.com/kongesque/line-cli/internal/messaging"
)

// eventsCommand reads the hub's log (fork). It needs no session and no lock:
// any number of readers can follow the same events at once.
func (a *App) eventsCommand(args []string) error {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() {
		fmt.Fprintln(a.Err, "Usage: line events [--follow] [--chat ID] [--from-revision N] [--dir DIR]\n       line events --status\nReads the event log written by the hub (line watch --log). Needs no LINE\nsession and takes no lock, so any number of readers can run at once.")
		fs.PrintDefaults()
	}
	follow := fs.Bool("follow", false, "keep reading as new events arrive")
	chat := fs.String("chat", "", "only message events in this chat (resync_required always passes)")
	from := fs.Int64("from-revision", 0, "only events after this revision")
	dir := fs.String("dir", "", "log directory (default: the hub's standard location)")
	status := fs.Bool("status", false, "print the hub's heartbeat; exit 1 if the hub is not running")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid options; run line events --help")
	}
	if fs.NArg() != 0 || *from < 0 {
		return errors.New("invalid arguments; run line events --help")
	}
	if *chat != "" {
		if err := messaging.ValidateChatID(*chat); err != nil {
			return err
		}
	}
	if *dir == "" {
		d, err := events.DefaultLogDir()
		if err != nil {
			return err
		}
		*dir = d
	}
	h, err := events.ReadHeartbeat(*dir)
	if err != nil {
		return err
	}
	if *status {
		if h == nil {
			return errors.New("no hub has run here; start one with line watch --log default")
		}
		if err := a.json(h); err != nil {
			return err
		}
		if h.Stale(time.Now()) {
			return errors.New("the hub is not running")
		}
		return nil
	}
	if h == nil {
		return errors.New("no hub has run here, so there are no events to read; start one with line watch --log default")
	}
	ctx := a.Context
	if ctx == nil {
		ctx = context.Background()
	}
	r := &events.Reader{Dir: *dir, Chat: *chat, FromRevision: *from, Follow: *follow, Out: a.Out,
		Stale: func(h *events.Heartbeat) {
			reason := "its heartbeat stopped"
			if h != nil && h.StopReason != "" {
				reason = h.StopReason
			}
			fmt.Fprintln(a.Err, "warning: the LINE hub is not running ("+reason+"); no new events will arrive until it restarts")
		}}
	if !*follow && h.Stale(time.Now()) {
		fmt.Fprintln(a.Err, "warning: the LINE hub is not running; this log may be out of date")
	}
	return r.Run(ctx)
}
