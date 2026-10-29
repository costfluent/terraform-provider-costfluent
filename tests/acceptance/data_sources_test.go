package acceptance

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The organization lists every workspace the token reaches, the one this test made among them.
func TestAccWorkspacesDataSource_listsTheFixture(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + `
data "costfluent_workspaces" "all" {
  depends_on = [costfluent_workspace.fixture]
}
`,
			Check: resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_workspaces.all", "workspaces.*", map[string]string{
				"name":      rName + "-ws",
				"currency":  "EUR",
				"time_zone": "UTC",
			}),
		}},
	})
}

// The tests running beside this one add and rename connections of their own in the acceptance
// organization, so the read is checked for its contract: it answers with a list.
func TestAccProvidersDataSource_basic(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `data "costfluent_providers" "all" {}`,
			Check:  resource.TestCheckResourceAttrSet("data.costfluent_providers.all", "providers.#"),
		}},
	})
}

// A new workspace holds no cost, so the queries are checked for their contract: the API takes the
// window, the groupings and the filter, and answers with totals and rows.
func TestAccCostDataSources_queryAWorkspace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + fmt.Sprintf(`
data "costfluent_cost_data" "by_provider" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
  granularity  = "month"
  group_by     = ["ProviderName", "ServiceName", "Tag:team"]
  filter       = "costs.provider = 'aws'"
  limit        = 100
}

data "costfluent_cost_data" "usage" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
  group_by     = ["ServiceName"]
  measure      = "usage"
}

data "costfluent_cost_summary" "window" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
}
`, "2026-09-01", "2026-09-30"),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_cost_data.by_provider", "total_records", "0"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.by_provider", "data.#", "0"),
				resource.TestCheckResourceAttrSet("data.costfluent_cost_data.by_provider", "total_cost"),
				resource.TestCheckResourceAttrSet("data.costfluent_cost_summary.window", "total_cost"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.usage", "total_records", "0"),
			),
		}},
	})
}

// The amounts of the two rows the cost test writes. Each one round-trips through float64, but their
// sum through float64 renders 8888889.000000011, not the exact 8888889.00000001, so a total added up
// in floating point is told from the API's.
const (
	testAccCostA     = "1234567.12345678"
	testAccListCostA = "1300000.00000001"
	testAccCostB     = "7654321.87654323"
	testAccListCostB = "7700000.00000001"
	testAccCostTotal = "8888889.00000001"
)

// Two rows written through a connection of the test's own are read back exactly, grouped, filtered,
// by usage and by month, through the workspace that reads the connection, and not at all through
// one that does not.
func TestAccCostDataSources_readTheRowsWritten(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	day := testAccCostDay(time.Now())
	date := day.Format(time.DateOnly)
	monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	serviceA, serviceB := rName+"-a", rName+"-b"
	var providerID string

	resources := testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_workspace" "other" {
  name     = %[1]q
  currency = "EUR"
}

resource "costfluent_provider" "custom" {
  key          = "custom"
  display_name = %[2]q
  credentials  = {}
}

resource "costfluent_workspace_provider" "custom" {
  workspace_id = costfluent_workspace.fixture.id
  provider_id  = costfluent_provider.custom.id
}
`, rName+"-other", rName)

	reads := fmt.Sprintf(`
data "costfluent_cost_data" "by_service" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[1]q
  granularity  = "day"
  group_by     = ["ServiceName"]
}

data "costfluent_cost_data" "filtered" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[1]q
  granularity  = "day"
  group_by     = ["ServiceName"]
  filter       = "costs.service = '%[3]s'"
}

data "costfluent_cost_data" "usage" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[1]q
  group_by     = ["ServiceName"]
  measure      = "usage"
}

data "costfluent_cost_data" "by_month" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[2]q
  end_date     = %[1]q
  granularity  = "month"
  group_by     = ["ServiceName"]
}

data "costfluent_cost_summary" "reads" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[1]q
}

data "costfluent_cost_data" "other" {
  workspace_id = costfluent_workspace.other.id
  start_date   = %[1]q
  end_date     = %[1]q
  group_by     = ["ServiceName"]
}

