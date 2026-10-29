package acceptance

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// tagPolicyHasRules is an existsFunc for a workspace's tag policy, which the API never deletes, only
// empties: it exists while it has rules. A workspace that is gone holds none.
func tagPolicyHasRules(ctx context.Context, c *costfluent.Client, workspaceID, _ string) (bool, error) {
	policy, err := c.GetTagPolicy(ctx, workspaceID)
	if costfluent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(policy.Rules) > 0, nil
}

// The rule limits in tag_policy_resource.go are a hand copy of the API's, and the null account list
// a rule leaves out is a reading of the API's [], so a real save and read back is what would catch
// either drifting.
func TestAccTagPolicy_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_tag_policy", tagPolicyHasRules),
		Steps: []resource.TestStep{
			{
				Config: testAccTagPolicyConfig(rName, testAccTagPolicyTwoRules),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("costfluent_tag_policy.test", "id", "costfluent_workspace.fixture", "id"),
					resource.TestCheckResourceAttrPair("costfluent_tag_policy.test", "workspace_id", "costfluent_workspace.fixture", "id"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.#", "2"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.provider", "aws"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.account_ids.0", "123456789012"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.required_keys.1", "env"),
					resource.TestCheckNoResourceAttr("costfluent_tag_policy.test", "rule.1.provider"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.1.required_keys.0", "cost-center"),
					resource.TestMatchResourceAttr("costfluent_tag_policy.test", "report_id", regexp.MustCompile(`^crp_`)),
					testAccRemember("costfluent_tag_policy.test", &created),
				),
			},
			{
				// The import ID is the workspace ID, which is the policy's ID.
				ResourceName:    "costfluent_tag_policy.test",
				ImportState:     true,
				ImportStateKind: resource.ImportBlockWithID,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				Config: testAccTagPolicyConfig(rName, `
  rule {
    provider      = "gcp"
    required_keys = ["owner", "team"]
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_tag_policy.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.#", "1"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.provider", "gcp"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.required_keys.#", "2"),
					resource.TestMatchResourceAttr("costfluent_tag_policy.test", "report_id", regexp.MustCompile(`^crp_`)),
				),
			},
			{
				// Removing the resource clears the rules while the workspace still exists.
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, tagPolicyHasRules),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_tag_policy", `
  rule {
    required_keys = ["team"]
  }
`),
		},
	})
}

const testAccTagPolicyTwoRules = `
  rule {
    provider      = "aws"
    account_ids   = ["123456789012"]
    required_keys = ["team", "env"]
  }

  rule {
    required_keys = ["cost-center"]
  }
`

func testAccTagPolicyConfig(name, rules string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_tag_policy" "test" {
  workspace_id = costfluent_workspace.fixture.id
%s}
`, rules)
}
