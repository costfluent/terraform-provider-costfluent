package datasources_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func fakeCostData(api *fakeapi.Server) {
	api.Handle("GET /v1/costs", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"links": map[string]any{"self": "/v1/costs"},
			"data": []map[string]any{
				{
					"date": "2026-09-01", "cost": "410.25000000", "listCost": "450.00000000", "amortizedCost": "400.10000000", "currency": "EUR",
					"dimensions": map[string]any{"ServiceName": "Amazon EC2", "Tag:team": "platform"},
				},
				{
					"date": "2026-09-01", "cost": "12.50000000", "listCost": "12.50000000", "amortizedCost": "12.50000000", "currency": "EUR",
					"dimensions": map[string]any{"ServiceName": "Amazon S3", "Tag:team": "data"},
				},
			},
			"meta": map[string]any{
				"startDate": "2026-09-01", "endDate": "2026-09-30", "granularity": "monthly",
				"totalRecords": 2, "totalCost": "422.75000000", "currency": "EUR",
			},
		})
	})
}

func TestCostDataDataSourceSendsTheQueryAndMapsRows(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostData(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_cost_data" "c" {
  workspace_id = %q
  start_date   = "2026-09-01"
  end_date     = "2026-09-30"
  granularity  = "monthly"
  group_by     = ["ServiceName", "Tag:team"]
  filter       = "provider = 'aws'"
  limit        = 50
}
`, testWorkspace),
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, "/v1/costs", url.Values{
					"workspaceId": {testWorkspace},
					"startDate":   {"2026-09-01"},
					"endDate":     {"2026-09-30"},
					"granularity": {"monthly"},
					"groupBy":     {"ServiceName", "Tag:team"},
					"filter":      {"provider = 'aws'"},
					"limit":       {"50"},
				}),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.date", "2026-09-01"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.cost", "410.25000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.list_cost", "450.00000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.amortized_cost", "400.10000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.dimensions.ServiceName", "Amazon EC2"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.dimensions.Tag:team", "platform"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.1.dimensions.ServiceName", "Amazon S3"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "total_cost", "422.75000000"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "total_records", "2"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "currency", "EUR"),
			),
		}},
	})
}

// An ungrouped query sends no groupBy and no paging, and the provider's workspace scopes it.
func TestCostDataDataSourceSendsNoUnsetOption(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostData(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// An unset limit asks for the API's largest page of cost rows.
				sentQuery(api, "/v1/costs", url.Values{"workspaceId": {testWorkspace}, "limit": {"5000"}}),
				notSent(api, "/v1/costs", "groupBy", "granularity", "filter", "page", "measure"),
				resource.TestCheckNoResourceAttr("data.costfluent_cost_data.c", "data.0.quantity"),
				resource.TestCheckNoResourceAttr("data.costfluent_cost_data.c", "data.0.unit"),
			),
		}},
	})
}

// A usage query sends its measure, and each row's quantity and unit reach the state.
func TestCostDataDataSourceReadsUsageQuantityAndUnit(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/costs", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"links": map[string]any{"self": "/v1/costs"},
			"data": []map[string]any{{
				"date": "2026-09-01", "cost": "410.25000000", "listCost": "450.00000000", "amortizedCost": "400.10000000", "currency": "EUR",
				"dimensions": map[string]any{"ServiceName": "Amazon EC2"}, "quantity": 730.5, "unit": "Hours",
			}},
			"meta": map[string]any{
				"startDate": "2026-09-01", "endDate": "2026-09-30", "granularity": "day", "measure": "usage",
				"totalRecords": 1, "totalCost": "410.25000000", "currency": "EUR",
			},
		})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  group_by   = ["ServiceName"]
  measure    = "usage"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				sentQuery(api, "/v1/costs", url.Values{"measure": {"usage"}}),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.quantity", "730.5"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.unit", "Hours"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.c", "data.0.cost", "410.25000000"),
			),
		}},
	})
}

func TestCostDataDataSourceRefusesAnUnknownMeasure(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  measure    = "tokens"
}
`,
			ExpectError: regexp.MustCompile(`value must be one of`),
		}},
	})
}

func TestCostDataDataSourceRefusesMoreThanThreeGroupings(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  group_by   = ["Provider", "ServiceName", "Region", "Tag:team"]
}
`,
			ExpectError: regexp.MustCompile(`list must contain at most 3 elements`),
		}},
	})
}

func TestCostDataDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/costs", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", "Segment grouping needs a workspace")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  group_by   = ["Segment"]
}
`,
			ExpectError: regexp.MustCompile(`Failed to query cost data(.|\n)*Segment grouping needs a workspace`),
		}},
	})
}
