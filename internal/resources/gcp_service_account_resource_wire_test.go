package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const gcpServiceAccountEmail = "cf-org-0001@costfluent-collect.iam.gserviceaccount.com"

func gcpServiceAccountConfig(api *fakeapi.Server) string {
	return api.ProviderBlock() + `
resource "costfluent_gcp_service_account" "sa" {}
`
}

// The account is provisioned once, read from state afterwards, and kept on destroy: it belongs to
// the organization, not to this configuration.
func TestGcpServiceAccountIsProvisionedOnceAndKeptOnDestroy(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("POST /v1/providers/gcp/service-account", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"serviceAccountEmail": gcpServiceAccountEmail,
			"organizationId":      "123456789012",
		})
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: gcpServiceAccountConfig(api),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_gcp_service_account.sa", "id", gcpServiceAccountEmail),
					resource.TestCheckResourceAttr("costfluent_gcp_service_account.sa", "email", gcpServiceAccountEmail),
					resource.TestCheckResourceAttr("costfluent_gcp_service_account.sa", "organization_id", "123456789012"),
					resource.TestCheckNoResourceAttr("costfluent_gcp_service_account.sa", "directory_customer_id"),
				),
			},
			{
				Config:   gcpServiceAccountConfig(api),
				PlanOnly: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if n := len(api.Requests(http.MethodPost, "/v1/providers/gcp/service-account")); n != 1 {
				return fmt.Errorf("service account provisioned %d times, want once", n)
			}
			if rs := api.Requests(http.MethodDelete, ""); len(rs) != 0 {
				return fmt.Errorf("destroy sent %s %s, want no request", rs[0].Method, rs[0].Path)
			}
			return nil
		},
	})
}

func TestGcpServiceAccountProvisioningFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("POST /v1/providers/gcp/service-account", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusForbidden, "Forbidden", "Only an organization owner can provision the service account")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: gcpServiceAccountConfig(api),
				ExpectError: regexp.MustCompile(
					`Failed to provision the GCP service account(.|\n)*Only an organization owner can provision`),
			},
		},
	})
}
