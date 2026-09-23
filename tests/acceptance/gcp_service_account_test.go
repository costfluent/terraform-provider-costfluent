package acceptance

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccGcpServiceAccount_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `resource "costfluent_gcp_service_account" "this" {}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_gcp_service_account.this", "email",
						regexp.MustCompile(`^cf[ldp]-[a-z2-7]{26}@costfluent-(dev|prod)-connect\.iam\.gserviceaccount\.com$`)),
					resource.TestCheckResourceAttrPair(
						"costfluent_gcp_service_account.this", "id", "costfluent_gcp_service_account.this", "email"),
				),
			},
			{
				Config:   `resource "costfluent_gcp_service_account" "this" {}`,
				PlanOnly: true,
			},
		},
	})
}

// A GCP connection needs a billing export shared with the organization's service account, which
// only the dogfood dataset has; it runs when its three values are set.
func TestAccProvider_gcp(t *testing.T) {
	billingAccount := os.Getenv("COSTFLUENT_ACC_GCP_BILLING_ACCOUNT_ID")
	project := os.Getenv("COSTFLUENT_ACC_GCP_PROJECT_ID")
	dataset := os.Getenv("COSTFLUENT_ACC_GCP_DATASET")
	if billingAccount == "" || project == "" || dataset == "" {
		t.Skip("COSTFLUENT_ACC_GCP_BILLING_ACCOUNT_ID, COSTFLUENT_ACC_GCP_PROJECT_ID and COSTFLUENT_ACC_GCP_DATASET must be set")
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "costfluent_gcp_service_account" "this" {}

resource "costfluent_provider" "gcp" {
  key  = "gcp"
  name = "GCP acceptance"
  credentials = {
    billing_account_id = %q
    project_id         = %q
    bigquery_dataset   = %q
  }
  depends_on = [costfluent_gcp_service_account.this]
}
`, billingAccount, project, dataset),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_provider.gcp", "id"),
					resource.TestCheckResourceAttr("costfluent_provider.gcp", "external_id", billingAccount),
				),
			},
		},
	})
}
