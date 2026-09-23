package costfluent

import (
	"context"
	"testing"
)

func TestCreateBudget(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"bdg_1","name":"Cloud","amount":1000,"currency":"EUR",
		"period":"Monthly","status":"active","currentSpend":250.5,"percentUsed":25.05,
		"spendAvailability":"available","alerts":[{"thresholdPercent":80,"isTriggered":false}],
		"createdAt":"2026-09-01T00:00:00Z"}`})

	segment := "seg_1"
	budget, err := c.CreateBudget(context.Background(), &CreateBudgetInput{
		Name: "Cloud", Amount: 1000, Currency: "EUR", Period: "Monthly",
		Alerts: []BudgetAlertInput{{ThresholdPercent: 80}}, SegmentID: &segment,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/budgets")
	wantBody(t, (*got)[0], "workspaceId", "ws_default")
	wantBody(t, (*got)[0], "segmentId", "seg_1")
	wantBody(t, (*got)[0], "period", "Monthly")
	if budget.ID != "bdg_1" || budget.PercentUsed != 25.05 || budget.SpendAvailability != "available" ||
		budget.Alerts[0].ThresholdPercent != 80 {
		t.Fatalf("decoded %+v", budget)
	}
}

func TestUpdateBudgetIsPut(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"bdg_1","amount":2000}`})

	amount := 2000.0
	if _, err := c.UpdateBudget(context.Background(), "bdg_1", &UpdateBudgetInput{Amount: &amount}); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "PUT", "/v1/budgets/bdg_1")
	wantBody(t, (*got)[0], "amount", 2000.0)
}

func TestListBudgetsWithoutWorkspaceListsOrganization(t *testing.T) {
	c, got := testClient(t, stub{200, `{"links":{"self":"/v1/budgets"},"data":[]}`})
	c = c.Workspace("")

	if _, err := c.ListBudgets(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)[0].Query["workspaceId"]; ok {
		t.Errorf("sent workspaceId %v with no workspace set", (*got)[0].Query["workspaceId"])
	}
}
