package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWorkspace_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccWorkspaceConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_workspace.test", "id"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "currency", "USD"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "timezone", "UTC"),
				),
			},
			// Import
			{
				ResourceName:      "costfluent_workspace.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update
			{
				Config: testAccWorkspaceConfigUpdated(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.test", "name", rName+"-updated"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "description", "Updated description"),
				),
			},
		},
	})
}

func testAccWorkspaceConfig(name string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "test" {
  name = %q
}
`, name)
}

func testAccWorkspaceConfigUpdated(name string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "test" {
  name        = %q
  description = "Updated description"
}
`, name+"-updated")
}
