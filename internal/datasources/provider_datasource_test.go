package datasources_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const (
	awsProvider = "prv_0000000000000000000001"
	gcpProvider = "prv_0000000000000000000002"
)

// syncedProviderJSON is a provider that has synced, with every optional field the response carries.
func syncedProviderJSON() map[string]any {
	return map[string]any{
		"id":                   awsProvider,
		"type":                 "cloud",
		"key":                  "aws",
		"displayName":          "AWS prod",
		"description":          "Management account",
		"status":               "active",
		"lastSyncAt":           "2026-10-09T03:00:00Z",
		"lastSyncStatus":       "succeeded",
		"nextSyncAt":           "2026-10-10T03:00:00Z",
		"syncFrequencyMinutes": 1440,
		"createdAt":            "2026-09-01T08:30:00Z",
		"updatedAt":            "2026-10-09T03:05:00Z",
	}
}

// newProviderJSON is a provider that has not synced yet: the optional fields are absent.
func newProviderJSON() map[string]any {
	return map[string]any{
		"id":                   gcpProvider,
		"type":                 "cloud",
		"key":                  "gcp",
		"displayName":          "GCP billing",
		"status":               "pending",
		"syncFrequencyMinutes": 360,
		"createdAt":            "2026-10-08T10:00:00Z",
	}
}

func fakeProviderPages(api *fakeapi.Server) {
	api.Handle("GET /v1/providers", func(r fakeapi.Request) fakeapi.Response {
		if r.Query.Get("page") == "2" {
			return page("/v1/providers", "", newProviderJSON())
		}
		return page("/v1/providers", "/v1/providers?page=2&limit=100", syncedProviderJSON())
	})
}

func TestProviderDataSourceReadsByID(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	api.Handle("GET /v1/providers/"+awsProvider, func(fakeapi.Request) fakeapi.Response {
		return fakeapi.JSON(syncedProviderJSON())
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
data "costfluent_provider" "p" {
  id = %q
}
`, awsProvider),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "key", "aws"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "display_name", "AWS prod"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "description", "Management account"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "status", "active"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "sync_frequency_minutes", "1440"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "last_sync_at", "2026-10-09T03:00:00Z"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "last_sync_status", "succeeded"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "next_sync_at", "2026-10-10T03:00:00Z"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "created_at", "2026-09-01T08:30:00Z"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "updated_at", "2026-10-09T03:05:00Z"),
			),
		}},
	})
}

// A display name is looked up across every page, and a provider that has never synced reads with its sync
// fields null, not as zero timestamps.
func TestProviderDataSourceFindsADisplayNameOnALaterPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviderPages(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + `
data "costfluent_provider" "p" {
  display_name = "GCP billing"
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "id", gcpProvider),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "key", "gcp"),
				resource.TestCheckResourceAttr("data.costfluent_provider.p", "status", "pending"),
				resource.TestCheckNoResourceAttr("data.costfluent_provider.p", "description"),
				resource.TestCheckNoResourceAttr("data.costfluent_provider.p", "last_sync_at"),
				resource.TestCheckNoResourceAttr("data.costfluent_provider.p", "last_sync_status"),
				resource.TestCheckNoResourceAttr("data.costfluent_provider.p", "next_sync_at"),
				resource.TestCheckNoResourceAttr("data.costfluent_provider.p", "updated_at"),
				sentQuery(api, "/v1/providers", url.Values{"limit": {"100"}}),
			),
		}},
	})
}

func TestProviderDataSourceRefusesBadLookups(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"unknown display name":        {`display_name = "Azure"`, `No provider found with display name "Azure"`},
		"neither id nor display name": {``, `Either id or display_name must be specified`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			fakeProviderPages(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      api.ProviderBlock() + "data \"costfluent_provider\" \"p\" {\n  " + tc.body + "\n}\n",
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}

// A connection made without a display name is named after its type, so one can match several
// connections. Taking the first would bind whichever account the listing happens to return first.
func TestProviderDataSourceRefusesADisplayNameSeveralProvidersShare(t *testing.T) {
	t.Parallel()
	const secondAWS = "prv_0000000000000000000003"
	aws := func(id, externalID string) map[string]any {
		p := map[string]any{
			"id": id, "type": "cloud", "key": "aws", "displayName": "Amazon Web Services", "status": "active",
			"syncFrequencyMinutes": 1440, "createdAt": "2026-09-01T08:30:00Z",
		}
		if externalID != "" {
			p["externalId"] = externalID
		}
		return p
	}
	// Terraform wraps the message, so a space in it may arrive as a line break.
	listed := func(id, externalID string) string {
		return id + `\s+\(external\s+id\s+` + externalID + `\)`
	}
	for name, tc := range map[string]struct {
		pages [][]map[string]any
		want  string
	}{
		"on one page": {
			[][]map[string]any{{aws(awsProvider, "111111111111"), aws(secondAWS, "222222222222")}},
			listed(awsProvider, "111111111111") + `,\s+` + listed(secondAWS, "222222222222"),
		},
		"across pages": {
			[][]map[string]any{{aws(awsProvider, "111111111111")}, {newProviderJSON(), aws(secondAWS, "222222222222")}},
			listed(awsProvider, "111111111111") + `,\s+` + listed(secondAWS, "222222222222"),
		},
		"no external id": {
			[][]map[string]any{{aws(awsProvider, ""), aws(secondAWS, "")}},
			awsProvider + `,\s+` + secondAWS + `\.`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET /v1/providers", func(r fakeapi.Request) fakeapi.Response {
				if len(tc.pages) == 2 && r.Query.Get("page") == "2" {
					return page("/v1/providers", "", tc.pages[1]...)
				}
				next := ""
				if len(tc.pages) == 2 {
					next = "/v1/providers?page=2&limit=100"
				}
				return page("/v1/providers", next, tc.pages[0]...)
			})

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      api.ProviderBlock() + "data \"costfluent_provider\" \"p\" {\n  display_name = \"Amazon Web Services\"\n}\n",
					ExpectError: regexp.MustCompile(`2\s+providers\s+are\s+named\s+"Amazon\s+Web\s+Services":\s+` + tc.want + `(.|\n)*Set\s+id`),
				}},
			})
		})
	}
}

func TestProviderDataSourceSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body string
		path string
		resp fakeapi.Response
		want string
	}{
		"missing ID": {
			fmt.Sprintf("id = %q", awsProvider), "/v1/providers/" + awsProvider, fakeapi.NotFound(),
			`Failed to read provider(.|\n)*does not exist`,
		},
		"listing forbidden": {
			`display_name = "AWS prod"`, "/v1/providers",
			fakeapi.Problem(http.StatusForbidden, "Forbidden", "token lacks providers:read"),
			`Failed to list providers(.|\n)*token lacks providers:read`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Handle("GET "+tc.path, func(fakeapi.Request) fakeapi.Response { return tc.resp })

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      api.ProviderBlock() + "data \"costfluent_provider\" \"p\" {\n  " + tc.body + "\n}\n",
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
}
