package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kongesque/line-cli/internal/session"
	"github.com/kongesque/line-cli/pkg/line"
)

type addAPI struct {
	session.API
	found     map[string]line.Contact
	friends   []string
	searches  []string
	adds      []addCall
	addErr    error
	searchErr error
}

type addCall struct {
	seq           int64
	mid, searchID string
}

func (f *addAPI) FindContactByUserid(id string) (*line.Contact, error) {
	f.searches = append(f.searches, id)
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	contact, ok := f.found[id]
	if !ok {
		return nil, errors.New("findContactByUserid failed: code 5 message NOT_FOUND")
	}
	return &contact, nil
}

func (f *addAPI) GetAllContactIds() ([]string, error) { return f.friends, nil }

func (f *addAPI) AddFriendByMid(seq int64, mid, searchID string) error {
	f.adds = append(f.adds, addCall{seq, mid, searchID})
	return f.addErr
}

func shopAPI() *addAPI {
	return &addAPI{found: map[string]line.Contact{"@shop": {Mid: lookupMID(7), DisplayName: "Shop", StatusMessage: "Open daily"}}}
}

func TestFindByIDIsReadOnlyAndReportsFriendship(t *testing.T) {
	f := shopAPI()
	a, out, _ := testApp(nil)
	a.Manager.NewClient = func(string) session.API { return f }
	if err := a.Run([]string{"contacts", "--find", "@shop", "--json"}); err != nil {
		t.Fatal(err)
	}
	var got foundContact
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mid != lookupMID(7) || got.DisplayName != "Shop" || got.Friend || got.SearchID != "@shop" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(f.adds) != 0 {
		t.Fatalf("--find must never add: %+v", f.adds)
	}
}

func TestAddSendsOnceWithSearchTracking(t *testing.T) {
	f := shopAPI()
	a, out, _ := testApp(nil)
	a.Manager.NewClient = func(string) session.API { return f }
	if err := a.Run([]string{"add", "@shop", "--json"}); err != nil {
		t.Fatal(err)
	}
	if len(f.adds) != 1 || f.adds[0].mid != lookupMID(7) || f.adds[0].searchID != "@shop" || f.adds[0].seq <= 0 {
		t.Fatalf("unexpected add calls: %+v", f.adds)
	}
	var got addFriendResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "added" || !got.Friend || got.RequestSequence != f.adds[0].seq {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestAddLeavesAnExistingFriendAlone(t *testing.T) {
	f := shopAPI()
	f.friends = []string{lookupMID(7)}
	a, out, _ := testApp(nil)
	a.Manager.NewClient = func(string) session.API { return f }
	if err := a.Run([]string{"add", "@shop"}); err != nil {
		t.Fatal(err)
	}
	if len(f.adds) != 0 || !strings.Contains(out.String(), "Already a friend") {
		t.Fatalf("existing friend was re-added or misreported: adds=%+v out=%s", f.adds, out.String())
	}
}

func TestAddFailureIsNotRetried(t *testing.T) {
	f := shopAPI()
	f.addErr = errors.New("addFriendByMid failed: code 4 message TOO_MANY_REQUESTS")
	a, _, _ := testApp(nil)
	a.Manager.NewClient = func(string) session.API { return f }
	err := a.Run([]string{"add", "@shop"})
	if err == nil || !strings.Contains(err.Error(), "check LINE before retrying") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.adds) != 1 {
		t.Fatalf("add must be attempted exactly once, got %d", len(f.adds))
	}
}

func TestAddUnknownIDNeverMutates(t *testing.T) {
	f := shopAPI()
	a, _, _ := testApp(nil)
	a.Manager.NewClient = func(string) session.API { return f }
	if err := a.Run([]string{"add", "@nobody"}); err == nil {
		t.Fatal("expected an error for an unknown ID")
	}
	if len(f.adds) != 0 {
		t.Fatalf("unknown ID must not add: %+v", f.adds)
	}
}

func TestSearchIDValidation(t *testing.T) {
	for _, args := range [][]string{
		{"add"},
		{"add", "two words"},
		{"add", strings.Repeat("x", 65)},
		{"contacts", "--find", "@shop", "--search", "x"},
	} {
		f := shopAPI()
		a, _, _ := testApp(nil)
		a.Manager.NewClient = func(string) session.API { return f }
		if err := a.Run(args); err == nil {
			t.Fatalf("%v: expected a validation error", args)
		}
		if len(f.searches)+len(f.adds) != 0 {
			t.Fatalf("%v: validation must happen before any request", args)
		}
	}
}
