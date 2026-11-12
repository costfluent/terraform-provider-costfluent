package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var costReportExists = existsByGet((*costfluent.Client).GetCostReport)

// A report round-trips through the real API. It is created in the spellings an import reads back
// (the API's camel case enums, the grouping dimensions as the provider documents them, the filter as
// its JSON document), because an import cannot know another spelling the configuration might use.
// The update then writes PascalCase enums, camel case dimensions and CQL, which the provider keeps
// while they mean what the API stored. The update sends the ETag the import's read
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
					`[{ dimension = "ProviderName" }, { dimension = "Tag", key = "team" }]`, testAccCostReportJSONFilter),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_cost_report.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", rName),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "2"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "last30Days"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "ProviderName"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.1.key", "team"),
					// A report that names no measure charts cost.
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.measure", "cost"),
					testAccRemember("costfluent_cost_report.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: testAccCostReportConfig(rName, rName+" renamed", "Last30Days",
					`[{ dimension = "providerName" }, { dimension = "tag", key = "team" }]`, `"costs.provider = 'aws'"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "Last30Days"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "providerName"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.1.dimension", "tag"),
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

// A report saved over the three whole months before this one, then updated in place to the range
// that runs to the end of the sixth month ahead, keeps each window: the API stores and reads back
// both members in the spelling written, so an import after either plans nothing.
func TestAccCostReport_completeMonths(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccCostReportConfig(rName, rName, "last3CompleteMonths", `[{ dimension = "ServiceName" }]`, "null"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_cost_report.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "last3CompleteMonths"),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: testAccCostReportConfig(rName, rName, "throughNext6Months", `[{ dimension = "ServiceName" }]`, "null"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "date_interval", "throughNext6Months"),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
		},
	})
}

// A report grouped by commitment and filtered to one round-trips: the Public API takes the
// dimension and the CommitmentId field, and an import reads both back. Written in the spellings an
// import reads back, as the lifecycle test explains, so the import plans nothing.
func TestAccCostReport_groupedByCommitment(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccCostReportConfig(rName, rName, "last30Days", `[{ dimension = "CommitmentId" }]`,
					`jsonencode([{ field = "CommitmentId", operator = "In", values = ["arn:aws:savingsplans::111122223333:savingsplan/sp-1"] }])`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "1"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "CommitmentId"),
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

// A report grouped by invoice issuer and filtered to one billing period round-trips: the Public
// API takes the InvoiceIssuer dimension and the BillingPeriod field, and an import reads both back.
func TestAccCostReport_groupedByInvoiceIssuer(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccCostReportConfig(rName, rName, "last30Days", `[{ dimension = "InvoiceIssuer" }]`,
					`jsonencode([{ field = "BillingPeriod", operator = "In", values = ["2026-03"] }])`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "1"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.dimension", "InvoiceIssuer"),
					resource.TestCheckResourceAttrWith("costfluent_cost_report.test", "filter", func(value string) error {
						if !strings.Contains(value, "BillingPeriod") || !strings.Contains(value, "2026-03") {
							return fmt.Errorf("filter %q does not name the billing period", value)
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
  group_by     = [{ dimension = "ServiceName" }]
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

// The API keeps a date an update leaves out and refuses to clear one of a custom window, so removing
// the dates of a custom window keeps the stored ones and plans nothing. Moved to a relative window
// with no dates, the report clears both, and the next plan is empty.
func TestAccCostReport_removedDatesAreKept(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(interval, dates string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  title         = %q
  date_interval = %q
%s}
`, rName, interval, dates)
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
				Config: config("custom", "  start_date    = \"2026-01-01\"\n  end_date      = \"2026-03-31\"\n"),
				Check:  datesKept,
			},
			{
				Config: config("custom", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: datesKept,
			},
			{
				Config: config("last30Days", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.test", "start_date"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.test", "end_date"),
					func(s *terraform.State) error {
						rs := s.RootModule().Resources["costfluent_cost_report.test"]
						report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
						if err != nil {
							return err
						}
						for name, date := range map[string]*string{"start": report.StartDate, "end": report.EndDate} {
							if date != nil {
								return fmt.Errorf("stored %s date = %s, want it cleared", name, *date)
							}
						}
						return nil
					},
				),
			},
			{Config: config("last30Days", ""), PlanOnly: true},
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

// Settings flags the configuration leaves out take the workspace's report defaults (R29 FU3), and a
// chart it leaves out is the API's default, the stacked bar every surface creates.
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
				resource.TestCheckResourceAttr("costfluent_cost_report.test", "chart_type", "stackedBar"),
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

