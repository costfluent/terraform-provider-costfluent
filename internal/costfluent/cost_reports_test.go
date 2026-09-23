package costfluent

import (
	"context"
	"testing"
)

func TestCreateCostReportReadsItBack(t *testing.T) {
	c, got := testClient(t,
		stub{200, `{"id":"rpt_1","title":"Monthly"}`},
		stub{200, `{"id":"rpt_1","title":"Monthly","dateInterval":"thisMonth","chartType":"bar","dateBin":"day",
			"settings":{"amortize":true}}`},
	)

	report, err := c.CreateCostReport(context.Background(), &CreateCostReportInput{WorkspaceID: "ws_1", Title: "Monthly"})
	if err != nil {
		t.Fatal(err)
	}
	wantRequest(t, (*got)[0], "POST", "/v1/cost-reports")
	wantRequest(t, (*got)[1], "GET", "/v1/cost-reports/rpt_1")
	wantQuery(t, (*got)[1], "workspaceId", "ws_1")
	if report.ChartType != "bar" || !report.Settings.Amortize {
		t.Fatalf("decoded %+v", report)
	}
}

func TestListCostReportsDecodesFolders(t *testing.T) {
	c, _ := testClient(t, stub{200, `{"reports":[],"folders":[{"id":"fld_1","title":"Teams","reportCount":4}],"total":0}`})

	list, err := c.ListCostReports(context.Background(), "ws_1")
	if err != nil {
		t.Fatal(err)
	}
	if list.Folders[0].Title != "Teams" || list.Folders[0].ReportCount != 4 {
		t.Fatalf("decoded %+v", list)
	}
}
