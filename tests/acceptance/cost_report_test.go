package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var costReportExists = existsByGet((*costfluent.Client).GetCostReport)

// A report round-trips through the real API. It is created in the spellings the API returns (camel
// case enums, the filter as its JSON document), because an import reads those back and cannot know
// another spelling the configuration might use. The update then writes PascalCase and CQL, which the
// provider keeps while they mean what the API stored. The update sends the ETag the import's read
// left as If-Match, so a version the API answered that its next read would not match fails it.
func TestAccCostReport_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccCostReportConfig(rName, rName, "last30Days",
					`[{ dimension = "providerName" }, { dimension = "tag", key = "team" }]`, testAccCostReportJSONFilter),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_cost_report.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", rName),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "2"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "last30Days"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "providerName"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.1.key", "team"),
					// A report that names no measure charts cost.
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.measure", "cost"),
					testAccRemember("costfluent_cost_report.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: testAccCostReportConfig(rName, rName+" renamed", "Last30Days",
					`[{ dimension = "ProviderName" }, { dimension = "Tag", key = "team" }]`, `"costs.provider = 'aws'"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "Last30Days"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "ProviderName"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.1.dimension", "Tag"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "filter", "costs.provider = 'aws'"),
					testAccCostReportVersioned("costfluent_cost_report.test"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, costReportExists),
			},
		},
	})
}

// A report grouped by commitment and filtered to one round-trips: the Public API takes the
// dimension and the CommitmentId field, and an import reads both back. Written in the spellings
// the API returns, as the lifecycle test explains, so the import plans nothing.
func TestAccCostReport_groupedByCommitment(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccCostReportConfig(rName, rName, "last30Days", `[{ dimension = "commitmentId" }]`,
					`jsonencode([{ field = "CommitmentId", operator = "In", values = ["arn:aws:savingsplans::111122223333:savingsplan/sp-1"] }])`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "1"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "commitmentId"),
					resource.TestCheckResourceAttrWith("costfluent_cost_report.test", "filter", func(value string) error {
						if !strings.Contains(value, "arn:aws:savingsplans::111122223333:savingsplan/sp-1") {
							return fmt.Errorf("filter %q does not name the Savings Plan", value)
						}
						return nil
					}),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
		},
	})
}

// A bar chart of a grouped report keeps the group axis it was given; changed to a line chart that
// leaves the axis out, it is drawn by date, which the next plan reads as no change.
func TestAccCostReport_chartAxis(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(chart string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  group_by     = [{ dimension = "serviceName" }]
%s}
`, rName, chart)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config("  chart_type = \"bar\"\n  chart_axis = \"group\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "chart_type", "bar"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "chart_axis", "group"),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: config("  chart_type = \"line\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "chart_type", "line"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "chart_axis", "date"),
				),
			},
			{Config: config("  chart_type = \"line\"\n"), PlanOnly: true},
		},
	})
}

// testAccCostReportVersioned checks the API answers the report's read with an ETag, the version an
// update and a deletion send back as If-Match.
func testAccCostReportVersioned(name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources[name]
		report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		if report.ETag == "" {
			return fmt.Errorf("%s read without an ETag, want the stored version", name)
		}
		return nil
	}
}

// testAccCostReportJSONFilter is the CQL filter costs.provider = 'aws' as the JSON document the API
// stores it as.
const testAccCostReportJSONFilter = `jsonencode([{ field = "Provider", operator = "In", values = ["aws"] }])`

func testAccCostReportConfig(name, title, dateInterval, groupBy, filter string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  title         = %q
  date_interval = %q
  group_by      = %s
  filter        = %s
}
`, title, dateInterval, groupBy, filter)
}

// The API keeps a date an update leaves out and cannot clear one, so removing the dates of a custom
// window keeps the stored ones and plans nothing.
func TestAccCostReport_removedDatesAreKept(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(dates string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  title         = %q
  date_interval = "custom"
%s}
`, rName, dates)
	}
	datesKept := resource.ComposeTestCheckFunc(
		resource.TestCheckResourceAttr("costfluent_cost_report.test", "start_date", "2026-01-01"),
		resource.TestCheckResourceAttr("costfluent_cost_report.test", "end_date", "2026-03-31"),
	)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config("  start_date    = \"2026-01-01\"\n  end_date      = \"2026-03-31\"\n"),
				Check:  datesKept,
			},
			{
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: datesKept,
			},
		},
	})
}

