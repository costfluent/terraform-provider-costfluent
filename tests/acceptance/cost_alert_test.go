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
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccCostAlert_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccCostAlertConfig(rName, 20, false, "50"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_cost_alert.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_type", "percentageIncrease"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_value", "20"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "comparison_period", "previousWeek"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "minimum_amount", "50"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "evaluation_frequency_minutes", "360"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "is_paused", "false"),
				),
			},
			{
				ResourceName:      "costfluent_cost_alert.test",
				ImportState:       true,
				ImportStateIdFunc: workspaceScopedImportID("costfluent_cost_alert.test"),
				ImportStateVerify: true,
			},
			// Update the threshold, remove the floor and pause, all in place
			{
				Config: testAccCostAlertConfig(rName, 35, true, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "threshold_value", "35"),
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.test", "minimum_amount"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "is_paused", "true"),
				),
			},
			testAccDefaultWorkspaceStep(testAccCostAlertConfig(rName, 35, true, ""), "costfluent_cost_alert", fmt.Sprintf(`  name              = %q
  threshold_type    = "percentageIncrease"
  threshold_value   = 20
  comparison_period = "previousWeek"
`, rName+" default")),
		},
	})
}

// testAccCostAlertConfig is the alert with a minimum amount, or with none when floor is empty.
func testAccCostAlertConfig(name string, threshold int, paused bool, floor string) string {
	minimum := ""
	if floor != "" {
		minimum = fmt.Sprintf("  minimum_amount               = %q\n", floor)
	}
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_cost_alert" "test" {
  workspace_id                 = costfluent_workspace.fixture.id
  name                         = %q
  threshold_type               = "percentageIncrease"
  threshold_value              = %d
  comparison_period            = "previousWeek"
  evaluation_frequency_minutes = 360
  is_paused                    = %t
%s}
`, name, threshold, paused, minimum)
}

// Removing the comparison period and the filter clears them on the same alert.
func TestAccCostAlert_clearsInPlace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var id string
	config := func(extra string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_alert" "test" {
  workspace_id    = costfluent_workspace.fixture.id
  name            = %q
  threshold_type  = "percentageIncrease"
  threshold_value = 20
%s}
`, rName, extra)
	}
	withBoth := "  comparison_period = \"previousWeek\"\n  filter            = \"costs.provider = 'aws'\"\n"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(withBoth),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "comparison_period", "previousWeek"),
					testAccKeepsID("costfluent_cost_alert.test", &id),
				),
			},
			{
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_cost_alert.test", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.test", "comparison_period"),
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.test", "filter"),
					testAccKeepsID("costfluent_cost_alert.test", &id),
				),
			},
			{Config: config(""), PlanOnly: true},
		},
	})
}

// The API's command validator refuses a percentageIncrease alert with no comparison period under the
// detail "One or more validation errors occurred", so the diagnostic must carry the field it names.
func TestAccCostAlert_refusalNamesTheField(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_alert" "test" {
  workspace_id    = costfluent_workspace.fixture.id
  name            = %q
  threshold_type  = "percentageIncrease"
  threshold_value = 20
}
`, rName),
			ExpectError: regexp.MustCompile(`Failed to create cost alert(.|\n)*comparisonPeriod:`),
		}},
	})
}

// An alert's filter round-trips through the real API as a saved filter's does: created as the JSON
// document the API stores, so an import plans nothing, then written as the same filter in CQL, which
// the provider keeps while it means what the API stored. A filter changed outside Terraform reads
// back as the API's document and plans an update, which writes the configured filter back.
func TestAccCostAlert_filter(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var alert trackedObject
	var id string
	config := func(filter string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_alert" "test" {
  workspace_id      = costfluent_workspace.fixture.id
  name              = %q
  threshold_type    = "percentageIncrease"
  threshold_value   = 20
  comparison_period = "previousWeek"
  filter            = %s
}
`, rName, filter)
	}
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("costfluent_cost_alert.test", plancheck.ResourceActionUpdate),
	}}
	const cql = `"costs.provider = 'aws'"`

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(testAccCostReportJSONFilter),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "filter", testAccAWSFilterDocument),
					testAccRemember("costfluent_cost_alert.test", &alert),
					testAccKeepsID("costfluent_cost_alert.test", &id),
				),
			},
			testAccImportPlansNothing("costfluent_cost_alert.test"),
			{
				Config:           config(cql),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "filter", "costs.provider = 'aws'"),
					testAccKeepsID("costfluent_cost_alert.test", &id),
				),
			},
			{
				PreConfig: func() {
					gcp := "costs.provider = 'gcp'"
					if _, err := testAccClient().UpdateCostAlert(context.Background(), alert.workspaceID, alert.id,
						&costfluent.UpdateCostAlertInput{Filter: &gcp}); err != nil {
						t.Fatalf("changing the filter outside Terraform: %v", err)
					}
				},
				Config:           config(cql),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.test", "filter", "costs.provider = 'aws'"),
					func(*terraform.State) error {
						stored, err := testAccClient().GetCostAlert(context.Background(), alert.workspaceID, alert.id)
						if err != nil {
							return err
						}
						if stored.Filter == nil || *stored.Filter != testAccAWSFilterDocument {
							return fmt.Errorf("stored filter = %v, want the apply to have written %s back", stored.Filter,
								testAccAWSFilterDocument)
						}
						return nil
					},
				),
			},
		},
	})
}
