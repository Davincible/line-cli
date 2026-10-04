package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kongesque/line-cli/internal/messaging"
	"github.com/kongesque/line-cli/internal/session"
	"github.com/kongesque/line-cli/pkg/line"
)

type messageAPI struct {
	session.API
	history []*line.Message
	sent    *line.Message
}

func (f *messageAPI) GetRecentMessagesV2(string, int) ([]*line.Message, error) { return f.history, nil }
func (f *messageAPI) GetBlockedContactIds() ([]string, error)                  { return nil, nil }
func (f *messageAPI) SendMessage(_ int64, msg *line.Message) (*line.Message, error) {
	f.sent = msg
	return &line.Message{ID: "message-id"}, nil
}

func TestMessageArgumentsValidateBeforeSessionAccess(t *testing.T) {
	for _, args := range [][]string{
		{"messages", "--help"}, {"send", "--help"}, {"messages"},
		{"messages", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--limit", "0"}, {"messages", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--limit", "10001"},
		{"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, {"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--text", ""},
		{"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--text", "hello", "--stdin"},
		{"send", "bad\nname", "--text", "hello"},
		{"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--text", "hello", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, _, _ := testApp(nil)
			a.Lock = func() (func(), error) { t.Fatal("session opened for invalid arguments"); return nil, nil }
			_ = a.Run(args)
		})
	}
}

func TestSendStdinPreservesMultilineTextAndJSONOutput(t *testing.T) {
	a, out, diagnostics := testApp(nil)
	f := &messageAPI{}
	a.Manager.Store = &testStore{state: &session.State{MID: "u-self", AccessToken: "token", NoE2EE: true}}
	a.Manager.NewClient = func(string) session.API { return f }
	a.In = strings.NewReader("hello\nworld\n")
	if err := a.Run([]string{"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--stdin", "--json"}); err != nil {
		t.Fatal(err)
	}
	var result messaging.SendResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != "message-id" || result.Encrypted || f.sent.Text != "hello\nworld\n" || diagnostics.Len() != 0 {
		t.Fatal("bad stdin send")
	}
}

func TestHistoryPartialFailureStillReturnsStructuredJSON(t *testing.T) {
	a, out, _ := testApp(nil)
	f := &messageAPI{history: []*line.Message{{ID: "plain", Text: "hello"}, {ID: "encrypted", ContentMetadata: map[string]string{"e2eeVersion": "2"}}}}
	a.Manager.NewClient = func(string) session.API { return f }
	err := a.Run([]string{"messages", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--json"})
	if err == nil {
		t.Fatal("missing partial failure status")
	}
	var result []messaging.Message
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].Text != "hello" || result[1].Status != "decryption_failed" || result[1].Text != "" {
		t.Fatal("invalid partial history output")
	}
}

func TestSendStdinHasBoundedSize(t *testing.T) {
	a, _, _ := testApp(nil)
	a.In = strings.NewReader(strings.Repeat("x", messaging.MaxTextUnits*4+1))
	a.Lock = func() (func(), error) { t.Fatal("session opened before input validation"); return nil, nil }
	if err := a.Run([]string{"send", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--stdin"}); err == nil {
		t.Fatal("accepted oversized stdin")
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]int64{
		"":                     0,
		"72h":                  now.Add(-72 * time.Hour).UnixMilli(),
		"30d":                  now.AddDate(0, 0, -30).UnixMilli(),
		"2026-09-01T00:00:00Z": time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	} {
		if got, err := parseSince(in, now); err != nil || got != want {
			t.Errorf("%q: got %d want %d err %v", in, got, want, err)
		}
	}
	if got, err := parseSince("2026-09-01", now); err != nil || got != time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local).UnixMilli() {
		t.Errorf("date: %d %v", got, err)
	}
	for _, bad := range []string{"yesterday", "-3d", "0d", "-1h"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

type countingAPI struct {
	messageAPI
	limits []int
}

func (f *countingAPI) GetRecentMessagesV2(_ string, limit int) ([]*line.Message, error) {
	f.limits = append(f.limits, limit)
	return nil, nil
}

func TestSinceWithoutLimitIsNotCappedAtTwenty(t *testing.T) {
	for args, want := range map[string]int{"--since 7d": 100, "--since 7d --limit 30": 30, "": 20} {
		a, _, _ := testApp(nil)
		f := &countingAPI{}
		a.Manager.Store = &testStore{state: &session.State{MID: "u-self", AccessToken: "token", NoE2EE: true}}
		a.Manager.NewClient = func(string) session.API { return f }
		run := append([]string{"messages", "Uaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--json"}, strings.Fields(args)...)
		if err := a.Run(run); err != nil {
			t.Fatal(args, err)
		}
		if len(f.limits) != 1 || f.limits[0] != want {
			t.Errorf("%q: first page asked for %v, want %d", args, f.limits, want)
		}
	}
}
