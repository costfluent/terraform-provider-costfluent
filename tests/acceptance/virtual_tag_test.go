package acceptance

import (
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var virtualTagExists = existsByGet((*costfluent.Client).GetVirtualTag)

// The limits in virtual_tag_resource.go are a hand copy of the API's, so a real create and update is
// what would catch the two drifting apart.
func TestAccVirtualTag_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_virtual_tag", virtualTagExists),
		Steps: []resource.TestStep{
			{
				Config: testAccVirtualTagConfig(rName, "unowned", ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_virtual_tag.test", "id"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "key", "team"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "collapsed_tag_keys.#", "2"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.#", "1"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.0.value", "platform"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "default_value", "unowned"),
					testAccRemember("costfluent_virtual_tag.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_virtual_tag.test"),
			{
				Config: testAccVirtualTagConfig(rName, "unassigned", testAccVirtualTagDataRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_virtual_tag.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "default_value", "unassigned"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.#", "2"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.test", "rule.1.value", "data"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, virtualTagExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_virtual_tag", `  key                = "team"
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value  = "platform"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EKS", "Amazon EC2"] }])
  }
`),
		},
	})
}

const testAccVirtualTagDataRule = `
  rule {
    value  = "data"
    filter = jsonencode([{ field = "Tag", tagKey = "app", operator = "StartsWith", values = ["warehouse-"] }])
  }
`

func testAccVirtualTagConfig(name, defaultValue, extraRule string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_virtual_tag" "test" {
  workspace_id       = costfluent_workspace.fixture.id
  key                = "team"
  description        = "Owning team"
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value  = "platform"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EKS", "Amazon EC2"] }])
  }
%s
  default_value = %q
}
`, extraRule, defaultValue)
}
