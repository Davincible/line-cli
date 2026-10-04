package messaging

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kongesque/line-cli/internal/session"
	"github.com/kongesque/line-cli/pkg/line"
)

// chatLog fakes a chat of n plaintext messages with IDs n..1 (newest first) and
// createdTime = id*1000. Paging repeats the cursor message at the top of each
// page, which is what LINE did against a live group chat on 4 October 2026.
type chatLog struct {
	*fakeAPI
	n       int
	recent  []int
	cursors []string
}

func (f *chatLog) msg(id int) *line.Message {
	ts := json.Number(strconv.Itoa(id * 1000))
	return &line.Message{ID: strconv.Itoa(id), From: "u-peer", To: "u-self", Text: fmt.Sprint("m", id), CreatedTime: ts, DeliveredTime: ts}
}

func (f *chatLog) GetRecentMessagesV2(_ string, limit int) ([]*line.Message, error) {
	f.recent = append(f.recent, limit)
	var out []*line.Message
	for id := f.n; id >= 1 && len(out) < limit; id-- {
		out = append(out, f.msg(id))
	}
	return out, nil
}

func (f *chatLog) GetPreviousMessagesV2(_ string, endID string, delivered json.Number, count int) ([]*line.Message, error) {
	f.cursors = append(f.cursors, endID+"@"+delivered.String())
	end, _ := strconv.Atoi(endID)
	var out []*line.Message
	for id := end; id >= 1 && len(out) < count; id-- { // includes the cursor message
		out = append(out, f.msg(id))
	}
	return out, nil
}

func pagingClient(t *testing.T, n int) (*Client, *chatLog) {
	c, f, _, _ := setup(t, true)
	log := &chatLog{fakeAPI: f, n: n}
	c.Session.NewClient = func(string) session.API { return log }
	return c, log
}

func TestHistoryUnder100IsOneRecentCall(t *testing.T) {
	c, log := pagingClient(t, 500)
	got, err := c.History("u-peer", 30)
	if err != nil || len(got) != 30 || got[0].ID != "500" || len(log.cursors) != 0 || log.recent[0] != 30 {
		t.Fatalf("got %d msgs, recent=%v cursors=%v err=%v", len(got), log.recent, log.cursors, err)
	}
}

func TestHistoryPagesBackWithoutDuplicates(t *testing.T) {
	c, log := pagingClient(t, 500)
	got, err := c.History("u-peer", 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 || got[0].ID != "500" || got[249].ID != "251" {
		t.Fatalf("got %d, first %s, last %s", len(got), got[0].ID, got[len(got)-1].ID)
	}
	seen := map[string]bool{}
	for _, m := range got {
		if seen[m.ID] {
			t.Fatal("duplicate", m.ID)
		}
		seen[m.ID] = true
	}
	// cursor is the oldest message held, with its delivered time
	if len(log.cursors) != 2 || log.cursors[0] != "401@401000" || log.cursors[1] != "302@302000" {
		t.Fatalf("cursors %v", log.cursors)
	}
}

func TestHistoryStopsAtStartOfChat(t *testing.T) {
	c, log := pagingClient(t, 150)
	got, err := c.History("u-peer", 5000)
	if err != nil || len(got) != 150 || got[149].ID != "1" {
		t.Fatalf("got %d err %v", len(got), err)
	}
	if len(log.cursors) > 2 {
		t.Fatalf("kept paging after the start: %v", log.cursors)
	}
	c, log = pagingClient(t, 40)
	if got, _ := c.History("u-peer", 5000); len(got) != 40 || len(log.cursors) != 0 {
		t.Fatal("short chat must not page")
	}
}

func TestHistorySinceStopsAtTheBoundary(t *testing.T) {
	c, _ := pagingClient(t, 500)
	got, err := c.HistorySince("u-peer", 10000, 120*1000) // keep ids >= 120
	if err != nil || len(got) != 381 || got[len(got)-1].ID != "120" {
		t.Fatalf("got %d, last %s, err %v", len(got), got[len(got)-1].ID, err)
	}
}

func TestFindMessageReachesPastTheFirstHundred(t *testing.T) {
	c, _ := pagingClient(t, 3000)
	msg, err := c.findMessage("u-peer", "1500")
	if err != nil || msg.ID != "1500" {
		t.Fatalf("msg %v err %v", msg, err)
	}
	if _, err := c.findMessage("u-peer", "900"); err == nil || !strings.Contains(err.Error(), "2000") {
		t.Fatalf("beyond FindDepth must fail and say so: %v", err)
	}
}

func TestHistoryRejectsOutOfRangeLimits(t *testing.T) {
	c, _ := pagingClient(t, 10)
	for _, n := range []int{0, MaxHistory + 1} {
		if _, err := c.History("u-peer", n); err == nil {
			t.Fatal("accepted limit", n)
		}
	}
}

// stuckLog ignores the cursor and always answers with the newest page.
type stuckLog struct{ *chatLog }

func (f *stuckLog) GetPreviousMessagesV2(_ string, _ string, _ json.Number, count int) ([]*line.Message, error) {
	return f.GetRecentMessagesV2("", count)
}

func TestHistoryErrorsWhenLINEIgnoresTheCursor(t *testing.T) {
	c, log := pagingClient(t, 500)
	c.Session.NewClient = func(string) session.API { return &stuckLog{log} }
	if got, err := c.History("u-peer", 300); err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("stuck cursor must fail loudly, got %d messages, err %v", len(got), err)
	}
}

// nilLog answers the first page with only nil messages.
type nilLog struct{ *chatLog }

func (f *nilLog) GetRecentMessagesV2(_ string, limit int) ([]*line.Message, error) {
	return make([]*line.Message, limit), nil
}

func TestHistoryDoesNotPanicOnAnAllNilFirstPage(t *testing.T) {
	c, log := pagingClient(t, 500)
	c.Session.NewClient = func(string) session.API { return &nilLog{log} }
	if got, err := c.History("u-peer", 300); err != nil || len(got) != 0 {
		t.Fatalf("got %d, err %v", len(got), err)
	}
}

func TestHistoryStopsOnAShortPageWithoutAnExtraRequest(t *testing.T) {
	c, log := pagingClient(t, 150) // 100 + one short page of 51 (50 new + cursor)
	if got, err := c.History("u-peer", 5000); err != nil || len(got) != 150 || len(log.cursors) != 1 {
		t.Fatalf("got %d, cursors %v, err %v", len(got), log.cursors, err)
	}
}
