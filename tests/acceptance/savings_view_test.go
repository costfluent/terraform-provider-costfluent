package acceptance

import (
	"context"
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var savingsViewExists = existsByGet((*costfluent.Client).GetSavingsView)

// A savings view round-trips through the real API: the minimum, which the API answers at eight
// decimals, stays as configured, and an import plans nothing. An update replaces the view whole, so
// removing the filters from the configuration clears them in the API, not only in state.
func TestAccSavingsView_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject
	var id string

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_savings_view", savingsViewExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSavingsViewConfig(rName, rName, "toReview", `
  source                 = "awsCostOptimizationHub"
  kind                   = "rightsizing"
  minimum_monthly_saving = "100"
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_savings_view.test", "id"),
					resource.TestCheckResourceAttrPair("costfluent_savings_view.test", "workspace_id", "costfluent_workspace.fixture", "id"),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "title", rName),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "tab", "toReview"),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "source", "awsCostOptimizationHub"),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "kind", "rightsizing"),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "minimum_monthly_saving", "100"),
					resource.TestCheckResourceAttrSet("costfluent_savings_view.test", "created_at"),
					testAccKeepsID("costfluent_savings_view.test", &id),
					testAccRemember("costfluent_savings_view.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_savings_view.test"),
			{
				Config: testAccSavingsViewConfig(rName, rName+" in progress", "inProgress", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_savings_view.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					testAccKeepsID("costfluent_savings_view.test", &id),
					resource.TestCheckResourceAttr("costfluent_savings_view.test", "tab", "inProgress"),
					resource.TestCheckNoResourceAttr("costfluent_savings_view.test", "source"),
					resource.TestCheckNoResourceAttr("costfluent_savings_view.test", "kind"),
					resource.TestCheckNoResourceAttr("costfluent_savings_view.test", "minimum_monthly_saving"),
					func(*terraform.State) error {
						stored, err := testAccClient().GetSavingsView(context.Background(), created.workspaceID, created.id)
						if err != nil {
							return err
						}
						if stored.Source != nil || stored.Kind != nil || stored.MinimumMonthlySaving != nil {
							return fmt.Errorf("stored view = %+v, want the update to have cleared its filters", stored)
						}
						return nil
					},
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, savingsViewExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_savings_view",
				fmt.Sprintf("  title = %q\n  tab   = \"verified\"\n", rName+" default")),
		},
	})
}

func testAccSavingsViewConfig(name, title, tab, filters string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_savings_view" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  tab          = %q
%s}
`, title, tab, filters)
}
