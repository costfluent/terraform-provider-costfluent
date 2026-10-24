package acceptance

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWorkspace_basic(t *testing.T) {
	rName := acctest.RandomWithPrefix("tf-acc-test")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
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

					// Its calendar runs in UTC unless a zone is configured.
					resource.TestCheckResourceAttr("costfluent_workspace.test", "time_zone", "UTC"),

					// New reports start on the product's defaults: this month by day, refunds out, compared.
					resource.TestCheckResourceAttr("costfluent_workspace.test", "report_defaults.date_interval", "thisMonth"),
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

  enable_automatic_syncing = false
  time_zone                = "Europe/Berlin"

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
