package datasources_test

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func fakeCostSummary(api *fakeapi.Server) {
	api.Handle("GET /v1/costs/summary", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"totalCost":          "1234.56000000",
			"totalListCost":      "1500.00000000",
			"totalAmortizedCost": "1200.25000000",
			"currency":           "EUR",
			"costChange":         "-100.50000000",
			"costChangePercent":  -7.53,
			"comparedThrough":    "2026-09-28",
			"topServices": []map[string]any{
				{"serviceName": "Amazon EC2", "cost": "800.00000000", "percentage": 64.8},
			},
			"byProvider": []map[string]any{
				{"providerId": awsProvider, "providerName": "AWS prod", "providerKey": "aws", "cost": "1234.56000000"},
			},
		})
	})
}

func TestCostSummaryDataSourceSendsTheRangeAndMapsTotals(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostSummary(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_cost_summary" "s" {
  workspace_id = %q
  start_date   = "2026-09-01"
  end_date     = "2026-09-30"
  filter       = "provider = 'aws'"
}
`, testWorkspace),
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, "/v1/costs/summary", url.Values{
					"workspaceId": {testWorkspace},
					"startDate":   {"2026-09-01"},
					"endDate":     {"2026-09-30"},
					"filter":      {"provider = 'aws'"},
				}),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "total_cost", "1234.56000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "total_list_cost", "1500.00000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "total_amortized_cost", "1200.25000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "cost_change", "-100.50000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "cost_change_percent", "-7.53"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "compared_through", "2026-09-28"),
			),
		}},
	})
}

// The API sends costChangePercent as null when the previous window was not compared, and currency
// as null when the window mixes currencies; both must be null in state, not 0 and "", while a zero
// change stays 0.
func TestCostSummaryDataSourceKeepsNullFiguresNull(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		figures map[string]any
		checks  []resource.TestCheckFunc
	}{
		"not compared, mixed currencies": {
			map[string]any{"currency": nil, "costChange": nil, "costChangePercent": nil},
			[]resource.TestCheckFunc{
				resource.TestCheckNoResourceAttr("data.costfluent_cost_summary.s", "currency"),
				resource.TestCheckNoResourceAttr("data.costfluent_cost_summary.s", "cost_change"),
				resource.TestCheckNoResourceAttr("data.costfluent_cost_summary.s", "cost_change_percent"),
				resource.TestCheckNoResourceAttr("data.costfluent_cost_summary.s", "compared_through"),
			},
		},
		"no change": {
			map[string]any{"currency": "USD", "costChange": "0.00000000", "costChangePercent": 0},
			[]resource.TestCheckFunc{
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "currency", "USD"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "cost_change", "0.00000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_summary.s", "cost_change_percent", "0"),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET /v1/costs/summary", func(fakeapi.Request) fakeapi.Response {
				body := map[string]any{"totalCost": "1234.56000000", "totalListCost": "1500.00000000", "totalAmortizedCost": "1200.25000000"}
				maps.Copy(body, tc.figures)
				return fakeapi.JSON(body)
			})

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_cost_summary" "s" {
  workspace_id = %q
  start_date   = "2026-09-01"
  end_date     = "2026-09-30"
}
`, testWorkspace),
					Check: resource.ComposeAggregateTestCheckFunc(tc.checks...),
				}},
			})
		})
	}
}

// Without workspace_id the summary is scoped to the provider's workspace.
func TestCostSummaryDataSourceFallsBackToTheProviderWorkspace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostSummary(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
data "costfluent_cost_summary" "s" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, "/v1/costs/summary", url.Values{"workspaceId": {testWorkspace}}),
				notSent(api, "/v1/costs/summary", "filter"),
			),
		}},
	})
}

func TestCostSummaryDataSourceRequiresADateRange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_cost_summary" "s" { start_date = "2026-09-01" }`,
			ExpectError: regexp.MustCompile(`The argument "end_date" is required`),
		}},
	})
}

func TestCostSummaryDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/costs/summary", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", "startDate is before the plan's history floor")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_cost_summary" "s" {
  start_date = "2020-01-01"
  end_date   = "2020-01-31"
}
`,
			ExpectError: regexp.MustCompile(`Failed to get cost summary(.|\n)*history floor`),
		}},
	})
}
