package acceptance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var allocationRuleExists = existsInList((*costfluent.Client).ListAllocationRules, func(r costfluent.AllocationRule) string { return r.ID })

// A rule is created with its filter as the JSON document the API returns, because an import reads
// that back and cannot know a CQL spelling the configuration might use. The update then narrows the
// filter in CQL, which the provider keeps while it means what the API stored.
func TestAccAllocationRule_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
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

// Both rules sit at the API's limits: the longest name, one at the highest priority and one at the
// lowest. An API that narrows either limit refuses an apply the provider's plan let through. The
// second name is 200 UTF-16 code units, as the API counts them, in more than 200 bytes.
func TestAccAllocationRule_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	names := testAccDistinctValues("rule", 2, 200)
	names[1] = "rule-01-" + strings.Repeat("é", 190) + "😀"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_allocation_rule", allocationRuleExists),
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceFixture(rName) + testAccAllocationSegment + fmt.Sprintf(`
resource "costfluent_allocation_rule" "last" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  priority     = 1000000
  filter       = %s
  segment_id   = costfluent_segment.test.id
}

resource "costfluent_allocation_rule" "first" {
  workspace_id = costfluent_workspace.fixture.id
  name         = %q
  priority     = -1000000
  filter       = %s
  segment_id   = costfluent_segment.test.id
}
`, names[0], testAccAllocationRuleJSONFilter, names[1], testAccAllocationRuleJSONFilter),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_allocation_rule.last", "name", names[0]),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.last", "priority", "1000000"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.first", "name", names[1]),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.first", "priority", "-1000000"),
					testAccCheckAllocationRuleSaved("costfluent_allocation_rule.last", names[0], 1_000_000),
					testAccCheckAllocationRuleSaved("costfluent_allocation_rule.first", names[1], -1_000_000),
				),
			},
		},
	})
}

// testAccCheckAllocationRuleSaved reads the rule back through the API, so its name and priority are
// what it stored, not what the provider kept in state.
func testAccCheckAllocationRuleSaved(address, name string, priority int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources[address].Primary
		rules, err := testAccClient().ListAllocationRules(context.Background(), rs.Attributes["workspace_id"])
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if rule.ID != rs.ID {
				continue
			}
			if rule.Name != name || rule.Priority != priority {
				return fmt.Errorf("allocation rule %s holds name %q and priority %d, want %q and %d",
					rs.ID, rule.Name, rule.Priority, name, priority)
			}
			return nil
		}
		return fmt.Errorf("allocation rule %s is not in its workspace", rs.ID)
	}
}
