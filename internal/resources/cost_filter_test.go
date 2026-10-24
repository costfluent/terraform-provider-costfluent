package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const ec2Query = "costs.service = 'Amazon EC2'"

// translateStub answers the API's translate operation for the one CQL filter these tests write,
// and counts the calls.
func translateStub(t *testing.T) (*costfluent.Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cost-filters/translate" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		calls++
		if got := r.URL.Query().Get("filter"); got != ec2Query {
			t.Errorf("translated %q, want %q", got, ec2Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"filter": storedFilter, "query": ec2Query})
	}))
	t.Cleanup(srv.Close)
	return costfluent.NewClient(costfluent.WithBaseURL(srv.URL), costfluent.WithAPIKey("key")), &calls
}

func storedReport(filter string) *costfluent.CostReport {
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return &costfluent.CostReport{ID: "rpt_1", Title: "EC2", Filter: &filter, CreatedAt: &created}
}

// Create, Read and Update all map through here, so a CQL filter that means the stored JSON stays
// as configured: the planned value survives apply and a refresh plans nothing.
func TestMapCostReportKeepsACqlFilterThatMeansTheStoredOne(t *testing.T) {
	t.Parallel()
	client, calls := translateStub(t)
	model := CostReportResourceModel{Filter: types.StringValue(ec2Query)}

	if diags := mapCostReportToModel(context.Background(), client, storedReport(storedFilter), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Filter.ValueString(); got != ec2Query {
		t.Fatalf("filter %s, want the configured %s", got, ec2Query)
	}
	if *calls != 1 {
		t.Fatalf("translate called %d times, want 1", *calls)
	}
}

// A filter changed outside Terraform is drift and shows as the stored JSON.
func TestMapCostReportShowsAFilterChangedOutsideTerraform(t *testing.T) {
	t.Parallel()
	client, _ := translateStub(t)
	changed := `[{"field":"Region","operator":"In","values":["eu-west-1"]}]`
	model := CostReportResourceModel{Filter: types.StringValue(ec2Query)}

	if diags := mapCostReportToModel(context.Background(), client, storedReport(changed), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Filter.ValueString(); got != changed {
		t.Fatalf("filter %s, want the stored %s", got, changed)
	}
}

// An import has no configured value, so it takes the API's JSON and translates nothing.
func TestMapCostReportOnImportTakesTheStoredFilter(t *testing.T) {
	t.Parallel()
	client, calls := translateStub(t)
	model := CostReportResourceModel{Filter: types.StringNull()}

	if diags := mapCostReportToModel(context.Background(), client, storedReport(storedFilter), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Filter.ValueString(); got != storedFilter {
		t.Fatalf("filter %s, want the stored %s", got, storedFilter)
	}
	if *calls != 0 {
		t.Fatalf("translate called %d times, want none", *calls)
	}
}

func TestMapVirtualTagKeepsACqlRuleFilterThatMeansTheStoredOne(t *testing.T) {
	t.Parallel()
	client, _ := translateStub(t)
	model := VirtualTagResourceModel{
		CollapsedTagKeys: types.ListNull(types.StringType),
		Rules:            []VirtualTagRuleModel{{Value: types.StringValue("platform"), Filter: types.StringValue(ec2Query)}},
	}

	if diags := mapVirtualTagToModel(context.Background(), client, storedTag(), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Rules[0].Filter.ValueString(); got != ec2Query {
		t.Fatalf("filter %s, want the configured %s", got, ec2Query)
	}
}

// A translate failure is an error, never a silent switch to the stored value that would plan a
// change the configuration did not make.
func TestMapCostReportFailsWhenTheFilterCannotBeTranslated(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Unknown field (at position 1)"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	client := costfluent.NewClient(costfluent.WithBaseURL(srv.URL), costfluent.WithAPIKey("key"))
	model := CostReportResourceModel{Filter: types.StringValue(ec2Query)}

	if diags := mapCostReportToModel(context.Background(), client, storedReport(storedFilter), &model); !diags.HasError() {
		t.Fatal("want an error diagnostic")
	}
}
