package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccBudget_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
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
					resource.TestCheckResourceAttr("costfluent_budget.test", "period", "Monthly"),
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "spend_availability"),
				),
			},
			// Import: the API does not return the workspace a budget was created in.
			{
				ResourceName:            "costfluent_budget.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"workspace_id"},
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

func TestAccBudget_withAlerts(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccBudgetConfigWithAlerts(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "id"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.#", "2"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.0.threshold_percent", "80"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "alerts.1.threshold_percent", "100"),
				),
			},
		},
	})
}

func testAccBudgetConfig(name string, amount int) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = %d
  currency     = "EUR"
  period       = "Monthly"
}
`, name, amount)
}

func testAccBudgetConfigWithAlerts(name string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_budget" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  amount       = 5000
  currency     = "EUR"
  period       = "Monthly"

  alerts = [
    { threshold_percent = 80 },
    { threshold_percent = 100 },
  ]
}
`, name)
}
