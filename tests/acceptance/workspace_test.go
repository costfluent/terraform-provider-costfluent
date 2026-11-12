package acceptance

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// workspaceExists reads a workspace by ID; it is its own workspace, so the read takes no other.
var workspaceExists = existsByGet(func(c *costfluent.Client, ctx context.Context, _, id string) (*costfluent.Workspace, error) {
	return c.GetWorkspace(ctx, id)
})

func TestAccWorkspace_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_workspace", workspaceExists),
		Steps: []resource.TestStep{
			// Create
			{
				Config: testAccWorkspaceConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_workspace.test", "id"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "name", rName),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "currency", "USD"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "provider_count", "0"),

					// A new workspace converts into its own currency from the start.
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "enable_currency_conversion", "true"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "conversion_currency", "USD"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "conversion_method", "monthlyAverage"),

					// A new workspace syncs automatically until somebody turns it off.
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "enable_automatic_syncing", "true"),

					// Its amortized figures keep each commitment discount where AWS applied it until an
					// Owner turns blending on.
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "blend_commitment_discounts", "false"),

					// Its calendar runs in UTC unless a zone is configured.
					resource.TestCheckResourceAttr("costfluent_workspace.test", "time_zone", "UTC"),

					// New reports start on the product's defaults: the last 30 days by day, refunds out, compared.
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_interval", "last30Days"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_bin", "day"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.include_refunds", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.compare_to_last_period", "true"),

					// Anomaly detection leaves out marketplace charges and commitment fees and notifies every anomaly.
					resource.TestCheckResourceAttr("costfluent_workspace.test", "anomalies_exclude_marketplace", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "anomalies_exclude_commitment_fees", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "anomaly_notification_threshold", "0"),
				),
			},
			// Import
			{
				ResourceName:      "costfluent_workspace.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update
			{
				Config: testAccWorkspaceConfigUpdated(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.test", "name", rName+"-updated"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "conversion_currency", "EUR"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "conversion_method", "transactionDate"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "enable_automatic_syncing", "false"),
					resource.TestCheckResourceAttr(
						"costfluent_workspace.test", "blend_commitment_discounts", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "time_zone", "Europe/Berlin"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_interval", "lastMonth"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.amortize", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.include_tax", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "anomalies_exclude_marketplace", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "anomaly_notification_threshold", "250"),
				),
			},
			// The API reads enums and currencies in any case and stores a zone trimmed, each in its own
			// spelling; state keeps the configured one, so the next plan is empty.
			{
				Config: testAccWorkspaceConfigSpelled(rName, `
  conversion_currency = "eur"
  conversion_method   = "TransactionDate"
  time_zone           = " Europe/Berlin "
  report_defaults = {
    date_interval = "LastMonth"
    date_bin      = "Week"
  }
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.test", "conversion_currency", "eur"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "conversion_method", "TransactionDate"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "time_zone", " Europe/Berlin "),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_interval", "LastMonth"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_bin", "Week"),
				),
			},
			// The display currency changes only with conversion off, and is stored upper-cased.
			{
				Config: testAccWorkspaceConfigSpelled(rName, `
  enable_currency_conversion = false
  currency                   = "eur"
`),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.test", "enable_currency_conversion", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.test", "currency", "eur"),
				),
			},
		},
	})
}

func testAccWorkspaceConfig(name string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "test" {
  name = %q
}
`, name)
}

func testAccWorkspaceConfigUpdated(name string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "test" {
  name                = %q
  conversion_currency = "EUR"
  conversion_method   = "transactionDate"

  enable_automatic_syncing   = false
  blend_commitment_discounts = true
  time_zone                  = "Europe/Berlin"

  report_defaults = {
    date_interval = "lastMonth"
    amortize      = false
  }

  anomalies_exclude_marketplace  = false
  anomaly_notification_threshold = "250"
}
`, name+"-updated")
}

func testAccWorkspaceConfigSpelled(name, attrs string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "test" {
  name = %q
%s}
`, name+"-updated", attrs)
}

// TestAccWorkspace_basic applies both conversion methods the provider accepts, so it fails when the
// API stops taking one. This holds the other side: the API refuses monthEndRate, which the
// contract's FxMethod declares but only an operator-set policy picks, and the provider refuses it
// at plan instead of passing it to that refusal at apply.
func TestAccWorkspace_conversionMethods(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckGone("costfluent_workspace", workspaceExists),
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceFixture(rName),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.fixture", "enable_currency_conversion", "true"),
					func(s *terraform.State) error {
						id := s.RootModule().Resources["costfluent_workspace.fixture"].Primary.ID
						c := testAccClient()
						method := "monthEndRate"
						_, err := c.UpdateWorkspace(context.Background(), id,
							&costfluent.UpdateWorkspaceInput{ConversionMethod: &method})
						if err == nil {
							return fmt.Errorf("the API took conversionMethod %q, which the provider refuses at plan", method)
						}
						if !costfluent.IsBadRequest(err) || !refusalNamesField(err, "ConversionMethod") {
							return fmt.Errorf("conversionMethod %q answered %w, want a 400 naming ConversionMethod", method, err)
						}
						ws, err := c.GetWorkspace(context.Background(), id)
						if err != nil {
							return err
						}
						if ws.ConversionMethod != "monthlyAverage" {
							return fmt.Errorf("conversion method after the refused update = %q, want monthlyAverage", ws.ConversionMethod)
						}
						return nil
					},
				),
			},
			{
				Config:      testAccWorkspaceFixtureWith(rName, `  conversion_method = "monthEndRate"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Attribute conversion_method value must be one of`),
			},
			// The post-test destroy plans the last step's configuration, which the validator would
			// refuse; ending on the valid one also holds that the refused update left no drift.
			{
				Config:   testAccWorkspaceFixture(rName),
				PlanOnly: true,
			},
		},
	})
}

// refusalNamesField reports whether err is an API problem with a field error on field.
func refusalNamesField(err error, field string) bool {
	var apiErr *costfluent.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, fe := range apiErr.Errors {
		if strings.EqualFold(fe.Name, field) {
			return true
		}
	}
	return false
}

// testAccWorkspaceFixtureWith is testAccWorkspaceFixture with attrs added to the workspace.
func testAccWorkspaceFixtureWith(name, attrs string) string {
	return fmt.Sprintf(`
resource "costfluent_workspace" "fixture" {
  name     = %q
  currency = "EUR"
%s
}
`, name+"-ws", attrs)
}
