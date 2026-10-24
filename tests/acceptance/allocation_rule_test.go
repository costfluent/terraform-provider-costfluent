package acceptance

import (
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var allocationRuleExists = existsInList((*costfluent.Client).ListAllocationRules, func(r costfluent.AllocationRule) string { return r.ID })

// A rule is created with its filter as the JSON document the API returns, because an import reads
// that back and cannot know a CQL spelling the configuration might use. The update then narrows the
// filter in CQL, which the provider keeps while it means what the API stored.
func TestAccAllocationRule_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_allocation_rule", allocationRuleExists),
		Steps: []resource.TestStep{
			{
				Config: testAccAllocationRuleConfig(rName, rName, testAccAllocationRuleJSONFilter, 10),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_allocation_rule.test", "id"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "priority", "10"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "cost_kind", "Direct"),
					resource.TestCheckResourceAttrPair("costfluent_allocation_rule.test", "segment_id", "costfluent_segment.test", "id"),
					testAccRemember("costfluent_allocation_rule.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_allocation_rule.test"),
			{
				Config: testAccAllocationRuleConfig(rName, rName+" renamed", fmt.Sprintf("%q", testAccAllocationRuleCQLFilter), 20),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_allocation_rule.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "name", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "filter", testAccAllocationRuleCQLFilter),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.test", "priority", "20"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName) + testAccAllocationSegment,
				Check:  testAccCheckRemoved(&created, allocationRuleExists),
			},
		},
	})
}

// testAccAllocationSegment is the segment the rule assigns cost to, kept when the rule is removed.
const testAccAllocationSegment = `
resource "costfluent_segment" "test" {
  workspace_id = costfluent_workspace.fixture.id
  team_label   = "Checkout team"
  product      = "Checkout"
  cost_centre  = "CC-1001"
}
`

// testAccAllocationRuleJSONFilter is the CQL filter costs.service = 'AmazonEC2' as the JSON document
// the API stores it as.
const testAccAllocationRuleJSONFilter = `jsonencode([{ field = "ServiceName", operator = "In", values = ["AmazonEC2"] }])`

const testAccAllocationRuleCQLFilter = "costs.service = 'AmazonEC2' AND costs.region = 'eu-central-1'"

// testAccAllocationRuleConfig takes the filter as an HCL expression, so a test can pass jsonencode.
func testAccAllocationRuleConfig(name, ruleName, filter string, priority int) string {
	return testAccWorkspaceFixture(name) + testAccAllocationSegment + fmt.Sprintf(`
resource "costfluent_allocation_rule" "test" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  priority     = %d
  filter       = %s
  segment_id   = costfluent_segment.test.id
}
`, ruleName, priority, filter)
}
