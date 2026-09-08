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
				Config: testAccBudgetConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_budget.test", "id"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_budget.test", "amount", "1000"),
					resource.TestCheckResourceAttr("costfluent_budget.test", "period", "monthly"),
				),
			},
			// Import
			{
				ResourceName:      "costfluent_budget.test",
				ImportState:       true,
				ImportStateVerify: true,
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
				),
			},
		},
	})
}

func testAccBudgetConfig(name string) string {
	return fmt.Sprintf(`
resource "costfluent_budget" "test" {
  name     = %q
  amount   = 1000
  currency = "USD"
  period   = "monthly"
}
`, name)
}

func testAccBudgetConfigWithAlerts(name string) string {
	return fmt.Sprintf(`
resource "costfluent_budget" "test" {
  name     = %q
  amount   = 5000
  currency = "USD"
  period   = "monthly"

  alerts {
    threshold_percent = 80
  }
  alerts {
    threshold_percent = 100
  }
}
`, name)
}
