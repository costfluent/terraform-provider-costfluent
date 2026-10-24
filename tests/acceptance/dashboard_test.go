package acceptance

import (
	"context"
	"fmt"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var dashboardExists = existsByGet((*costfluent.Client).GetDashboard)

func TestAccDashboard_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_dashboard", dashboardExists),
		Steps: []resource.TestStep{
			{
				Config: testAccDashboardConfig(rName, rName, "day"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_dashboard.test", "id"),
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "title", rName),
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_interval", "thisMonth"),
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_bin", "day"),
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "is_default", "false"),
					testAccRemember("costfluent_dashboard.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_dashboard.test"),
			{
				Config: testAccDashboardConfig(rName, rName+" renamed", "week"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "title", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_bin", "week"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, dashboardExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_dashboard",
				fmt.Sprintf("  title = %q\n", rName+" default")),
		},
	})
}

// The API drops the dates sent beside a preset and keeps a custom range's; the configured dates
// stay in state either way, so each apply is consistent and plans nothing after it.
func TestAccDashboard_presetKeepsTheConfiguredDatesTheAPIIgnores(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var dashboard trackedObject
	var id string

	// storedRange checks the range the API holds; "" is no date.
	storedRange := func(wantStart, wantEnd string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			got, err := testAccClient().GetDashboard(context.Background(), dashboard.workspaceID, dashboard.id)
			if err != nil {
				return err
			}
			start, end := "", ""
			if got.StartDate != nil {
				start = *got.StartDate
			}
			if got.EndDate != nil {
				end = *got.EndDate
			}
			if start != wantStart || end != wantEnd {
				return fmt.Errorf("API range %q to %q, want %q to %q", start, end, wantStart, wantEnd)
			}
			return nil
		}
	}
	configuredDates := resource.ComposeTestCheckFunc(
		resource.TestCheckResourceAttr("costfluent_dashboard.test", "start_date", "2026-08-01"),
		resource.TestCheckResourceAttr("costfluent_dashboard.test", "end_date", "2026-08-31"),
	)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_dashboard", dashboardExists),
		Steps: []resource.TestStep{
			{
				Config: testAccDashboardRangeConfig(rName, "lastMonth"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_interval", "lastMonth"),
					configuredDates,
					testAccRemember("costfluent_dashboard.test", &dashboard),
					testAccKeepsID("costfluent_dashboard.test", &id),
					storedRange("", ""),
				),
			},
			{
				Config: testAccDashboardRangeConfig(rName, "custom"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_interval", "custom"),
					configuredDates,
					testAccKeepsID("costfluent_dashboard.test", &id),
					storedRange("2026-08-01", "2026-08-31"),
				),
			},
			{
				Config: testAccDashboardRangeConfig(rName, "lastMonth"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "date_interval", "lastMonth"),
					configuredDates,
					testAccKeepsID("costfluent_dashboard.test", &id),
					storedRange("", ""),
				),
			},
		},
	})
}

func testAccDashboardRangeConfig(name, interval string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_dashboard" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  title         = %q
  date_interval = %q
  start_date    = "2026-08-01"
  end_date      = "2026-08-31"
}
`, name, interval)
}

// Someone makes another dashboard the default in the app: the next apply takes the default back
// in place, so the dashboard keeps its ID and its widgets.
func TestAccDashboard_defaultTakenOutsideTerraformIsRestoredInPlace(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var dashboard, widget trackedObject
	var otherID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_dashboard", dashboardExists),
		Steps: []resource.TestStep{
			{
				Config: testAccDefaultDashboardConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "is_default", "true"),
					testAccRemember("costfluent_dashboard.test", &dashboard),
					testAccRemember("costfluent_dashboard_widget.text", &widget),
				),
			},
			{
				PreConfig: func() {
					c := testAccClient()
					other, err := c.CreateDashboard(context.Background(), &costfluent.CreateDashboardInput{
						WorkspaceID: dashboard.workspaceID, Title: rName + " made default in the app", IsDefault: true,
					})
					if err != nil {
						t.Fatalf("making another dashboard the default outside Terraform: %v", err)
					}
					otherID = other.ID
					t.Cleanup(func() {
						if err := c.DeleteDashboard(context.Background(), dashboard.workspaceID, otherID); err != nil && !costfluent.IsNotFound(err) {
							t.Errorf("deleting the dashboard made outside Terraform: %v", err)
						}
					})
				},
				Config: testAccDefaultDashboardConfig(rName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("costfluent_dashboard_widget.text", plancheck.ResourceActionNoop),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.test", "is_default", "true"),
					resource.TestCheckResourceAttrPtr("costfluent_dashboard.test", "id", &dashboard.id),
					resource.TestCheckResourceAttrPtr("costfluent_dashboard_widget.text", "id", &widget.id),
					func(*terraform.State) error {
						other, err := testAccClient().GetDashboard(context.Background(), dashboard.workspaceID, otherID)
						if err != nil {
							return err
						}
						if other.IsDefault {
							return fmt.Errorf("dashboard %s is still the default, want the apply to have taken it back", otherID)
						}
						return nil
					},
				),
			},
		},
	})
}

func testAccDefaultDashboardConfig(name string) string {
	return testAccWorkspaceFixture(name) + `
resource "costfluent_dashboard" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "Default"
  is_default   = true
}

resource "costfluent_dashboard_widget" "text" {
  workspace_id = costfluent_workspace.fixture.id
  dashboard_id = costfluent_dashboard.test.id
  type         = "text"
  title        = "Notes"
  content      = "Kept when the default moves"
  position     = { x = 0, y = 0, w = 6, h = 2 }
}
`
}

func testAccDashboardConfig(name, title, dateBin string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_dashboard" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  title         = %q
  date_interval = "thisMonth"
  date_bin      = %q
}
`, title, dateBin)
}
