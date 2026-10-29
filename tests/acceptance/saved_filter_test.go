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

var savedFilterExists = existsByGet((*costfluent.Client).GetSavedFilter)

// testAccAWSFilterDocument is the JSON document the API stores the filter costs.provider = 'aws' as,
// which testAccCostReportJSONFilter encodes.
const testAccAWSFilterDocument = `[{"field":"Provider","operator":"In","values":["aws"]}]`

// A saved filter round-trips through the real API. It is created with the filter as the JSON
// document the API stores, because an import reads that back and cannot know another spelling the
// configuration might use. The update then writes the same filter as CQL, which the provider keeps
// while it means what the API stored. A filter changed outside Terraform then reads back as the
// API's document and plans an update, which writes the configured filter back.
func TestAccSavedFilter_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_saved_filter", savedFilterExists),
		Steps: []resource.TestStep{
			{
				Config: testAccSavedFilterConfig(rName, rName, testAccCostReportJSONFilter),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_saved_filter.test", "id"),
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "title", rName),
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "filter", testAccAWSFilterDocument),
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "is_default", "false"),
					testAccRemember("costfluent_saved_filter.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_saved_filter.test"),
			{
				Config: testAccSavedFilterConfig(rName, rName+" renamed", `"costs.provider = 'aws'"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_saved_filter.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "title", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "filter", "costs.provider = 'aws'"),
				),
			},
			{
				PreConfig: func() {
					gcp := "costs.provider = 'gcp'"
					if _, err := testAccClient().UpdateSavedFilter(context.Background(), created.workspaceID, created.id,
						&costfluent.UpdateSavedFilterInput{Filter: &gcp}); err != nil {
						t.Fatalf("changing the filter outside Terraform: %v", err)
					}
				},
				Config: testAccSavedFilterConfig(rName, rName+" renamed", `"costs.provider = 'aws'"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_saved_filter.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.test", "filter", "costs.provider = 'aws'"),
					func(*terraform.State) error {
						stored, err := testAccClient().GetSavedFilter(context.Background(), created.workspaceID, created.id)
						if err != nil {
							return err
						}
						if stored.Filter != testAccAWSFilterDocument {
							return fmt.Errorf("stored filter = %s, want the apply to have written %s back", stored.Filter,
								testAccAWSFilterDocument)
						}
						return nil
					},
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, savedFilterExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_saved_filter",
				fmt.Sprintf("  title  = %q\n  filter = \"costs.provider = 'aws'\"\n", rName+" default")),
		},
	})
}

// testAccSavedFilterConfig is the saved filter with filter as an HCL expression: a quoted CQL
// string or a jsonencode call.
func testAccSavedFilterConfig(name, title, filter string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_saved_filter" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  filter       = %s
}
`, title, filter)
}
