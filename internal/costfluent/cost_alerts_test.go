package costfluent

import (
	"context"
	"testing"
)

const costAlertJSON = `{"id":"cal_1","name":"Spike","providerIds":["prv_1"],"filter":"service = 'EC2'",
	"thresholdType":"percentageIncrease","thresholdValue":20,"comparisonPeriod":"previousWeek",
	"appIds":[],"evaluationFrequencyMinutes":60,"status":"active","createdAt":"2026-09-01T00:00:00Z"}`

func TestCreateCostAlertReadsItBack(t *testing.T) {
	c, got := testClient(t, stub{200, `{"id":"cal_1","name":"Spike"}`}, stub{200, costAlertJSON})

	period := CostAlertComparePreviousWeek
	alert, err := c.CreateCostAlert(context.Background(), &CreateCostAlertInput{
		Name: "Spike", ThresholdType: CostAlertThresholdPercentageIncrease, ThresholdValue: 20,
		ComparisonPeriod: &period, ProviderIDs: []string{"prv_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/cost-alerts")
	wantBody(t, (*got)[0], "workspaceId", "ws_default")
	wantBody(t, (*got)[0], "thresholdType", "percentageIncrease")
	wantRequest(t, (*got)[1], "GET", "/v1/cost-alerts/cal_1")
	wantQuery(t, (*got)[1], "workspaceId", "ws_default")
	if alert.ID != "cal_1" || alert.ProviderIDs[0] != "prv_1" || *alert.ComparisonPeriod != "previousWeek" ||
		alert.EvaluationFrequencyMinutes != 60 {
		t.Fatalf("decoded %+v", alert)
	}
}

func TestUpdateCostAlertIsPutThenGet(t *testing.T) {
	c, got := testClient(t, stub{204, ""}, stub{200, costAlertJSON})

	value := 30.0
	if _, err := c.UpdateCostAlert(context.Background(), "ws_1", "cal_1", &UpdateCostAlertInput{ThresholdValue: &value}); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "PUT", "/v1/cost-alerts/cal_1")
	wantQuery(t, (*got)[0], "workspaceId", "ws_1")
	wantBody(t, (*got)[0], "thresholdValue", 30.0)
	wantRequest(t, (*got)[1], "GET", "/v1/cost-alerts/cal_1")
}

func TestPauseCostAlert(t *testing.T) {
	c, got := testClient(t, stub{204, ""})

	if err := c.PauseCostAlert(context.Background(), "ws_1", "cal_1"); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/cost-alerts/cal_1/pause")
	wantQuery(t, (*got)[0], "workspaceId", "ws_1")
}
