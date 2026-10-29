package acceptance

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// A new workspace has no connection to generate findings and nobody has written one, so the read is
// checked for its contract: the API takes every filter and answers an empty queue, and a tab it does
// not know is refused rather than answered with an empty one.
func TestAccSavingsItems_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceFixture(rName) + `
data "costfluent_savings_items" "all" {
  workspace_id = costfluent_workspace.fixture.id
}

data "costfluent_savings_items" "filtered" {
  workspace_id           = costfluent_workspace.fixture.id
  tab                    = "toReview"
  source                 = "awsCostOptimizationHub"
  kind                   = "rightsizing"
  minimum_monthly_saving = "100"
  sort                   = "largestSaving"
}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.costfluent_savings_items.all", "savings_items.#", "0"),
					resource.TestCheckResourceAttr("data.costfluent_savings_items.filtered", "savings_items.#", "0"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName) + `
data "costfluent_savings_items" "unknown" {
  workspace_id = costfluent_workspace.fixture.id
  tab          = "pending"
}
`,
				ExpectError: regexp.MustCompile(`Failed to list savings items(.|\n)*\(400\)`),
			},
		},
	})
}

// The seeded sample organization holds two Cost Optimization Hub findings on its AWS connection, an
// EC2 rightsize and a Compute Savings Plan, and no Recommender finding, since it has no GCP
// connection. Their titles and amounts are fixed, so the filters and the sort are checked by them.
func TestAccSavingsItems_readTheSeededFindings(t *testing.T) {
	testAccUseSampleKey(t)
	rightsize := map[string]string{
		"source":                   "awsCostOptimizationHub",
		"kind":                     "rightsizing",
		"title":                    "Rightsize Ec2Instance i-0a7c3e91f42b6d805 (m5.large to t3.medium)",
		"estimated_monthly_saving": "48.94000000",
		"currency":                 "EUR",
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccSampleWorkspace + `
data "costfluent_savings_items" "all" {
  workspace_id = data.costfluent_workspace.sample.id
}

data "costfluent_savings_items" "rightsizing" {
  workspace_id = data.costfluent_workspace.sample.id
  kind         = "rightsizing"
}

data "costfluent_savings_items" "large" {
  workspace_id           = data.costfluent_workspace.sample.id
  minimum_monthly_saving = "100"
}

data "costfluent_savings_items" "largest_first" {
  workspace_id = data.costfluent_workspace.sample.id
  sort         = "largestSaving"
}

data "costfluent_savings_items" "gcp" {
  workspace_id = data.costfluent_workspace.sample.id
  source       = "gcpRecommender"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_savings_items.all", "savings_items.#", "2"),
				resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_savings_items.all", "savings_items.*", rightsize),

				resource.TestCheckResourceAttr("data.costfluent_savings_items.rightsizing", "savings_items.#", "1"),
				resource.TestCheckTypeSetElemNestedAttrs("data.costfluent_savings_items.rightsizing", "savings_items.*", rightsize),

				resource.TestCheckResourceAttr("data.costfluent_savings_items.large", "savings_items.#", "1"),
				resource.TestCheckResourceAttr("data.costfluent_savings_items.large", "savings_items.0.kind", "commitmentPurchase"),
				resource.TestCheckResourceAttr("data.costfluent_savings_items.large", "savings_items.0.estimated_monthly_saving", "339.90000000"),

				resource.TestCheckResourceAttr("data.costfluent_savings_items.largest_first", "savings_items.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_savings_items.largest_first", "savings_items.0.kind", "commitmentPurchase"),
				resource.TestCheckResourceAttr("data.costfluent_savings_items.largest_first", "savings_items.1.kind", "rightsizing"),

				resource.TestCheckResourceAttr("data.costfluent_savings_items.gcp", "savings_items.#", "0"),
			),
		}},
	})
}
