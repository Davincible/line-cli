package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/kongesque/line-cli/internal/events"
)

func hubAt(t *testing.T, beat time.Time, stopped bool) string {
	dir := t.TempDir()
	t.Setenv("LINE_CLI_EVENTS_DIR", dir)
	if !beat.IsZero() {
		events.WriteHeartbeat(dir, events.Heartbeat{HeartbeatAt: beat, Stopped: stopped})
	}
	return dir
}

func TestOnlyTheHubMayWatchOnceAHubHasRun(t *testing.T) {
	hubAt(t, time.Time{}, false)
	if err := checkHubOwnership("", false); err != nil {
		t.Fatal("no hub yet: plain watch keeps upstream behaviour")
	}
	dir := hubAt(t, time.Now().Add(-time.Hour), true) // a hub ran and is down now
	if err := checkHubOwnership("", false); err == nil || !strings.Contains(err.Error(), "line events") {
		t.Fatalf("plain watch while the hub is down must be refused: %v", err)
	}
	if err := checkHubOwnership(t.TempDir(), false); err == nil {
		t.Fatal("a second hub logging elsewhere must be refused")
	}
	if err := checkHubOwnership("default", false); err != nil {
		t.Fatalf("the hub itself must be allowed: %v", err)
	}
	if err := checkHubOwnership(dir, false); err != nil {
		t.Fatalf("the hub by explicit path must be allowed: %v", err)
	}
	if err := checkHubOwnership("", true); err != nil {
		t.Fatal("--direct must override")
	}
}

func TestLogoutRefusedUnderARunningHub(t *testing.T) {
	hubAt(t, time.Now(), false)
	if err := checkHubBeforeLogout(false); err == nil {
		t.Fatal("logout under a live hub must be refused")
	}
	if err := checkHubBeforeLogout(true); err != nil {
		t.Fatal("--force must override")
	}
	hubAt(t, time.Now().Add(-time.Hour), false)
	if err := checkHubBeforeLogout(false); err != nil {
		t.Fatal("a dead hub must not block logout")
	}
}
