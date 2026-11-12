package acceptance

import (
	"context"
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// providerExists reads a connection by ID; connections belong to the organization, so the read
// takes no workspace.
var providerExists = existsByGet(func(c *costfluent.Client, ctx context.Context, _, id string) (*costfluent.Provider, error) {
	return c.GetProvider(ctx, id)
})

// A custom connection needs no cloud, so it shows on the local stack that renaming a connection and
// removing its description update the same connection rather than replacing it.
func TestAccProvider_renamedInPlace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string
	config := func(name, extra string) string {
		return fmt.Sprintf(`
resource "costfluent_provider" "test" {
  key          = "custom"
  display_name = %q
  credentials  = {}
%s}
`, name, extra)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_provider", providerExists),
		Steps: []resource.TestStep{
			{
				Config: config(rName, "  description = \"Billing exports\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "display_name", rName),
					testAccKeepsID("costfluent_provider.test", &id),
				),
			},
			{
				Config: config(rName+" renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_provider.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.test", "display_name", rName+" renamed"),
					resource.TestCheckNoResourceAttr("costfluent_provider.test", "description"),
					testAccKeepsID("costfluent_provider.test", &id),
				),
			},
			{Config: config(rName+" renamed", ""), PlanOnly: true},
		},
	})
}
