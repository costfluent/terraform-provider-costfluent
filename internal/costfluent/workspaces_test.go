package costfluent

import (
	"context"
	"testing"
)

func TestCreateWorkspaceReadsItBack(t *testing.T) {
	c, got := testClient(t,
		stub{200, `{"id":"ws_1","name":"Prod"}`},
		stub{200, `{"id":"ws_1","name":"Prod","currency":"EUR","enableCurrencyConversion":false,
			"conversionMethod":"monthEndRate","enableAutomaticSyncing":true,"providerCount":2,
			"createdAt":"2026-09-01T00:00:00Z"}`},
	)

	ws, err := c.CreateWorkspace(context.Background(), &CreateWorkspaceInput{Name: "Prod", Currency: "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/workspaces")
	wantBody(t, (*got)[0], "currency", "EUR")
	wantRequest(t, (*got)[1], "GET", "/v1/workspaces/ws_1")
	if ws.ID != "ws_1" || ws.Currency != "EUR" || ws.ProviderCount != 2 || !ws.EnableAutomaticSyncing {
		t.Fatalf("decoded %+v", ws)
	}
}

func TestUpdateWorkspaceIsPut(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"ws_1","name":"Renamed","currency":"EUR"}`})

	name := "Renamed"
	if _, err := c.UpdateWorkspace(context.Background(), "ws_1", &UpdateWorkspaceInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "PUT", "/v1/workspaces/ws_1")
	wantBody(t, (*got)[0], "name", "Renamed")
	if len((*got)[0].Body) != 1 {
		t.Errorf("unset fields were sent: %v", (*got)[0].Body)
	}
}

func TestListAllWorkspacesFollowsPages(t *testing.T) {
	c, got := testClient(t,
		stub{200, `{"links":{"self":"/v1/workspaces?page=1","next":"/v1/workspaces?page=2"},"data":[{"id":"ws_1"}]}`},
		stub{200, `{"links":{"self":"/v1/workspaces?page=2"},"data":[{"id":"ws_2"}]}`},
	)

	all, err := c.ListAllWorkspaces(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	wantQuery(t, (*got)[1], "page", "2")
	wantQuery(t, (*got)[1], "limit", "1")
	if len(all) != 2 || all[1].ID != "ws_2" {
		t.Fatalf("all %+v", all)
	}
}
