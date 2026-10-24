package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The organization lists every workspace the token reaches, the one this test made among them.
func TestAccWorkspacesDataSource_listsTheFixture(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + `
data "costfluent_workspaces" "all" {
  depends_on = [costfluent_workspace.fixture]
}
`,
			Check: resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_workspaces.all", "workspaces.*", map[string]string{
				"name":      rName + "-ws",
				"currency":  "EUR",
				"time_zone": "UTC",
			}),
		}},
	})
}

// The acceptance organization holds only the seeded AWS connection, which never collects, so the
// read is checked for its contract: it answers with a list.
func TestAccProvidersDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: `data "costfluent_providers" "all" {}`,
			Check:  resource.TestCheckResourceAttrSet("data.costfluent_providers.all", "providers.#"),
		}},
	})
}

// A new workspace holds no cost, so the queries are checked for their contract: the API takes the
// window, the groupings and the filter, and answers with totals and rows.
func TestAccCostDataSources_queryAWorkspace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + fmt.Sprintf(`
data "costfluent_cost_data" "by_provider" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
  granularity  = "month"
  group_by     = ["ProviderName", "ServiceName", "Tag:team"]
  filter       = "costs.provider = 'aws'"
  limit        = 100
}

data "costfluent_cost_data" "usage" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
  group_by     = ["ServiceName"]
  measure      = "usage"
}

data "costfluent_cost_summary" "window" {
  workspace_id = costfluent_workspace.fixture.id
  start_date   = %[1]q
  end_date     = %[2]q
}
`, "2026-09-01", "2026-09-30"),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_cost_data.by_provider", "total_records", "0"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.by_provider", "data.#", "0"),
				resource.TestCheckResourceAttrSet("data.costfluent_cost_data.by_provider", "total_cost"),
				resource.TestCheckResourceAttrSet("data.costfluent_cost_summary.window", "total_cost"),
				resource.TestCheckResourceAttr("data.costfluent_cost_data.usage", "total_records", "0"),
			),
		}},
	})
}