// A report compares with the same days a year earlier as a saved setting, which an import reads
// back; the grouping is in the API's spelling, which an import reads back too. An update that leaves
// the mode out keeps the report's own, as the API does.
func TestAccCostReport_comparisonMode(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(title, mode string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  group_by     = [{ dimension = "ServiceName" }]
  settings = {
    compare_to_last_period = true
%s  }
}
`, title, mode)
	}
	yearAgo := "    comparison_mode        = \"samePeriodLastYear\"\n"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config(rName, yearAgo),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.comparison_mode", "samePeriodLastYear"),
					testAccCostReportComparesWith("costfluent_cost_report.test", costfluent.ComparisonModeSamePeriodLastYear),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: config(rName+" renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.comparison_mode", "samePeriodLastYear"),
					testAccCostReportComparesWith("costfluent_cost_report.test", costfluent.ComparisonModeSamePeriodLastYear),
				),
			},
			{Config: config(rName+" renamed", ""), PlanOnly: true},
			{
				Config: config(rName+" renamed", "    comparison_mode        = \"previousPeriod\"\n"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.comparison_mode", "previousPeriod"),
					testAccCostReportComparesWith("costfluent_cost_report.test", costfluent.ComparisonModePreviousPeriod),
				),
			},
		},
	})
}

// testAccCostReportComparesWith checks the API stores mode on the report, not only the state.
func testAccCostReportComparesWith(name, mode string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources[name]
		report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		if report.Settings.ComparisonMode != mode {
			return fmt.Errorf("%s stored comparison mode %q, want %q", name, report.Settings.ComparisonMode, mode)
		}
		return nil
	}
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

// The API trims every label key and value of a unit cost filter and counts each value once, so a
// padded key, a padded value and a repeated one apply as configured, on create and on update, and
// the plan after each apply is empty, while the API stores the trimmed, distinct filter.
func TestAccCostReport_paddedUnitCostLabelFilterIsKept(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	config := func(filter string) string {
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_business_metric" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = "customer"
  label_columns = ["region"]
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.test.id
    unit_cost_label_filter = %s
  }
}
`, rName, rName, filter)
	}

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config(`[{ key = " region", values = ["eu", "eu "] }]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.0.key", " region"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.0.values.1", "eu "),
					testAccCheckReportChartsMetric("costfluent_cost_report.test", "costfluent_business_metric.test",
						[]costfluent.UnitCostLabelCondition{{Key: "region", Values: []string{"eu"}}}),
				),
			},
			{
				Config: config(`[{ key = "region ", values = [" us", "eu", "us"] }]`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.0.values.#", "3"),
					testAccCheckReportChartsMetric("costfluent_cost_report.test", "costfluent_business_metric.test",
						[]costfluent.UnitCostLabelCondition{{Key: "region", Values: []string{"us", "eu"}}}),
				),
			},
		},
	})
}

// A report at every grouping and label filter limit applies and reads back as configured, so a
// lower limit in the API fails the apply. The second step spells the same report as the API does not
// store it, an empty key and padded and repeated labels, so a provider that reads back the API's
// spelling fails it. Direct calls hold two of the API's refusals the provider copies, a Tag slot
// with no key and a VirtualTag slot naming a plain key, so an API that stops refusing either fails
// them, and a provider that stops refusing a keyless Tag slot fails the plan-only step.
func TestAccCostReport_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	tagKey := testAccDistinctValues("team", 1, 128)[0]
	columns := testAccDistinctValues("column", 5, 100)
	values := testAccDistinctValues("value", 100, 100)
	// spelled writes the second condition as the API does not store it, its key and value padded and
	// the value repeated, which the API stores as the unspelled condition.
	config := func(groupBy string, spelled bool) string {
		var conditions strings.Builder
		for i, column := range columns {
			key, labels := column, []string{values[0]}
			if i == 0 {
				labels = values
			}
			if i == 1 && spelled {
				key, labels = " "+column+" ", []string{" " + values[0], values[0] + " ", values[0]}
			}
			fmt.Fprintf(&conditions, "\n      { key = %q, values = %s },", key, testAccHCLList(labels))
		}
		return testAccWorkspaceFixture(rName) + fmt.Sprintf(`
resource "costfluent_virtual_tag" "test" {
  workspace_id = costfluent_workspace.fixture.id
  key          = "owner"

  rule {
    value  = "compute"
    filter = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EC2"] }])
  }
}

resource "costfluent_business_metric" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = "customer"
  label_columns = %s
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  group_by     = %s
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.test.id
    unit_cost_label_filter = [%s
    ]
  }
}
`, rName, testAccHCLList(columns), rName, groupBy, conditions.String())
	}
	atTheLimits := fmt.Sprintf(`[
    { dimension = "Tag", key = %q },
    { dimension = "VirtualTag", key = costfluent_virtual_tag.test.id },
    { dimension = "Segment" },
  ]`, tagKey)
	// The API reads an empty key as none.
	emptySegmentKey := strings.Replace(atTheLimits, `{ dimension = "Segment" }`, `{ dimension = "Segment", key = "" }`, 1)

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: config(atTheLimits, false),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.#", "3"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.0.key", tagKey),
					resource.TestCheckResourceAttrPair("costfluent_cost_report.test", "group_by.1.key", "costfluent_virtual_tag.test", "id"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.#", "5"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.0.values.#", "100"),
					testAccCheckCostReportAtTheLimitsSaved("costfluent_cost_report.test", "costfluent_virtual_tag.test", tagKey, columns, values),
					testAccCheckAPIRefusesGrouping("costfluent_cost_report.test", rName, costfluent.Grouping{Dimension: "Tag"}),
					testAccCheckAPIRefusesGrouping("costfluent_cost_report.test", rName, costfluent.Grouping{Dimension: "VirtualTag", Key: &tagKey}),
				),
			},
			// What the API stores the same as step 1 applies, reads back as configured and then
			// plans nothing, so the provider keeps a spelling the API normalizes.
			{
				Config: config(emptySegmentKey, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "group_by.2.key", ""),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.1.key", " "+columns[1]+" "),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_label_filter.1.values.#", "3"),
					testAccCheckCostReportAtTheLimitsSaved("costfluent_cost_report.test", "costfluent_virtual_tag.test", tagKey, columns, values),
				),
			},
			{
				Config:      config(`[{ dimension = "Tag" }]`, false),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`group_by\[0\]: grouping by Tag needs the key it groups on`),
			},
			// The post-test destroy plans the last step's configuration, which the provider refuses,
			// so the last step returns to what step 2 applied.
			{Config: config(emptySegmentKey, true), PlanOnly: true},
		},
	})
}

// testAccCheckCostReportAtTheLimitsSaved reads the report back through the API, so the slots and the
// label filter are what it stored, not what the provider kept in state.
func testAccCheckCostReportAtTheLimitsSaved(address, virtualTag, tagKey string, columns, values []string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs := s.RootModule().Resources[address]
		report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		tagID := s.RootModule().Resources[virtualTag].Primary.ID
		want := []struct{ dimension, key string }{{"Tag", tagKey}, {"VirtualTag", tagID}, {"Segment", ""}}
		if len(report.GroupBy) != len(want) {
			return fmt.Errorf("the API stored groupBy %v, want %v", report.GroupBy, want)
		}
		for i, slot := range report.GroupBy {
			key := ""
			if slot.Key != nil {
				key = *slot.Key
			}
			// The API answers the dimension in its own case.
			if !strings.EqualFold(slot.Dimension, want[i].dimension) || key != want[i].key {
				return fmt.Errorf("the API stored slot %d as %s, want %v", i, slot, want[i])
			}
		}
		filter := report.Settings.UnitCostLabelFilter
		if len(filter) != 5 || len(filter[0].Values) != len(values) || filter[0].Values[len(values)-1] != values[len(values)-1] {
			return fmt.Errorf("the API stored the label filter %v, want 5 conditions, the first with %d values", filter, len(values))
		}
		for i, condition := range filter {
			if condition.Key != columns[i] || (i > 0 && !slices.Equal(condition.Values, values[:1])) {
				return fmt.Errorf("the API stored label condition %d as %v, want key %s and, after the first, the one value %s", i, condition, columns[i], values[0])
			}
		}
		return nil
	}
}

// testAccCheckAPIRefusesGrouping creates a report grouped by slot directly, so the API's own rule is
// what is held: a 400 naming groupBy. A report the API takes instead is deleted before the check
// fails.
func testAccCheckAPIRefusesGrouping(address, name string, slot costfluent.Grouping) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		ctx := context.Background()
		workspaceID := s.RootModule().Resources[address].Primary.Attributes["workspace_id"]
		created, err := testAccClient().CreateCostReport(ctx, &costfluent.CreateCostReportInput{
			WorkspaceID: workspaceID,
			Title:       name + " refused",
			GroupBy:     []costfluent.Grouping{slot},
		})
		if err == nil {
			if deleteErr := testAccClient().DeleteCostReport(ctx, workspaceID, created.ID); deleteErr != nil {
				return fmt.Errorf("the API created a report grouped by %s; deleting %s: %w", slot, created.ID, deleteErr)
			}
			return fmt.Errorf("the API created a report grouped by %s, want a refusal on groupBy", slot)
		}
		var apiErr *costfluent.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("creating a report grouped by %s: %w, want a 400", slot, err)
		}
		for _, fe := range apiErr.Errors {
			if fe.Name == "groupBy" {
				return nil
			}
		}
		return fmt.Errorf("creating a report grouped by %s was refused without naming groupBy: %w", slot, err)
	}
}
