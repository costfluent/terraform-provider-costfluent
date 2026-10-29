package acceptance

import (
	"fmt"
	"os"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var subscriptionExists = existsByGet((*costfluent.Client).GetCostReportSubscription)

func TestAccCostReportSubscription_lifecycle(t *testing.T) {
	appID := os.Getenv("COSTFLUENT_ACC_APP_ID")
	if appID == "" {
		t.Skip("COSTFLUENT_ACC_APP_ID names no email channel")
	}
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report_subscription", subscriptionExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSubscriptionConfig(rName, appID, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("costfluent_cost_report_subscription.test", "cost_report_id",
						"costfluent_cost_report.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "delivery_hour", "9"),
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "time_zone", "UTC"),
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "is_active", "true"),
					testAccRemember("costfluent_cost_report_subscription.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report_subscription.test"),
			{
				Config: testAccSubscriptionConfig(rName, appID, "delivery_hour = 7\n  time_zone = \"Europe/Berlin\"\n  is_active = false"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "delivery_hour", "7"),
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "time_zone", "Europe/Berlin"),
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "is_active", "false"),
				),
			},
			{
				// The API stores the zone trimmed; state keeps the configured spelling.
				Config: testAccSubscriptionConfig(rName, appID, "delivery_hour = 7\n  time_zone = \" Europe/Berlin \"\n  is_active = false"),
				Check:  resource.TestCheckResourceAttr("costfluent_cost_report_subscription.test", "time_zone", " Europe/Berlin "),
			},
			{
				Config: testAccCostReportConfig(rName, rName, "last30Days", "null", "null"),
				Check:  testAccCheckRemoved(&created, subscriptionExists),
			},
			testAccDefaultWorkspaceStep(testAccCostReportConfig(rName, rName, "last30Days", "null", "null"),
				"costfluent_cost_report_subscription", fmt.Sprintf(`  cost_report_id = costfluent_cost_report.test.id
  name           = %q
  schedule_type  = "weekly"
  app_ids        = [%q]
`, rName+" default", appID)),
		},
	})
}

func testAccSubscriptionConfig(name, appID, extra string) string {
	return testAccCostReportConfig(name, name, "last30Days", "null", "null") + fmt.Sprintf(`
resource "costfluent_cost_report_subscription" "test" {
  workspace_id   = costfluent_workspace.fixture.id
  cost_report_id = costfluent_cost_report.test.id
  name           = %q
  schedule_type  = "weekly"
  app_ids        = [%q]
  %s
}
`, name, appID, extra)
}
