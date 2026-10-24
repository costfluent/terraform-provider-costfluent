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

// widgetExists reports whether the API still has a widget, which it reads through its dashboard.
// A dashboard that is gone holds no widgets.
func widgetExists(ctx context.Context, w trackedWidget) (bool, error) {
	_, err := testAccClient().GetDashboardWidget(ctx, w.workspaceID, w.dashboardID, w.id)
	if costfluent.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// trackedWidget is one widget's address in the API, remembered from an earlier step's state.
type trackedWidget struct {
	workspaceID, dashboardID, id string
}

func trackedWidgetOf(s *terraform.State, address string) (trackedWidget, error) {
	rs, ok := s.RootModule().Resources[address]
	if !ok {
		return trackedWidget{}, fmt.Errorf("%s not in state", address)
	}
	return trackedWidget{rs.Primary.Attributes["workspace_id"], rs.Primary.Attributes["dashboard_id"], rs.Primary.ID}, nil
}

// testAccCheckWidgetsGone is a CheckDestroy for costfluent_dashboard_widget.
func testAccCheckWidgetsGone(s *terraform.State) error {
	for address, rs := range s.RootModule().Resources {
		if rs.Type != "costfluent_dashboard_widget" {
			continue
		}
		w, err := trackedWidgetOf(s, address)
		if err != nil {
			return err
		}
		if err := testAccCheckWidgetGone(w); err != nil {
			return err
		}
	}
	return nil
}

func testAccCheckWidgetGone(w trackedWidget) error {
	found, err := widgetExists(context.Background(), w)
	if err != nil {
		return fmt.Errorf("looking up widget %s: %w", w.id, err)
	}
	if found {
		return fmt.Errorf("widget %s still exists", w.id)
	}
	return nil
}

// widgetImportID builds the "<workspace ID>:<dashboard ID>:<widget ID>" import identifier.
func widgetImportID(address string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		w, err := trackedWidgetOf(s, address)
		return w.workspaceID + ":" + w.dashboardID + ":" + w.id, err
	}
}

// A dashboard with a saved filter, a report widget and a text widget: created, imported, changed
// in place, the filter detached, and the widgets deleted while the dashboard stays.
func TestAccDashboardWidget_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var report, text trackedWidget
	remember := func(address string, into *trackedWidget) resource.TestCheckFunc {
		return func(s *terraform.State) (err error) {
			*into, err = trackedWidgetOf(s, address)
			return err
		}
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testAccCheckWidgetsGone,
			testAccCheckGone("costfluent_dashboard", dashboardExists),
		),
		Steps: []resource.TestStep{
			{
				Config: testAccDashboardWidgetConfig(rName, true, testAccWidgets("Spend", "", "Spend is *up* this month")),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"costfluent_dashboard.test", "saved_filter_id", "costfluent_saved_filter.eu", "id"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.report", "type", "costReport"),
					resource.TestCheckResourceAttrPair(
						"costfluent_dashboard_widget.report", "cost_report_id", "costfluent_cost_report.test", "id"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.report", "display", "chart"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.report", "position.w", "6"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.text", "type", "text"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.text", "content", "Spend is *up* this month"),
					resource.TestCheckNoResourceAttr("costfluent_dashboard_widget.text", "display"),
					remember("costfluent_dashboard_widget.report", &report),
					remember("costfluent_dashboard_widget.text", &text),
				),
			},
			{
				ResourceName:      "costfluent_dashboard_widget.report",
				ImportState:       true,
				ImportStateIdFunc: widgetImportID("costfluent_dashboard_widget.report"),
				ImportStateVerify: true,
			},
			{
				ResourceName:      "costfluent_dashboard_widget.text",
				ImportState:       true,
				ImportStateIdFunc: widgetImportID("costfluent_dashboard_widget.text"),
				ImportStateVerify: true,
			},
			testAccImportPlansNothing("costfluent_dashboard.test"),
			{
				Config: testAccDashboardWidgetConfig(rName, false, testAccWidgets("Spend by service", "kpi", "Spend is flat")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("costfluent_dashboard_widget.report", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("costfluent_dashboard_widget.text", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_dashboard.test", "saved_filter_id"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.report", "title", "Spend by service"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.report", "display", "kpi"),
					resource.TestCheckResourceAttr("costfluent_dashboard_widget.text", "content", "Spend is flat"),
				),
			},
			{
				Config: testAccDashboardWidgetConfig(rName, false, ""),
				Check: func(*terraform.State) error {
					for _, w := range []trackedWidget{report, text} {
						if err := testAccCheckWidgetGone(w); err != nil {
							return fmt.Errorf("after its resource was removed: %w", err)
						}
					}
					return nil
				},
			},
			testAccDefaultWorkspaceStep(testAccDashboardWidgetConfig(rName, false, ""), "costfluent_dashboard_widget", `  dashboard_id = costfluent_dashboard.test.id
  type         = "Text"
  title        = "Notes"
  content      = "Created in the provider's workspace"
  position     = { x = 0, y = 0, w = 6, h = 2 }
`),
		},
	})
}

func testAccWidgets(reportTitle, display, content string) string {
	displayAttr := ""
	if display != "" {
		displayAttr = fmt.Sprintf("  display        = %q\n", display)
	}
	return fmt.Sprintf(`
resource "costfluent_dashboard_widget" "report" {
  workspace_id   = costfluent_workspace.fixture.id
  dashboard_id   = costfluent_dashboard.test.id
  cost_report_id = costfluent_cost_report.test.id
  title          = %q
%s  position       = { x = 0, y = 0, w = 6, h = 4 }
}

resource "costfluent_dashboard_widget" "text" {
  workspace_id = costfluent_workspace.fixture.id
  dashboard_id = costfluent_dashboard.test.id
  type         = "text"
  title        = "Notes"
  content      = %q
  position     = { x = 6, y = 0, w = 6, h = 2 }
}
`, reportTitle, displayAttr, content)
}

func testAccDashboardWidgetConfig(name string, filtered bool, widgets string) string {
	filterAttr := ""
	if filtered {
		filterAttr = "  saved_filter_id = costfluent_saved_filter.eu.id\n"
	}
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_saved_filter" "eu" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  filter       = "costs.region LIKE 'eu-%%'"
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}

resource "costfluent_dashboard" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
%s}
`, name+" EU", name+" report", name, filterAttr) + widgets
}
