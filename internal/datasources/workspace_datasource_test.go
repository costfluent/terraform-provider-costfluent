package datasources_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func workspaceJSON(id, name string) map[string]any {
	return map[string]any{
		"id":                       id,
		"name":                     name,
		"currency":                 "EUR",
		"timeZone":                 "Europe/Berlin",
		"enableCurrencyConversion": false,
		"enableAutomaticSyncing":   true,
		"blendCommitmentDiscounts": false,
		"providerCount":            2,
		"createdAt":                "2026-09-01T08:30:00Z",
	}
}

// fakeWorkspacePages serves /v1/workspaces as two pages, the second holding "Platform".
func fakeWorkspacePages(api *fakeapi.Server) {
	api.Handle("GET /v1/workspaces", func(r fakeapi.Request) fakeapi.Response {
		if r.Query.Get("page") == "2" {
			platform := workspaceJSON("wsp_0000000000000000000002", "Platform")
			platform["updatedAt"] = "2026-10-01T12:00:00Z"
			return page("/v1/workspaces", "", platform)
		}
		return page("/v1/workspaces", "/v1/workspaces?page=2&limit=100",
			workspaceJSON(testWorkspace, "Finance"))
	})
}

func TestWorkspaceDataSourceReadsByID(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/workspaces/{id}", func(r fakeapi.Request) fakeapi.Response {
		if r.PathValue("id") != testWorkspace {
			return fakeapi.NotFound()
		}
		return fakeapi.JSON(workspaceJSON(testWorkspace, "Finance"))
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_workspace" "w" {
  id = %q
}
`, testWorkspace),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "id", testWorkspace),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "name", "Finance"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "time_zone", "Europe/Berlin"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "enable_automatic_syncing", "true"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "blend_commitment_discounts", "false"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "created_at", "2026-09-01T08:30:00Z"),
				resource.TestCheckNoResourceAttr("data.costfluent_workspace.w", "updated_at"),
				func(*terraform.State) error {
					if n := len(api.Requests(http.MethodGet, "/v1/workspaces")); n != 0 {
						return fmt.Errorf("a lookup by ID listed workspaces %d times", n)
					}
					return nil
				},
			),
		}},
	})
}

// The data source reads the blend as the workspace holds it, by ID and by name alike.
func TestWorkspaceBlendCommitmentDiscountsIsReadByTheDataSource(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	blended := workspaceJSON(testWorkspace, "Finance")
	blended["blendCommitmentDiscounts"] = true
	api.Handle("GET /v1/workspaces/{id}", func(r fakeapi.Request) fakeapi.Response {
		if r.PathValue("id") != testWorkspace {
			return fakeapi.NotFound()
		}
		return fakeapi.JSON(blended)
	})
	api.Handle("GET /v1/workspaces", func(fakeapi.Request) fakeapi.Response {
		return page("/v1/workspaces", "", blended)
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_workspace" "by_id" {
  id = %q
}

data "costfluent_workspace" "by_name" {
  name = "Finance"
}
`, testWorkspace),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_workspace.by_id", "blend_commitment_discounts", "true"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.by_name", "blend_commitment_discounts", "true"),
			),
		}},
	})
}

// A name is looked up across every page of the listing, not only the first.
func TestWorkspaceDataSourceFindsANameOnALaterPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspacePages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_workspace" "w" {
  name = "Platform"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "id", "wsp_0000000000000000000002"),
				resource.TestCheckResourceAttr("data.costfluent_workspace.w", "updated_at", "2026-10-01T12:00:00Z"),
				func(*terraform.State) error {
					pages := map[string]bool{}
					for _, r := range api.Requests(http.MethodGet, "/v1/workspaces") {
						if r.Query.Get("limit") != "100" {
							return fmt.Errorf("listing limit = %q, want 100", r.Query.Get("limit"))
						}
						pages[r.Query.Get("page")] = true
					}
					if len(pages) != 2 || !pages["1"] || !pages["2"] {
						return fmt.Errorf("pages requested = %v, want 1 and 2", pages)
					}
					return nil
				},
			),
		}},
	})
}

func TestWorkspaceDataSourceRefusesAnUnknownName(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspacePages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_workspace" "w" {
  name = "Marketing"
}
`,
			ExpectError: regexp.MustCompile(`No workspace found with name "Marketing"`),
		}},
	})
}

func TestWorkspaceDataSourceNeedsAnIDOrAName(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_workspace" "w" {}`,
			ExpectError: regexp.MustCompile(`Either id or name must be specified`),
		}},
	})
}

func TestWorkspaceDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		attribute string
		value     string
		path      string
		want      string
	}{
		"missing ID":        {"id", "wsp_0000000000000000000009", "/v1/workspaces/wsp_0000000000000000000009", `Failed to read workspace(.|\n)*does not exist`},
		"listing forbidden": {"name", "Finance", "/v1/workspaces", `Failed to list workspaces(.|\n)*token lacks workspaces:read`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET "+tc.path, func(fakeapi.Request) fakeapi.Response {
				if tc.attribute == "id" {
					return fakeapi.NotFound()
				}
				return fakeapi.Problem(http.StatusForbidden, "Forbidden", "token lacks workspaces:read")
			})

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_workspace" "w" {
  %s = %q
}
`, tc.attribute, tc.value),
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}

func TestWorkspacesDataSourceListsEveryPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspacePages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `data "costfluent_workspaces" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.#", "2"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.id", testWorkspace),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.name", "Finance"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.currency", "EUR"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.time_zone", "Europe/Berlin"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.0.created_at", "2026-09-01T08:30:00Z"),
				resource.TestCheckNoResourceAttr("data.costfluent_workspaces.all", "workspaces.0.updated_at"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.1.name", "Platform"),
				resource.TestCheckResourceAttr("data.costfluent_workspaces.all", "workspaces.1.updated_at", "2026-10-01T12:00:00Z"),
				sentQuery(api, "/v1/workspaces", url.Values{"limit": {"100"}}),
			),
		}},
	})
}

func TestWorkspacesDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/workspaces", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusUnauthorized, "Unauthorized", "API key revoked")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      api.ProviderBlock() + `data "costfluent_workspaces" "all" {}`,
			ExpectError: regexp.MustCompile(`Failed to list workspaces(.|\n)*API key revoked`),
		}},
	})
}
