package events

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"time"
)

// Reader replays and follows the hub's daily files. It never takes a lock and
// never contacts LINE, so any number can run at once.
type Reader struct {
	Dir          string
	Chat         string // only message events for this chat; "" means all events
	FromRevision int64  // emit only events with a higher revision
	Follow       bool
	Poll         time.Duration
	Out          io.Writer
	Stale        func(*Heartbeat) // called once each time the hub goes stale while following
	Now          func() time.Time
}

type head struct {
	Event    string `json:"event"`
	Revision string `json:"revision"`
	ChatID   string `json:"chat_id"`
}

func (r *Reader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Run emits matching lines in revision order, deduplicating the at-most-one
// replay a hub crash can leave. With Follow it tails today's file and moves to
// the next day's when it appears.
func (r *Reader) Run(ctx context.Context) error {
	if r.Poll <= 0 {
		r.Poll = 500 * time.Millisecond
	}
	last := r.FromRevision
	offsets := map[string]int64{}
	staleReported := false
	for {
		files, err := LogFiles(r.Dir)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, path := range files {
			n, err := r.drain(path, offsets[path], &last)
			if err != nil {
				return err
			}
			offsets[path] = n
		}
		if !r.Follow {
			return nil
		}
		if r.Stale != nil {
			h, _ := ReadHeartbeat(r.Dir)
			if h.Stale(r.now()) {
				if !staleReported {
					r.Stale(h)
					staleReported = true
				}
			} else {
				staleReported = false
			}
		}
		timer := time.NewTimer(r.Poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// drain reads complete lines from offset and returns the new offset. A partial
// last line (the hub mid-write) is left for the next pass.
func (r *Reader) drain(path string, offset int64, last *int64) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) { // pruned between listing and opening
			return offset, nil
		}
		return offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			return offset, nil // incomplete or no line: retry from here later
		}
		if err != nil {
			return offset, err
		}
		offset += int64(len(line))
		var h head
		if json.Unmarshal(line, &h) != nil {
			continue
		}
		rev, err := strconv.ParseInt(h.Revision, 10, 64)
		if err != nil || rev <= *last {
			continue
		}
		*last = rev
		if r.Chat != "" && (h.Event != "message" || h.ChatID != r.Chat) && h.Event != "resync_required" {
			continue
		}
		if _, err := r.Out.Write(line); err != nil {
			return offset, err
		}
	}
}
