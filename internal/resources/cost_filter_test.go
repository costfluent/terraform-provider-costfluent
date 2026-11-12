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

// refusingClient is a client whose API fails every request and reports it, for comparisons that must
// not reach the network.
func refusingClient(t *testing.T) *costfluent.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, `{"message":"no request was expected"}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	return costfluent.NewClient(costfluent.WithBaseURL(srv.URL), costfluent.WithAPIKey("key"))
}

// The API reads a JSON filter's field and operator values in any case and its member names only as
// written, skipping any it does not know, then answers one canonical document: one group as a bare array, values always, no null tag
// key. A configuration that means that document is kept as written without asking the API, so the
// value planned for a create survives apply; one that means another document is drift.
func TestKeepConfiguredFilterKeepsJSONThatMeansTheStoredDocument(t *testing.T) {
	t.Parallel()
	const (
		ec2       = `{"field":"ServiceName","operator":"In","values":["Amazon EC2"]}`
		euWest    = `{"field":"Region","operator":"In","values":["eu-west-1"]}`
		regionNil = `[{"field":"Region","operator":"IsNull","values":[]}]`
		teamTag   = `[{"field":"Tag","operator":"In","values":["platform"],"tagKey":"team"}]`
	)
	tests := []struct {
		name       string
		configured string
		stored     string
		kept       bool
	}{
		{"already canonical", storedFilter, storedFilter, true},
		{"empty", `[]`, `[]`, true},
		{"operator in lower case", `[{"field":"ServiceName","operator":"in","values":["Amazon EC2"]}]`, storedFilter, true},
		{"field in camel case", `[{"field":"serviceName","operator":"In","values":["Amazon EC2"]}]`, storedFilter, true},
		{"one group wrapped in anyOf", `{"anyOf":[[` + ec2 + `]]}`, storedFilter, true},
		{
			"two groups with a lower-case operator",
			`{"anyOf":[[{"field":"ServiceName","operator":"in","values":["Amazon EC2"]}],[` + euWest + `]]}`,
			`{"anyOf":[[` + ec2 + `],[` + euWest + `]]}`,
			true,
		},
		{"null test without values", `[{"field":"Region","operator":"IsNull"}]`, regionNil, true},
		{"null test with null values", `[{"field":"region","operator":"isNull","values":null}]`, regionNil, true},
		{"null tag key", `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"],"tagKey":null}]`, storedFilter, true},
		{"tag key as stored", teamTag, teamTag, true},
		{"member the API does not read", `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"],"note":"x"}]`, storedFilter, true},
		{"tag key member in another case", `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"],"TagKey":"team"}]`, storedFilter, true},
		{"values member in another case", `[{"field":"Region","operator":"IsNull","Values":["x"]}]`, regionNil, true},
		{"values member also in another case", `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"],"Values":["x"]}]`, storedFilter, true},
		{"value in another case", `[{"field":"ServiceName","operator":"In","values":["amazon ec2"]}]`, storedFilter, false},
		{"another field", `[{"field":"Region","operator":"In","values":["Amazon EC2"]}]`, storedFilter, false},
		{"another operator", `[{"field":"ServiceName","operator":"notIn","values":["Amazon EC2"]}]`, storedFilter, false},
		{"clauses in another order", `[` + euWest + `,` + ec2 + `]`, `[` + ec2 + `,` + euWest + `]`, false},
		{"groups in another order", `{"anyOf":[[` + euWest + `],[` + ec2 + `]]}`, `{"anyOf":[[` + ec2 + `],[` + euWest + `]]}`, false},
		{"values in another order", `[{"field":"Region","operator":"In","values":["b","a"]}]`, `[{"field":"Region","operator":"In","values":["a","b"]}]`, false},
		{"two groups against one", `{"anyOf":[[` + ec2 + `],[` + euWest + `]]}`, `[` + ec2 + `]`, false},
		{"tag key in another case", `[{"field":"Tag","operator":"In","values":["platform"],"tagKey":"Team"}]`, teamTag, false},
		{"tag key against none", `[{"field":"Tag","operator":"In","values":["platform"],"tagKey":"team"}]`, `[{"field":"Tag","operator":"In","values":["platform"]}]`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stored := tt.stored

			got, err := keepConfiguredFilter(context.Background(), refusingClient(t), types.StringValue(tt.configured), &stored)
			if err != nil {
				t.Fatal(err)
			}

			want := tt.stored
			if tt.kept {
				want = tt.configured
			}
			if got.ValueString() != want {
				t.Fatalf("filter %s, want %s", got.ValueString(), want)
			}
		})
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
