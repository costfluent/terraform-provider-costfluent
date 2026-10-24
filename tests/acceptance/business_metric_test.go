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

	resource.Test(t, resource.TestCase{
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
