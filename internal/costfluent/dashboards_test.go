package costfluent

import (
	"context"
	"testing"
)

func TestCreateDashboardReadsItBack(t *testing.T) {
	c, got := testClient(t,
		stub{200, `{"id":"dsh_1","title":"Overview"}`},
		stub{200, `{"id":"dsh_1","title":"Overview","isDefault":true,"dateInterval":"thisMonth","dateBin":"day",
			"widgets":[{"id":"wdg_1","type":"chart","title":"Spend","position":{"x":0,"y":0,"w":6,"h":4}}],
			"createdAt":"2026-09-01T00:00:00Z"}`},
	)

	dash, err := c.CreateDashboard(context.Background(), &CreateDashboardInput{Title: "Overview", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/dashboards")
	wantBody(t, (*got)[0], "isDefault", true)
	wantRequest(t, (*got)[1], "GET", "/v1/dashboards/dsh_1")
	if !dash.IsDefault || dash.Widgets[0].Position.W != 6 || dash.DateBin != "day" {
		t.Fatalf("decoded %+v", dash)
	}
}

func TestUpdateDashboardIsPut(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"dsh_1","title":"Renamed"}`})

	title := "Renamed"
	if _, err := c.UpdateDashboard(context.Background(), "ws_1", "dsh_1", &UpdateDashboardInput{Title: &title}); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "PUT", "/v1/dashboards/dsh_1")
	wantQuery(t, (*got)[0], "workspaceId", "ws_1")
}
