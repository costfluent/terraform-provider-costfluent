package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const gcpServiceAccountEmail = "cf-org-0001@costfluent-collect.iam.gserviceaccount.com"

func gcpServiceAccountConfig(api *fakeapi.Server) string {
	return api.ProviderBlock() + `
resource "costfluent_gcp_service_account" "sa" {}
`
}

// The account is provisioned on create and again on every refresh, which recreates one Costfluent
// reclaimed before the plan's grants apply, and kept on destroy: it belongs to the organization, not
// to this configuration.
func TestGcpServiceAccountIsProvisionedOnEveryRefreshAndKeptOnDestroy(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("POST /v1/providers/gcp/service-account", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(map[string]any{
			"serviceAccountEmail": gcpServiceAccountEmail,
			"organizationId":      "123456789012",
		})
	})
	var refreshedFrom int

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
				PreConfig:    func() { refreshedFrom = len(api.Requests(http.MethodPost, "/v1/providers/gcp/service-account")) },
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_gcp_service_account.sa", "id", gcpServiceAccountEmail),
					resource.TestCheckResourceAttr("costfluent_gcp_service_account.sa", "email", gcpServiceAccountEmail),
					func(*terraform.State) error {
						if n := len(api.Requests(http.MethodPost, "/v1/providers/gcp/service-account")); n <= refreshedFrom {
							return fmt.Errorf("the refresh sent no provisioning call (%d before, %d after)", refreshedFrom, n)
						}
						return nil
					},
				),
			},
			{
				Config:   gcpServiceAccountConfig(api),
				PlanOnly: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
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

// A refresh whose provisioning call fails, such as the alerted answer while the organization's
// account is missing but still in use, fails the plan with the API's message and leaves state alone.
func TestGcpServiceAccountRefreshFailure_ShowsTheApiMessage(t *testing.T) {
	t.Parallel()
	const alerted = "Costfluent cannot create GCP service accounts at the moment and has been alerted."
	var failing atomic.Bool
	api := fakeapi.New(t)
	api.Handle("POST /v1/providers/gcp/service-account", func(fakeapi.Request) fakeapi.Response {
		if failing.Load() {
			return fakeapi.Problem(http.StatusServiceUnavailable, "provider_unavailable", alerted)
		}
		return fakeapi.JSON(map[string]any{"serviceAccountEmail": gcpServiceAccountEmail})
	})

	const sa = "costfluent_gcp_service_account.sa"
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: gcpServiceAccountConfig(api),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(sa, "id", gcpServiceAccountEmail),
					resource.TestCheckResourceAttr(sa, "email", gcpServiceAccountEmail),
					resource.TestCheckNoResourceAttr(sa, "organization_id"),
					resource.TestCheckNoResourceAttr(sa, "directory_customer_id"),
				),
			},
			{
				PreConfig:   func() { failing.Store(true) },
				Config:      gcpServiceAccountConfig(api),
				ExpectError: regexp.MustCompile(`Failed to provision the GCP service account(.|\n)*has been alerted`),
			},
			// The stored state, read by a plan that does not refresh: every refresh provisions, so one
			// would rebuild a state the failed refresh had dropped or rewritten, and the next step's
			// Create would hide the loss.
			{
				PreConfig: func() { failing.Store(false) },
				Config:    gcpServiceAccountConfig(api),
				PlanOnly:  true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(sa, plancheck.ResourceActionNoop),
						plancheck.ExpectKnownValue(sa, tfjsonpath.New("id"), knownvalue.StringExact(gcpServiceAccountEmail)),
						plancheck.ExpectKnownValue(sa, tfjsonpath.New("email"), knownvalue.StringExact(gcpServiceAccountEmail)),
						plancheck.ExpectKnownValue(sa, tfjsonpath.New("organization_id"), knownvalue.Null()),
						plancheck.ExpectKnownValue(sa, tfjsonpath.New("directory_customer_id"), knownvalue.Null()),
					},
				},
			},
			{
				PreConfig: func() { failing.Store(false) },
				Config:    gcpServiceAccountConfig(api),
				Check:     resource.TestCheckResourceAttr(sa, "email", gcpServiceAccountEmail),
			},
		},
	})
}
