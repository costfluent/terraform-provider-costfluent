package datasources_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// prose matches sentence however Terraform wraps it.
func prose(sentence string) string {
	return strings.Join(strings.Fields(regexp.QuoteMeta(sentence)), `\s+`)
}

// Every API call a data source makes names the capability a 403 says the token lacks, and where to
// create a token that carries it, as the resources do.
func TestDataSourcesNameTheCapabilityTheTokenLacks(t *testing.T) {
	t.Parallel()
	workspaceScoped := fmt.Sprintf("workspace = %q", testWorkspace)
	for name, tc := range map[string]struct {
		provider   string
		config     string
		path       string
		capability costfluent.Capability
		summary    string
	}{
		"anomalies": {"", `data "costfluent_anomalies" "a" {}`, "/v1/anomalies", costfluent.CapabilityViewAnomalies, "Failed to list anomalies"},
		"aws_provider_info": {"", `data "costfluent_aws_provider_info" "aws" {}`, "/v1/providers/aws/connector",
			costfluent.CapabilityViewProviders, "Failed to read the AWS connector"},
		"cost_data": {workspaceScoped, `data "costfluent_cost_data" "c" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
}`, "/v1/costs", costfluent.CapabilityViewCosts, "Failed to query cost data"},
		"cost_summary": {"", `data "costfluent_cost_summary" "s" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
}`, "/v1/costs/summary", costfluent.CapabilityViewCosts, "Failed to get cost summary"},
		"entitlement": {"", `data "costfluent_entitlement" "e" {}`, "/v1/entitlement", costfluent.CapabilityViewSubscription, "Failed to read entitlement"},
		"provider by id": {"", fmt.Sprintf(`data "costfluent_provider" "p" { id = %q }`, awsProvider), "/v1/providers/" + awsProvider,
			costfluent.CapabilityViewProviders, "Failed to read provider"},
		"provider by display name": {"", `data "costfluent_provider" "p" { display_name = "AWS prod" }`, "/v1/providers",
			costfluent.CapabilityViewProviders, "Failed to list providers"},
		"providers": {"", `data "costfluent_providers" "all" {}`, "/v1/providers", costfluent.CapabilityViewProviders, "Failed to list providers"},
		"user":      {"", `data "costfluent_user" "u" { email = "lena@example.com" }`, "/v1/users", costfluent.CapabilityViewMembers, "Failed to list users"},
		"workspace by id": {"", fmt.Sprintf(`data "costfluent_workspace" "w" { id = %q }`, testWorkspace), "/v1/workspaces/" + testWorkspace,
			costfluent.CapabilityViewWorkspace, "Failed to read workspace"},
		"workspace by name": {"", `data "costfluent_workspace" "w" { name = "Finance" }`, "/v1/workspaces",
			costfluent.CapabilityViewWorkspace, "Failed to list workspaces"},
		"workspaces": {"", `data "costfluent_workspaces" "all" {}`, "/v1/workspaces", costfluent.CapabilityViewWorkspace, "Failed to list workspaces"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET "+tc.path, func(fakeapi.Request) fakeapi.Response { return fakeapi.MissingCapability(tc.capability) })

			providerBlock := api.ProviderBlock()
			if tc.provider != "" {
				providerBlock = api.ProviderBlock(tc.provider)
			}
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: providerBlock + tc.config + "\n",
					ExpectError: regexp.MustCompile(`(?s)` + prose(tc.summary) + `.*` +
						prose("The API token lacks `"+string(tc.capability)+"`; create a token that carries it in Settings, API tokens.")),
				}},
			})
		})
	}
}
