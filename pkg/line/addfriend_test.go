package line

import (
	"encoding/json"
	"testing"
)

// The body must match what LINE Chrome 3.7.2 sends after a search by ID.
func TestAddFriendByMidRequestShape(t *testing.T) {
	req := AddFriendByMidRequest{
		ReqSeq:  42,
		UserMid: "u0123",
		Tracking: AddFriendTracking{
			TrackingMetaV2: AddFriendTrackingMeta{SearchID: &AddFriendSearchID{SearchID: "@shop"}},
		},
	}
	body, err := json.Marshal([]interface{}{req})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"reqSeq":42,"userMid":"u0123","tracking":{"reference":"","trackingMetaV2":{"searchId":{"searchId":"@shop"}}}}]`
	if string(body) != want {
		t.Fatalf("got  %s\nwant %s", body, want)
	}
}
