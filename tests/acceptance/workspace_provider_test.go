package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// testAccPartnerConnection is the AWS connection seeded into the acceptance organization, with its two member accounts.
const testAccPartnerConnection = "AWS E2E Partner"

// The seeded connection is enabled in a workspace of the test's own for one account, widened to
// both, imported, paused, respelled with padding the API trims, and removed with the workspace. It
// runs on the "Read and write" token a customer is offered, which carries the workspace providers.
func TestAccWorkspaceProvider_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceProviderConfig(rName, `["333333333333"]`, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_workspace_provider.test", "id"),
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "provider_name", testAccPartnerConnection),
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "provider_key", "aws"),
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "account_ids.#", "1"),
					resource.TestCheckTypeSetElemAttr("costfluent_workspace_provider.test", "account_ids.*", "333333333333"),
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "is_enabled", "true"),
					resource.TestCheckResourceAttrSet("costfluent_workspace_provider.test", "added_at"),
				),
			},
			{
				ResourceName:      "costfluent_workspace_provider.test",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_workspace_provider.test"),
				ImportStateVerify: true,
			},
			{
				Config: testAccWorkspaceProviderConfig(rName, `["333333333333", "444444444444"]`, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "account_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr("costfluent_workspace_provider.test", "account_ids.*", "444444444444"),
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "is_enabled", "false"),
				),
			},
			{
				Config: testAccWorkspaceProviderConfig(rName, `[" 333333333333", "444444444444 "]`, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace_provider.test", "account_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr("costfluent_workspace_provider.test", "account_ids.*", " 333333333333"),
					resource.TestCheckTypeSetElemAttr("costfluent_workspace_provider.test", "account_ids.*", "444444444444 "),
				),
			},
		},
	})
}

func testAccWorkspaceProviderConfig(name, accounts string, enabled bool) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
data "costfluent_provider" "partner" {
  display_name = %q
}

resource "costfluent_workspace_provider" "test" {
  workspace_id = costfluent_workspace.fixture.id
  provider_id  = data.costfluent_provider.partner.id
  account_ids  = %s
  is_enabled   = %t
}
`, testAccPartnerConnection, accounts, enabled)
}
