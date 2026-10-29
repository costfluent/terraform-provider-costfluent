package acceptance

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// A new workspace reads no connection, so it holds no commitment, and the read is checked for its
// contract: the API takes every filter and answers an empty list, and a status it does not know is
// refused rather than answered with an empty one.
func TestAccCommitments_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceFixture(rName) + `
data "costfluent_commitments" "all" {
  workspace_id = costfluent_workspace.fixture.id
}

data "costfluent_commitments" "filtered" {
  workspace_id   = costfluent_workspace.fixture.id
  provider_key   = "aws"
  status         = "expiring"
  type           = "SavingsPlan"
  service        = "Amazon EC2"
  account        = "123456789012"
  region         = "eu-central-1"
  term_years     = 3
  payment_option = "No Upfront"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.costfluent_commitments.all", "commitments.#", "0"),
					resource.TestCheckResourceAttr("data.costfluent_commitments.filtered", "commitments.#", "0"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName) + `
data "costfluent_commitments" "unknown" {
  workspace_id = costfluent_workspace.fixture.id
  status       = "retired"
}
`,
				ExpectError: regexp.MustCompile(`Failed to list commitments(.|\n)*\(400\)`),
			},
		},
	})
}

// The seeded sample organization holds two commitments, both bought All Upfront on its AWS
// connection. Their terms and statuses are drawn at random, so they are not checked.
func TestAccCommitments_readTheSeededCommitments(t *testing.T) {
	testAccUseSampleKey(t)
	every := map[string]func(string) error{
		"provider_key":   testAccEquals("aws"),
		"provider_name":  testAccEquals(testAccSampleConnection),
		"payment_option": testAccEquals("All Upfront"),
		"full_term_cost": testAccNotEmpty,
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccSampleWorkspace + `
data "costfluent_commitments" "all" {
  workspace_id = data.costfluent_workspace.sample.id
}

data "costfluent_commitments" "aws" {
  workspace_id = data.costfluent_workspace.sample.id
  provider_key = "aws"
}

data "costfluent_commitments" "no_upfront" {
  workspace_id   = data.costfluent_workspace.sample.id
  payment_option = "No Upfront"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_commitments.all", "commitments.#", "2"),
				testAccCheckEveryElem("data.costfluent_commitments.all", "commitments", 2, every),
				resource.TestCheckResourceAttr("data.costfluent_commitments.aws", "commitments.#", "2"),
				testAccCheckEveryElem("data.costfluent_commitments.aws", "commitments", 2, every),
				resource.TestCheckResourceAttr("data.costfluent_commitments.no_upfront", "commitments.#", "0"),
			),
		}},
	})
}
