package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccCostAlert_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCostAlertConfig(rName, 20, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_cost_alert.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_type", "percentageIncrease"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_value", "20"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "comparison_period", "previousWeek"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "evaluation_frequency_minutes", "360"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "is_paused", "false"),
				),
			},
			{
				ResourceName:      "costfluent_cost_alert.test",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_cost_alert.test"),
				ImportStateVerify: true,
			},
			// Update the threshold and pause, both in place
			{
				Config: testAccCostAlertConfig(rName, 35, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_value", "35"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "is_paused", "true"),
				),
			},
		},
	})
}

func testAccCostAlertConfig(name string, threshold int, paused bool) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_cost_alert" "test" {
  workspace_id                 = costfluent_workspace.fixture.id
  name                         = %q
  threshold_type               = "percentageIncrease"
  threshold_value              = %d
  comparison_period            = "previousWeek"
  evaluation_frequency_minutes = 360
  is_paused                    = %t
}
`, name, threshold, paused)
}