// The API takes a report out of its folder only on clearFolder and otherwise answers the folder it
// keeps, so removing folder_id unfiles the report and then plans nothing.
func TestAccCostReport_removingTheFolderTakesItOut(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(folder string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_folder" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
%s}
`, rName, rName, folder)
	}
	unfiled := resource.ComposeTestCheckFunc(
		resource.TestCheckNoResourceAttr("costfluent_cost_report.test", "folder_id"),
		func(s *terraform.State) error {
			rs := s.RootModule().Resources["costfluent_cost_report.test"]
			report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
			if err != nil {
				return err
			}
			if report.FolderID != nil {
				return fmt.Errorf("the API still files the report under %s", *report.FolderID)
			}
			return nil
		},
	)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config("  folder_id    = costfluent_folder.test.id\n"),
				Check: resource.TestCheckResourceAttrPair(
					"costfluent_cost_report.test", "folder_id", "costfluent_folder.test", "id"),
			},
			{
				Config: config(""),
				Check:  unfiled,
			},
			{
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: unfiled,
			},
		},
	})
}

// Settings flags the configuration leaves out take the workspace's report defaults (R29 FU3).
func TestAccCostReport_unsetSettingsTakeTheWorkspaceDefaults(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
resource "costfluent_workspace" "defaults" {
  name = %q

  report_defaults = {
    include_tax            = true
    compare_to_last_period = true
  }
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.defaults.id
  title        = %q
  settings     = { show_forecast = true }
}
`, rName+"-ws", rName),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.show_forecast", "true"),
				resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.include_tax", "true"),
				resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.compare_to_last_period", "true"),
			),
		}},
	})
}

// A comparison window set in the app survives a Terraform update that carries settings: the API
// takes the window as sent with settings, so the provider sends back the stored one.
func TestAccCostReport_comparisonWindowIsKept(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var report trackedObject
	config := func(title string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  settings     = { include_credits = true }
}
`, title)
	}
	start, end := "2026-09-01", "2026-09-30"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config(rName),
				Check:  testAccRemember("costfluent_cost_report.test", &report),
			},
			{
				PreConfig: func() {
					credits := true
					if _, err := testAccClient().UpdateCostReport(context.Background(), report.workspaceID, report.id,
						&costfluent.UpdateCostReportInput{Settings: &costfluent.CostReportSettingsInput{
							IncludeCredits: &credits, ComparisonStartDate: &start, ComparisonEndDate: &end,
						}}); err != nil {
						t.Fatalf("setting the comparison window as the app would: %v", err)
					}
				},
				Config: config(rName + "-renamed"),
				Check: func(*terraform.State) error {
					stored, err := testAccClient().GetCostReport(context.Background(), report.workspaceID, report.id)
					if err != nil {
						return err
					}
					got, want := stored.Settings, start+" "+end
					if got.ComparisonStartDate == nil || got.ComparisonEndDate == nil ||
						*got.ComparisonStartDate+" "+*got.ComparisonEndDate != want {
						return fmt.Errorf("comparison window after the update = %v %v, want %s",
							got.ComparisonStartDate, got.ComparisonEndDate, want)
					}
					return nil
				},
			},
		},
	})
}

// A report filtered to a segment names the segment by the id Terraform knows only after the
// segment is created. The API stores the filter as its JSON document with a Segment clause, and
// the provider keeps the CQL as written while it means what is stored, so a second apply plans
// nothing.
func TestAccCostReport_filteredToASegment(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := testAccSegmentConfig(rName, "Checkout", "CC-1001") + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  group_by     = [{ dimension = "ServiceName" }]
  filter       = "costs.segment = '${costfluent_segment.test.id}'"
}
`, rName)
	filteredToTheSegment := func(s *terraform.State) error {
		segment := s.RootModule().Resources["costfluent_segment.test"].Primary.ID
		rs := s.RootModule().Resources["costfluent_cost_report.test"]
		if got, want := rs.Primary.Attributes["filter"], "costs.segment = '"+segment+"'"; got != want {
			return fmt.Errorf("filter in state = %q, want %q", got, want)
		}
		report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		if report.Filter == nil {
			return fmt.Errorf("the API stored no filter, want a Segment clause naming %s", segment)
		}
		var clauses []struct {
			Field    string   `json:"field"`
			Operator string   `json:"operator"`
			Values   []string `json:"values"`
		}
		if err := json.Unmarshal([]byte(*report.Filter), &clauses); err != nil {
			return fmt.Errorf("reading the stored filter %s: %w", *report.Filter, err)
		}
		if len(clauses) != 1 || clauses[0].Field != "Segment" || clauses[0].Operator != "In" ||
			len(clauses[0].Values) != 1 || clauses[0].Values[0] != segment {
			return fmt.Errorf("stored filter = %s, want one Segment In clause naming %s", *report.Filter, segment)
		}
		return nil
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  filteredToTheSegment,
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: filteredToTheSegment,
			},
		},
	})
}
