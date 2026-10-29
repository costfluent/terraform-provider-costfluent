package acceptance

import (
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var businessMetricExists = existsByGet((*costfluent.Client).GetBusinessMetric)

func TestAccBusinessMetric_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_business_metric", businessMetricExists),
		Steps: []resource.TestStep{
			{
				Config: testAccBusinessMetricConfig(rName, rName, "customer"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_business_metric.test", "id"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "unit", "customer"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "label_columns.0", "region"),
					resource.TestCheckResourceAttrPair(
						"costfluent_cost_report.test", "settings.unit_cost_metric_id", "costfluent_business_metric.test", "id"),
					resource.TestCheckResourceAttr(
						"costfluent_cost_report.test", "settings.unit_cost_label_filter.0.values.0", "eu"),
					testAccRemember("costfluent_business_metric.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_business_metric.test"),
			{
				Config: testAccBusinessMetricConfig(rName, rName+" renamed", "account"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_business_metric.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "name", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "unit", "account"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, businessMetricExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_business_metric",
				fmt.Sprintf("  name = %q\n  unit = \"customer\"\n", rName+" default")),
		},
	})
}

// testAccBusinessMetricConfig also saves a report charting cost per unit of the metric's European
// values, so the report's reference to it and its label filter are applied against the API.
func testAccBusinessMetricConfig(name, metricName, unit string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = %q
  label_columns = ["region"]
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%s per unit"
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.test.id
    unit_cost_label_filter = [{ key = "region", values = ["eu"] }]
  }
}
`, metricName, unit, name)
}

// A gross margin divides revenue less cost by revenue, so its metric is counted in the workspace's
// currency. An update naming the metric but not the calculation keeps the margin, and per unit takes
// a scale.
func TestAccBusinessMetric_unitCostCalculation(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccUnitCostCalculationConfig(rName, "Margin", `unit_cost_calculation = "grossMargin"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"costfluent_business_metric.revenue", "unit", "costfluent_workspace.fixture", "currency"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "grossMargin"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_scale", "1"),
				),
			},
			{
				// The calculation and scale read back as they were set, so the same configuration plans
				// nothing.
				Config:   testAccUnitCostCalculationConfig(rName, "Margin", `unit_cost_calculation = "grossMargin"`),
				PlanOnly: true,
			},
			{
				Config: testAccUnitCostCalculationConfig(rName, "Margin renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", "Margin renamed"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "grossMargin"),
				),
			},
			{
				Config: testAccUnitCostCalculationConfig(rName, "Cost per thousand euros of revenue",
					"unit_cost_calculation = \"perUnit\"\n    unit_cost_scale       = 1000"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "perUnit"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_scale", "1000"),
				),
			},
		},
	})
}

// testAccUnitCostCalculationConfig is a revenue metric in the workspace's currency and a cost report
// charting it, with settings added beside the metric.
func testAccUnitCostCalculationConfig(name, title, settings string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "revenue" {
  workspace_id = costfluent_workspace.fixture.id
  name         = "%s revenue"
  unit         = costfluent_workspace.fixture.currency
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  settings = {
    unit_cost_metric_id   = costfluent_business_metric.revenue.id
    %s
  }
}
`, name, title, settings)
}
