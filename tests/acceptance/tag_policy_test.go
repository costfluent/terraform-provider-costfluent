package acceptance

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
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

// Every count and length here sits at the limit tag_policy_resource.go holds, so an API that lowers
// one refuses an apply the provider's plan let through.
func TestAccTagPolicy_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	keys := testAccDistinctValues("key", 20, 128)
	accounts := testAccDistinctValues("acct", 50, 256)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_tag_policy", tagPolicyHasRules),
		Steps: []resource.TestStep{
			{
				Config: testAccTagPolicyConfig(rName, testAccTagPolicyAtTheLimits(keys, accounts)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.#", "20"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.required_keys.#", "20"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.required_keys.19", keys[19]),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.account_ids.#", "50"),
					resource.TestCheckResourceAttr("costfluent_tag_policy.test", "rule.0.account_ids.49", accounts[49]),
					testAccCheckTagPolicySaved("costfluent_tag_policy.test", 20, 20, 50),
				),
			},
		},
	})
}

// testAccDistinctValues returns count distinct ASCII values of exactly length characters each.
func testAccDistinctValues(prefix string, count, length int) []string {
	values := make([]string, count)
	for i := range values {
		head := fmt.Sprintf("%s-%02d-", prefix, i)
		values[i] = head + strings.Repeat("x", length-len(head))
	}
	return values
}

// testAccTagPolicyAtTheLimits is one rule with the most keys and accounts at their longest, then
// short rules up to the most a policy holds.
func testAccTagPolicyAtTheLimits(keys, accounts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  rule {\n    account_ids   = %s\n    required_keys = %s\n  }\n", testAccHCLList(accounts), testAccHCLList(keys))
	for i := 1; i < 20; i++ {
		fmt.Fprintf(&b, "\n  rule {\n    required_keys = [\"team-%02d\"]\n  }\n", i)
	}
	return b.String()
}

func testAccHCLList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// testAccCheckTagPolicySaved reads the policy back through the API, so the counts are what it
// stored, not what the provider kept in state.
func testAccCheckTagPolicySaved(address string, rules, firstRuleKeys, firstRuleAccounts int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		workspaceID := s.RootModule().Resources[address].Primary.Attributes["workspace_id"]
		policy, err := testAccClient().GetTagPolicy(context.Background(), workspaceID)
		if err != nil {
			return err
		}
		if len(policy.Rules) != rules {
			return fmt.Errorf("tag policy of %s holds %d rules, want %d", workspaceID, len(policy.Rules), rules)
		}
		first := policy.Rules[0]
		if len(first.RequiredKeys) != firstRuleKeys || len(first.AccountIDs) != firstRuleAccounts {
			return fmt.Errorf("tag policy of %s: first rule holds %d keys and %d accounts, want %d and %d",
				workspaceID, len(first.RequiredKeys), len(first.AccountIDs), firstRuleKeys, firstRuleAccounts)
		}
		return nil
	}
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
