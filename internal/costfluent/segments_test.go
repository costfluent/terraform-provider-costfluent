package costfluent

import (
	"context"
	"testing"
)

func TestListSegmentsDecodesBareArray(t *testing.T) {
	c, got := testClient(t, stub{200, `[{"id":"seg_1","teamLabel":"Platform","product":"API","costCentre":"CC1",
		"isShared":false,"createdAt":"2026-09-01T00:00:00Z"}]`})

	segments, err := c.ListSegments(context.Background(), "ws_1")
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "GET", "/v1/segments")
	wantQuery(t, (*got)[0], "workspaceId", "ws_1")
	if len(segments) != 1 || segments[0].TeamLabel != "Platform" {
		t.Fatalf("decoded %+v", segments)
	}
}
