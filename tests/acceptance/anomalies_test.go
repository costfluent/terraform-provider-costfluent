package acceptance

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The acceptance organization has no anomalies to rely on, since detection needs a month of real
// cost, so the read is checked for its contract: it answers with a list, and naming a connection
// the organization does not have is an error rather than every connection.
func TestAccAnomalies_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "costfluent_anomalies" "open" { unacknowledged_only = true }`,
				Check:  resource.TestCheckResourceAttrSet("data.costfluent_anomalies.open", "anomalies.#"),
			},
			{
				Config:      `data "costfluent_anomalies" "unknown" { cloud_account_id = "prv_AAAAAAAAAAAAAAAAAAAAAA" }`,
				ExpectError: regexp.MustCompile(`(?i)not found|404`),
			},
		},
	})
}