data "costfluent_cost_summary" "other" {
  workspace_id = costfluent_workspace.other.id
  start_date   = %[1]q
  end_date     = %[1]q
}
`, date, monthStart, serviceA)

	rowA := func(extra map[string]string) map[string]string {
		return testAccWith(map[string]string{"dimensions.ServiceName": serviceA}, extra)
	}
	rowB := func(extra map[string]string) map[string]string {
		return testAccWith(map[string]string{"dimensions.ServiceName": serviceB}, extra)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: resources,
				Check: func(s *terraform.State) error {
					rs, ok := s.RootModule().Resources["costfluent_provider.custom"]
					if !ok {
						return fmt.Errorf("costfluent_provider.custom not in state")
					}
					providerID = rs.Primary.ID
					return nil
				},
			},
			{
				PreConfig: func() {
					row := func(service string, billed, list costfluent.Decimal, quantity float64) costfluent.CostEntry {
						unit, currency := "Hrs", "EUR"
						return costfluent.CostEntry{ChargePeriodStart: date, ChargeCategory: "Usage", ServiceName: service,
							BilledCost: billed, ListCost: &list, ConsumedQuantity: &quantity, ConsumedUnit: &unit,
							BillingCurrency: &currency, LineItemID: &service}
					}
					testAccSubmitCosts(t, providerID, []costfluent.CostEntry{
						row(serviceA, testAccCostA, testAccListCostA, 3.5),
						row(serviceB, testAccCostB, testAccListCostB, 2),
					})
				},
				Config: resources + reads,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.costfluent_cost_data.by_service", "total_records", "2"),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.by_service", "data.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.by_service", "data.*", rowA(map[string]string{
						"cost": testAccCostA, "list_cost": testAccListCostA, "currency": "EUR", "date": date,
					})),
					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.by_service", "data.*", rowB(map[string]string{
						"cost": testAccCostB, "list_cost": testAccListCostB, "currency": "EUR", "date": date,
					})),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.by_service", "total_cost", testAccCostTotal),

					resource.TestCheckResourceAttr("data.costfluent_cost_data.filtered", "total_records", "1"),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.filtered", "data.#", "1"),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.filtered", "data.0.dimensions.ServiceName", serviceA),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.filtered", "data.0.cost", testAccCostA),

					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.usage", "data.*", rowA(map[string]string{
						"quantity": "3.5", "unit": "Hrs",
					})),
					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.usage", "data.*", rowB(map[string]string{
						"quantity": "2", "unit": "Hrs",
					})),

					resource.TestCheckResourceAttr("data.costfluent_cost_data.by_month", "data.#", "2"),
					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.by_month", "data.*", rowA(map[string]string{
						"date": monthStart, "cost": testAccCostA,
					})),
					resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_cost_data.by_month", "data.*", rowB(map[string]string{
						"date": monthStart, "cost": testAccCostB,
					})),

					resource.TestCheckResourceAttr("data.costfluent_cost_summary.reads", "total_cost", testAccCostTotal),

					resource.TestCheckResourceAttr("data.costfluent_cost_data.other", "total_records", "0"),
					resource.TestCheckResourceAttr("data.costfluent_cost_data.other", "data.#", "0"),
					resource.TestCheckResourceAttr("data.costfluent_cost_summary.other", "total_cost", "0.00000000"),
				),
			},
		},
	})
}

// testAccWith is base with extra's keys added.
func testAccWith(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// testAccCostDay is the day the cost test writes its rows on: six days back in UTC, and a day
// further when that is the 1st, so a month's row, dated the 1st, is told from the day's. The client
// smoke tests that run after this suite on the same organization add up every row of three and four
// days back, so this day stays clear of theirs.
func testAccCostDay(now time.Time) time.Time {
	day := now.UTC().AddDate(0, 0, -6)
	if day.Day() == 1 {
		day = day.AddDate(0, 0, -1)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
}

// testAccSubmitCosts writes rows to the custom connection providerID and fails the test unless the
// API accepts every one. The provider has nothing that writes cost rows, so the test writes them
// through the SDK the provider embeds, which waits out a quota refusal as the provider's own calls
// do; the timeout leaves room for a minute's wait. It needs COSTFLUENT_BASE_URL, so the write never
// falls back to the production API.
func testAccSubmitCosts(t *testing.T, providerID string, rows []costfluent.CostEntry) {
	t.Helper()
	if os.Getenv("COSTFLUENT_BASE_URL") == "" {
		t.Fatal("COSTFLUENT_BASE_URL must name the API the cost rows are written to")
	}
	if providerID == "" {
		t.Fatal("no custom connection was remembered to write the cost rows to")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	submitted, err := testAccClient().SubmitCosts(ctx, providerID, &costfluent.SubmitCostsInput{Costs: rows})
	if err != nil {
		t.Fatalf("writing the cost rows: %v", err)
	}
	if submitted.AcceptedRows != len(rows) || submitted.RejectedRows != 0 {
		t.Fatalf("the cost write accepted %d and rejected %d of %d rows: %+v",
			submitted.AcceptedRows, submitted.RejectedRows, len(rows), submitted.Errors)
	}
}
