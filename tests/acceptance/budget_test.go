package acceptance

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccBudget_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccBudgetConfig(rName, 1000),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "id"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_budget.test", "amount", "1000"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "currency", "EUR"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "interval_count", "1"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "interval_unit", "month"),
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "start_date"),
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "state"),
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "spend_availability"),
					// A new workspace has no spend in another currency, and a budget starting today
					// starts after the plan's history floor.
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "unconverted.#"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "unconverted.#", "0"),
					resource.TestCheckNoResourceAttr("costfluent_budget.test", "history_floor"),
					// Omitted alerts are the API's defaults.
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.#", "3"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.threshold_percent", "50"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.2.threshold_percent", "100"),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
			// Spend is computed on read, so the create response's availability is older than the
			// import's.
			{
				ResourceName:            "costfluent_budget.test",
				ImportState:             true,
				ImportStateIdFunc:       workspaceScopedImportID("costfluent_budget.test"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"spend_availability", "state"},
			},
			// Update in place
			{
				Config: testAccBudgetConfig(rName, 2000),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "amount", "2000"),
				),
			},
		},
	})
}

// A budget with an amount per period on a quarterly cadence reads back its periods as configured,
// imports with nothing to change, and takes new amounts in place.
func TestAccBudget_periods(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigWithPeriods(rName, "3000"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_budget.test", "amount"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "start_date", "2026-01-01"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "interval_count", "3"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "periods.#", "2"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "periods.0.amount", "3000"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "periods.1.end_date", "2026-06-30"),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
			{
				Config: testAccBudgetConfigWithPeriods(rName, "3500"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "periods.0.amount", "3500"),
				),
			},
		},
	})
}

// Thresholds change in place: the same budget takes a forecast threshold and drops a spend one,
// reads them back in the API's order, and imports with nothing to change.
func TestAccBudget_withAlerts(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigWithAlerts(rName, `
    { threshold_percent = 80 },
    { threshold_percent = 100 },`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "id"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.#", "2"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.threshold_percent", "80"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.basis", "actual"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.1.threshold_percent", "100"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			// Configured alerts are in the order the API returns, so an import changes nothing.
			testAccImportPlansNothing("costfluent_budget.test"),
			{
				Config: testAccBudgetConfigWithAlerts(rName, `
    { threshold_percent = 100 },
    { threshold_percent = 90, basis = "forecast" },`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.#", "2"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.threshold_percent", "100"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.basis", "actual"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.1.threshold_percent", "90"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.1.basis", "forecast"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
		},
	})
}

// The budget GET is organization-wide, so an import ID naming another workspace is refused by the
// workspace list check rather than imported under the wrong workspace_id.
func TestAccBudget_importRefusesAnotherWorkspace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfig(rName, 1000) + fmt.Sprintf(`
resource "costfluent_workspace" "other" {
  name     = %q
  currency = "EUR"
}
`, rName+"-other"),
			},
			{
				ResourceName:    "costfluent_budget.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					return s.RootModule().Resources["costfluent_workspace.other"].Primary.ID + ":" +
						s.RootModule().Resources["costfluent_budget.test"].Primary.ID, nil
				},
				ExpectError: regexp.MustCompile(`Budget not in workspace`),
			},
		},
	})
}

// The "Read and write" acceptance token cannot create a notification channel, so this takes an
// existing one of the organization the API key belongs to.
func TestAccBudget_channels(t *testing.T) {
	appID := os.Getenv("COSTFLUENT_ACC_APP_ID")
	if appID == "" {
		t.Skip("COSTFLUENT_ACC_APP_ID names no notification channel")
	}
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigWithChannels(rName, fmt.Sprintf("app_ids = [%q]", appID)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "app_ids.#", "1"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "app_ids.0", appID),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
			// Removing the channels clears them on the same budget.
			{
				Config: testAccBudgetConfigWithChannels(rName, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_budget.test", "app_ids.#"),
				),
			},
		},
	})
}

