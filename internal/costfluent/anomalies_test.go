package costfluent

import (
	"context"
	"testing"
)

func TestListAnomalies(t *testing.T) {
	c, got := testClient(t, stub{200, `{"data":[{"id":"anm_1","providerId":"prv_1","anomalyDate":"2026-09-20",
		"serviceName":"EC2","anomalyType":"spike","severity":"high","expectedCost":10,"actualCost":40,
		"deviationPercent":300,"currency":"EUR","isAcknowledged":false,"detectedAt":"2026-09-21T02:00:00Z"}],
		"totalCount":7,"unacknowledgedCount":3}`})

	list, err := c.ListAnomalies(context.Background(), &ListAnomaliesOptions{Severity: "high", UnacknowledgedOnly: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	g := (*got)[0]
	wantRequest(t, g, "GET", "/v1/anomalies")
	wantQuery(t, g, "severity", "high")
	wantQuery(t, g, "unacknowledgedOnly", "true")
	wantQuery(t, g, "limit", "20")
	if list.TotalCount != 7 || list.UnacknowledgedCount != 3 || list.Data[0].DeviationPercent != 300 ||
		list.Data[0].ProviderID != "prv_1" {
		t.Fatalf("decoded %+v", list)
	}
}

func TestAcknowledgeAnomalySendsNoBody(t *testing.T) {
	c, got := testClient(t, stub{204, ""})

	if err := c.AcknowledgeAnomaly(context.Background(), "anm_1"); err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/anomalies/anm_1/acknowledge")
	if (*got)[0].Body != nil {
		t.Errorf("sent body %v", (*got)[0].Body)
	}
}
