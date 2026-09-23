package costfluent

import (
	"context"
	"testing"
)

func TestQueryCostData(t *testing.T) {
	c, got := testClient(t, stub{200, `{"data":[{"date":"2026-09-01","cost":12.5,"listCost":14,
		"amortizedCost":12.5,"currency":"EUR","dimensions":{"service":"EC2"}}],
		"meta":{"startDate":"2026-09-01","endDate":"2026-09-30","granularity":"Day","totalRecords":1,
		"totalCost":12.5,"currency":"EUR","presentation":{"totalsByCurrency":[{"currency":"EUR","amount":12.5}]}}}`})

	credits := true
	resp, err := c.QueryCostData(context.Background(), &CostDataQuery{
		CostFilterOptions: CostFilterOptions{
			StartDate: "2026-09-01", EndDate: "2026-09-30",
			CloudAccountIDs: []string{"a", "b"}, IncludeCredits: &credits,
		},
		Granularity: "Day", GroupBy: "Service",
	})
	if err != nil {
		t.Fatal(err)
	}
	g := (*got)[0]
	wantRequest(t, g, "GET", "/v1/costs")
	wantQuery(t, g, "startDate", "2026-09-01")
	wantQuery(t, g, "workspaceId", "ws_default")
	wantQuery(t, g, "groupBy", "Service")
	wantQuery(t, g, "includeCredits", "true")
	if len(g.Query["cloudAccountIds"]) != 2 {
		t.Errorf("cloudAccountIds %v", g.Query["cloudAccountIds"])
	}
	if _, sent := g.Query["amortize"]; sent {
		t.Error("sent amortize although it was left to the API default")
	}
	if resp.Data[0].Dimensions["service"] != "EC2" || resp.Meta.TotalCost != 12.5 ||
		resp.Meta.Presentation.TotalsByCurrency[0].Amount != 12.5 {
		t.Fatalf("decoded %+v", resp)
	}
}

func TestGetCostSummary(t *testing.T) {
	c, got := testClient(t, stub{200, `{"totalCost":100,"currency":"EUR","costChangePercent":-5,
		"topServices":[{"serviceName":"EC2","cost":60,"percentage":60}],
		"byProvider":[{"providerId":"prv_1","providerName":"AWS","providerKey":"aws","cost":100}]}`})

	summary, err := c.GetCostSummary(context.Background(), &CostFilterOptions{StartDate: "2026-09-01", EndDate: "2026-09-30"})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "GET", "/v1/costs/summary")
	wantQuery(t, (*got)[0], "endDate", "2026-09-30")
	if summary.TotalCost != 100 || summary.ByProvider[0].ProviderID != "prv_1" || summary.TopServices[0].Percentage != 60 {
		t.Fatalf("decoded %+v", summary)
	}
}