// A budget on a saved report counts what the report counts; pointing it at another report, or at
// none, moves the same budget in place.
func TestAccBudget_onAReport(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigOnReport(rName, "first"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("costfluent_budget.test", "cost_report_id", "costfluent_cost_report.first", "id"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "spend_availability", "computed"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
			{
				Config: testAccBudgetConfigOnReport(rName, "second"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("costfluent_budget.test", "cost_report_id", "costfluent_cost_report.second", "id"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			{
				Config: testAccBudgetConfigOnReport(rName, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_budget.test", "cost_report_id"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			{Config: testAccBudgetConfigOnReport(rName, ""), PlanOnly: true},
		},
	})
}

// A usage budget counts one unit on a saved report's rows: it plans, applies and imports with
// nothing to change, keeps the currency every budget carries, and takes a new unit in place.
func TestAccBudget_usage(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigUsage(rName, "Tokens"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "measure", "usage"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "usage_unit", "Tokens"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "amount", "5000000"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "currency", "EUR"),
					resource.TestCheckResourceAttrPair("costfluent_budget.test", "cost_report_id", "costfluent_cost_report.usage", "id"),
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "spend_availability"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			testAccImportPlansNothing("costfluent_budget.test"),
			{
				ResourceName:            "costfluent_budget.test",
				ImportState:             true,
				ImportStateIdFunc:       workspaceScopedImportID("costfluent_budget.test"),
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"spend_availability", "state"},
			},
			{
				Config: testAccBudgetConfigUsage(rName, "GPU-Hours"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.test", "usage_unit", "GPU-Hours"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "amount", "5000000"),
					testAccKeepsID("costfluent_budget.test", &id),
				),
			},
			{Config: testAccBudgetConfigUsage(rName, "GPU-Hours"), PlanOnly: true},
		},
	})
}

func testAccBudgetConfigUsage(name, unit string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_cost_report" "usage" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%[1]s usage"
}

resource "costfluent_budget" "test" {
  workspace_id   = costfluent_workspace.fixture.id
  name           = %[1]q
  amount         = "5000000"
  currency       = "EUR"
  measure        = "usage"
  usage_unit     = %[2]q
  cost_report_id = costfluent_cost_report.usage.id
}
`, name, unit)
}

// An empty list is not sent, so the API would give the budget its default thresholds.
func TestAccBudget_emptyAlertsRefused(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = 1000
  currency     = "EUR"
  alerts       = []
}
`, rName),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`list must contain at least 1 elements`),
		}},
	})
}

// testAccBudgetConfigOnReport is the budget on the report named first or second, or on the whole
// workspace when report is empty.
func testAccBudgetConfigOnReport(name, report string) string {
	onReport := ""
	if report != "" {
		onReport = fmt.Sprintf("  cost_report_id = costfluent_cost_report.%s.id\n", report)
	}
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_cost_report" "first" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%[1]s first"
}

resource "costfluent_cost_report" "second" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%[1]s second"
}

resource "costfluent_budget" "test" {
  workspace_id   = costfluent_workspace.fixture.id
  name           = %[1]q
  amount         = 1000
  currency       = "EUR"
%[2]s}
`, name, onReport)
}

func testAccBudgetConfigWithChannels(name, channels string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = 1000
  currency     = "EUR"
  %s
}
`, name, channels)
}

func testAccBudgetConfig(name string, amount int) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = %d
  currency     = "EUR"
}
`, name, amount)
}

func testAccBudgetConfigWithAlerts(name, alerts string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = 5000
  currency     = "EUR"

  alerts = [%s
  ]
}
`, name, alerts)
}

func testAccBudgetConfigWithPeriods(name, firstQuarter string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id   = costfluent_workspace.fixture.id
  name           = %q
  currency       = "EUR"
  start_date     = "2026-01-01"
  interval_count = 3
  interval_unit  = "month"
  periods = [
    { start_date = "2026-01-01", end_date = "2026-03-31", amount = %q },
    { start_date = "2026-04-01", end_date = "2026-06-30", amount = "4000" },
  ]
}
`, name, firstQuarter)
}
