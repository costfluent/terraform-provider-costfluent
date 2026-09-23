package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccFolder_nested(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderConfig(rName, "Child"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_folder.parent", "id"),
					resource.TestCheckResourceAttr("costfluent_folder.parent", "title", rName),
					resource.TestCheckResourceAttr("costfluent_folder.parent", "report_count", "0"),
					resource.TestCheckResourceAttr("costfluent_folder.child", "title", "Child"),
					resource.TestCheckResourceAttrPair(
						"costfluent_folder.child", "parent_id", "costfluent_folder.parent", "id"),
				),
			},
			{
				ResourceName:      "costfluent_folder.child",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_folder.child"),
				ImportStateVerify: true,
			},
			// Rename in place
			{
				Config: testAccFolderConfig(rName, "Renamed"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.child", "title", "Renamed"),
				),
			},
		},
	})
}

func testAccFolderConfig(name, child string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_folder" "parent" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}

resource "costfluent_folder" "child" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  parent_id    = costfluent_folder.parent.id
}
`, name, child)
}

// workspaceScopedImportID builds the "<workspace ID>:<ID>" import identifier from state.
func workspaceScopedImportID(address string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return "", fmt.Errorf("%s not in state", address)
		}
		return rs.Primary.Attributes["workspace_id"] + ":" + rs.Primary.ID, nil
	}
}
