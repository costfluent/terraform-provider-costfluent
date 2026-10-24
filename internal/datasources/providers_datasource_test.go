package datasources_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestProvidersDataSourceListsEveryPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviderPages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_providers" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.id", awsProvider),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.key", "aws"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.name", "AWS prod"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.description", "Management account"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.status", "active"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.sync_frequency_minutes", "1440"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.last_sync_at", "2026-10-09T03:00:00Z"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.last_sync_status", "succeeded"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.0.created_at", "2026-09-01T08:30:00Z"),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.1.id", gcpProvider),
				resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.1.status", "pending"),
				resource.TestCheckNoResourceAttr("data.costfluent_providers.all", "providers.1.description"),
				resource.TestCheckNoResourceAttr("data.costfluent_providers.all", "providers.1.last_sync_at"),
				resource.TestCheckNoResourceAttr("data.costfluent_providers.all", "providers.1.last_sync_status"),
				sentQuery(api, "/v1/providers", url.Values{"limit": {"100"}}),
			),
		}},
	})
}

func TestProvidersDataSourceIsEmptyWithoutProviders(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/providers", func(fakeapi.Request) fakeapi.Response { return page("/v1/providers", "") })

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_providers" "all" {}`,
			Check:  resource.TestCheckResourceAttr("data.costfluent_providers.all", "providers.#", "0"),
		}},
	})
}

func TestProvidersDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/providers", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusForbidden, "Forbidden", "token lacks providers:read")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_providers" "all" {}`,
			ExpectError: regexp.MustCompile(`Failed to list providers(.|\n)*token lacks providers:read`),
		}},
	})
}
