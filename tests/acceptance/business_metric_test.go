package acceptance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var businessMetricExists = existsByGet((*costfluent.Client).GetBusinessMetric)

func TestAccBusinessMetric_lifecycle(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var created trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_business_metric", businessMetricExists),
		Steps: []resource.TestStep{
			{
				Config: testAccBusinessMetricConfig(rName, rName, "customer"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_business_metric.test", "id"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "unit", "customer"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "label_columns.0", "region"),
					resource.TestCheckResourceAttrPair(
						"costfluent_cost_report.test", "settings.unit_cost_metric_id", "costfluent_business_metric.test", "id"),
					resource.TestCheckResourceAttr(
						"costfluent_cost_report.test", "settings.unit_cost_label_filter.0.values.0", "eu"),
					testAccRemember("costfluent_business_metric.test", &created),
				),
			},
			testAccImportPlansNothing("costfluent_business_metric.test"),
			{
				Config: testAccBusinessMetricConfig(rName, rName+" renamed", "account"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_business_metric.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "name", rName+" renamed"),
					resource.TestCheckResourceAttr("costfluent_business_metric.test", "unit", "account"),
				),
			},
			{
				Config: testAccWorkspaceFixture(rName),
				Check:  testAccCheckRemoved(&created, businessMetricExists),
			},
			testAccDefaultWorkspaceStep(testAccWorkspaceFixture(rName), "costfluent_business_metric",
				fmt.Sprintf("  name = %q\n  unit = \"customer\"\n", rName+" default")),
		},
	})
}

// testAccBusinessMetricConfig also saves a report charting cost per unit of the metric's European
// values, so the report's reference to it and its label filter are applied against the API.
func testAccBusinessMetricConfig(name, metricName, unit string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "test" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = %q
  label_columns = ["region"]
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%s per unit"
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.test.id
    unit_cost_label_filter = [{ key = "region", values = ["eu"] }]
  }
}
`, metricName, unit, name)
}

// Changing a metric's label columns replaces it. Deleting the old one resets the report charting it
// to cost alone, per unit and per 1, a change to the report the customer's own apply made, so the
// report's update that follows in the same apply charts the new metric with its label filter and
// scale rather than being refused as a change made outside Terraform.
func TestAccBusinessMetric_replacedUnderAReport(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	var replaced trackedObject

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_business_metric", businessMetricExists),
		Steps: []resource.TestStep{
			{
				Config: testAccMetricUnderAReportConfig(rName, `["region"]`),
				Check:  testAccRemember("costfluent_business_metric.charted", &replaced),
			},
			{
				Config: testAccMetricUnderAReportConfig(rName, `["region", "customer"]`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_business_metric.charted", plancheck.ResourceActionReplace),
						plancheck.ExpectResourceAction("costfluent_cost_report.charting", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.charted", "label_columns.1", "customer"),
					testAccCheckRemoved(&replaced, businessMetricExists),
					testAccCheckReportChartsMetric("costfluent_cost_report.charting", "costfluent_business_metric.charted",
						[]costfluent.UnitCostLabelCondition{{Key: "region", Values: []string{"eu"}}}),
				),
			},
		},
	})
}

func testAccMetricUnderAReportConfig(name, columns string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "charted" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = "customer"
  label_columns = %s
}

resource "costfluent_cost_report" "charting" {
  workspace_id = costfluent_workspace.fixture.id
  title        = "%s per unit"
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.charted.id
    unit_cost_label_filter = [{ key = "region", values = ["eu"] }]
    unit_cost_scale        = 1000
  }
}
`, name, columns, name)
}

// testAccCheckReportChartsMetric reads the report reportAddress holds back from the API and requires
// it to chart the metric metricAddress holds, counting the values filter chooses.
func testAccCheckReportChartsMetric(reportAddress, metricAddress string, filter []costfluent.UnitCostLabelCondition) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[reportAddress]
		if !ok {
			return fmt.Errorf("%s not in state", reportAddress)
		}
		metric, ok := s.RootModule().Resources[metricAddress]
		if !ok {
			return fmt.Errorf("%s not in state", metricAddress)
		}
		report, err := testAccClient().GetCostReport(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		if got := report.Settings.UnitCostMetricID; got == nil || *got != metric.Primary.ID {
			return fmt.Errorf("cost report %s charts metric %v, want %s", rs.Primary.ID, got, metric.Primary.ID)
		}
		if !slices.EqualFunc(report.Settings.UnitCostLabelFilter, filter, func(a, b costfluent.UnitCostLabelCondition) bool {
			return a.Key == b.Key && slices.Equal(a.Values, b.Values)
		}) {
			return fmt.Errorf("cost report %s label filter = %+v, want %+v", rs.Primary.ID, report.Settings.UnitCostLabelFilter, filter)
		}
		return nil
	}
}

