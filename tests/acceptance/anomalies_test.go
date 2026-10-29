package acceptance

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The seeded sample organization's AWS connection, which bills, and its Kubernetes staging cluster,
// which reports but bills nothing.
const (
	testAccSampleConnection     = "AWS E2E Company"
	testAccSampleIdleConnection = "Staging EU"
)

// The acceptance organization has no anomalies to rely on, since detection needs a month of real
// cost, so the read is checked for its contract: it answers with a list, and naming a connection
// the organization does not have is an error rather than every connection.
func TestAccAnomalies_basic(t *testing.T) {
	resource.ParallelTest(t, resource.TestCase{
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

// The seeded sample organization's AWS connection bills, so it holds anomalies; its Kubernetes
// staging cluster reports but bills nothing, so it holds none. Which anomalies are open, and their
// figures, are drawn at random, so they are checked set, never valued.
func TestAccAnomalies_readTheSeededAnomalies(t *testing.T) {
	testAccUseSampleKey(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
data "costfluent_provider" "aws" {
  display_name = %q
}

data "costfluent_provider" "idle" {
  display_name = %q
}

data "costfluent_anomalies" "aws" {
  cloud_account_id = data.costfluent_provider.aws.id
}

data "costfluent_anomalies" "open" {
  unacknowledged_only = true
}

data "costfluent_anomalies" "idle" {
  cloud_account_id = data.costfluent_provider.idle.id
}
`, testAccSampleConnection, testAccSampleIdleConnection),
			Check: resource.ComposeAggregateTestCheckFunc(
				func(s *terraform.State) error {
					aws, ok := s.RootModule().Resources["data.costfluent_provider.aws"]
					if !ok {
						return fmt.Errorf("data.costfluent_provider.aws not in state")
					}
					return testAccCheckEveryElem("data.costfluent_anomalies.aws", "anomalies", 1, map[string]func(string) error{
						"provider_id":   testAccEquals(aws.Primary.ID),
						"service_name":  testAccNotEmpty,
						"actual_cost":   testAccNotEmpty,
						"expected_cost": testAccNotEmpty,
					})(s)
				},
				testAccCheckEveryElem("data.costfluent_anomalies.open", "anomalies", 1, map[string]func(string) error{
					"is_acknowledged": testAccEquals("false"),
				}),
				resource.TestCheckResourceAttr("data.costfluent_anomalies.idle", "anomalies.#", "0"),
			),
		}},
	})
}