// A gross margin divides revenue less cost by revenue, so its metric is counted in the workspace's
// currency. An update naming the metric but not the calculation keeps the margin, and per unit takes
// a scale.
func TestAccBusinessMetric_unitCostCalculation(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_cost_report", costReportExists),
		Steps: []resource.TestStep{
			{
				Config: testAccUnitCostCalculationConfig(rName, "Margin", `unit_cost_calculation = "grossMargin"`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair(
						"costfluent_business_metric.revenue", "unit", "costfluent_workspace.fixture", "currency"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "grossMargin"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_scale", "1"),
				),
			},
			testAccImportPlansNothing("costfluent_cost_report.test"),
			{
				Config: testAccUnitCostCalculationConfig(rName, "Margin renamed", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_report.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "title", "Margin renamed"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "grossMargin"),
				),
			},
			{
				Config: testAccUnitCostCalculationConfig(rName, "Cost per thousand euros of revenue",
					"unit_cost_calculation = \"perUnit\"\n    unit_cost_scale       = 1000"),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_calculation", "perUnit"),
					resource.TestCheckResourceAttr("costfluent_cost_report.test", "settings.unit_cost_scale", "1000"),
				),
			},
		},
	})
}

// testAccUnitCostCalculationConfig is a revenue metric in the workspace's currency and a cost report
// charting it, with settings added beside the metric.
func testAccUnitCostCalculationConfig(name, title, settings string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "revenue" {
  workspace_id = costfluent_workspace.fixture.id
  name         = "%s revenue"
  unit         = costfluent_workspace.fixture.currency
}

resource "costfluent_cost_report" "test" {
  workspace_id = costfluent_workspace.fixture.id
  title        = %q
  settings = {
    unit_cost_metric_id   = costfluent_business_metric.revenue.id
    %s
  }
}
`, name, title, settings)
}

// The provider's label column rule held to the API's from both sides: the most columns at their
// longest apply and read back, the API itself refuses a reserved column and a repeat ignoring case,
// and the provider refuses the same at plan, before a replacement would destroy the metric.
func TestAccBusinessMetric_atTheLimits(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")
	columns := testAccDistinctValues("col", 5, 100)
	atTheLimits := testAccBusinessMetricColumnsConfig(rName, testAccHCLList(columns))

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_business_metric", businessMetricExists),
		Steps: []resource.TestStep{
			{
				Config: atTheLimits,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_business_metric.limits", "label_columns.#", "5"),
					resource.TestCheckResourceAttr("costfluent_business_metric.limits", "label_columns.4", columns[4]),
					testAccCheckBusinessMetricColumns("costfluent_business_metric.limits", columns),
					testAccCheckAPIRefusesLabelColumns("costfluent_business_metric.limits", rName, []string{"Date"}),
					testAccCheckAPIRefusesLabelColumns("costfluent_business_metric.limits", rName, []string{"region", "Region"}),
				),
			},
			{
				// The provider's message, not the API's: the plan is refused before the replacement.
				Config:      testAccBusinessMetricColumnsConfig(rName, `["region", "Date"]`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`label_columns\[1\]\s+"Date"\s+is\s+reserved`),
			},
			// The post-test destroy plans the last step's configuration, which the validator refuses.
			{
				Config:   atTheLimits,
				PlanOnly: true,
			},
		},
	})
}

func testAccBusinessMetricColumnsConfig(name, columns string) string {
	return testAccWorkspaceFixture(name) + fmt.Sprintf(`
resource "costfluent_business_metric" "limits" {
  workspace_id  = costfluent_workspace.fixture.id
  name          = %q
  unit          = "customer"
  label_columns = %s
}
`, name, columns)
}

// testAccCheckBusinessMetricColumns reads the metric address holds back from the API and compares
// its label columns, in order, to want.
func testAccCheckBusinessMetricColumns(address string, want []string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		metric, err := testAccClient().GetBusinessMetric(context.Background(), rs.Primary.Attributes["workspace_id"], rs.Primary.ID)
		if err != nil {
			return err
		}
		if !slices.Equal(metric.LabelColumns, want) {
			return fmt.Errorf("business metric %s label columns = %q, want %q", rs.Primary.ID, metric.LabelColumns, want)
		}
		return nil
	}
}

// testAccCheckAPIRefusesLabelColumns creates a metric with columns directly through the API, in the
// workspace of the metric address holds, and expects a 400 naming labelColumns. A metric the API
// accepts instead is deleted before the check fails.
func testAccCheckAPIRefusesLabelColumns(address, name string, columns []string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[address]
		if !ok {
			return fmt.Errorf("%s not in state", address)
		}
		ctx := context.Background()
		workspaceID := rs.Primary.Attributes["workspace_id"]
		created, err := testAccClient().CreateBusinessMetric(ctx, &costfluent.CreateBusinessMetricInput{
			WorkspaceID:  workspaceID,
			Name:         name + " refused",
			Unit:         "customer",
			LabelColumns: columns,
		})
		if err == nil {
			if deleteErr := testAccClient().DeleteBusinessMetric(ctx, workspaceID, created.ID); deleteErr != nil {
				return fmt.Errorf("the API created a metric with label columns %q; deleting %s: %w", columns, created.ID, deleteErr)
			}
			return fmt.Errorf("the API created a metric with label columns %q, want a refusal on labelColumns", columns)
		}
		var apiErr *costfluent.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("creating a metric with label columns %q: %w, want a 400", columns, err)
		}
		for _, fe := range apiErr.Errors {
			if fe.Name == "labelColumns" {
				return nil
			}
		}
		return fmt.Errorf("creating a metric with label columns %q was refused without naming labelColumns: %w", columns, err)
	}
}
