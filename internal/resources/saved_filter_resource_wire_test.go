package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	savedFilterQuery  = "costs.provider = 'aws' AND costs.service = 'Amazon EC2'"
	savedFilterStored = `[{"field":"Provider","operator":"In","values":["aws"]},` +
		`{"field":"ServiceName","operator":"In","values":["Amazon EC2"]}]`
	savedFilterGCPStored = `[{"field":"Provider","operator":"In","values":["gcp"]}]`
)

// savedFilterDocuments is how the API stores a saved filter sent as CQL: as its JSON document.
var savedFilterDocuments = map[string]string{savedFilterQuery: savedFilterStored}

// translatedFilters serves the translate operation the provider uses to tell whether a configured
// CQL filter means the stored JSON: each CQL filter in documents translates to its JSON document,
// and any other text does not parse.
func translatedFilters(api *fakeapi.Server, documents map[string]string) {
	api.Handle("GET /v1/cost-filters/translate", func(r fakeapi.Request) fakeapi.Response {
		query := r.Query.Get("filter")
		stored, ok := documents[query]
		if !ok {
			return fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "The filter does not parse")
		}
		return fakeapi.JSON(map[string]any{"filter": stored, "query": query})
	})
}

// storedFilter is a sent filter as the API stores and answers it: a CQL filter in documents as its
// JSON document, a JSON document re-serialised, which here means in sorted key order, and no filter
// as none.
func storedFilter(documents map[string]string, sent any) any {
	text, ok := sent.(string)
	if !ok {
		return sent
	}
	if stored, ok := documents[text]; ok {
		return stored
	}
	var document any
	if json.Unmarshal([]byte(text), &document) == nil {
		canonical, _ := json.Marshal(document)
		return string(canonical)
	}
	return text
}

// savedFilterFake serves /v1/saved-filters with the SavedFilterCreatedResponse and
// SavedFilterResponse shapes, storing the filter as its JSON document as the API does, and the
// translate operation.
func savedFilterFake(api *fakeapi.Server) {
	api.Collection("/v1/saved-filters", fakeapi.Collection{
		IDPrefix:  "flt_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			return map[string]any{
				"title":     body["title"],
				"filter":    storedFilter(savedFilterDocuments, body["filter"]),
				"isDefault": body["isDefault"] == true,
				"createdAt": "2026-10-01T09:30:00Z",
			}
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "title": obj["title"]})
		},
		Apply: func(obj, body map[string]any) {
			for k, v := range body {
				if v != nil {
					obj[k] = v
				}
			}
			if f, sent := body["filter"]; sent && f != nil {
				obj["filter"] = storedFilter(savedFilterDocuments, f)
			}
		},
	})
	translatedFilters(api, savedFilterDocuments)
}

func savedFilterConfig(api *fakeapi.Server, title, extra string) string {
	return savedFilterConfigWith(api, title, savedFilterQuery, extra)
}

func savedFilterConfigWith(api *fakeapi.Server, title, filter, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_saved_filter" "f" {
  workspace_id = %q
  title        = %q
  filter       = %q
%s}
`, testWorkspace, title, filter, extra)
}

// savedFilterStoredAs checks the API holds the saved filter's filter as want.
func savedFilterStoredAs(t *testing.T, api *fakeapi.Server, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		stored, _ := api.Get("/v1/saved-filters", savedFilterOnlyID(t, api))
		if stored["filter"] != want {
			return fmt.Errorf("stored filter = %v, want %s", stored["filter"], want)
		}
		return nil
	}
}

func savedFilterOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/saved-filters")
	if len(ids) != 1 {
		t.Fatalf("saved filters = %v, want one", ids)
	}
	return ids[0]
}

func TestSavedFilterLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// The API answers with the filter's JSON document; the configured CQL means the same, so
				// state keeps it and the refresh plan the step runs after apply stays empty.
				Config: savedFilterConfig(api, "EC2", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "title", "EC2"),
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "filter", savedFilterQuery),
					savedFilterStoredAs(t, api, savedFilterStored),
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "is_default", "false"),
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "created_at", "2026-10-01T09:30:00Z"),
					resource.TestMatchResourceAttr("costfluent_saved_filter.f", "id", regexp.MustCompile(`^flt_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/saved-filters")
						if create.Body["workspaceId"] != testWorkspace || create.Body["title"] != "EC2" ||
							create.Body["filter"] != savedFilterQuery {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						if _, sent := create.Body["isDefault"]; sent {
							return fmt.Errorf("create sent isDefault false instead of omitting it: %s", create.Raw)
						}
						read := api.LastRequest(http.MethodGet, "/v1/saved-filters/"+savedFilterOnlyID(t, api))
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						return nil
					},
				),
			},
			{
				Config: savedFilterConfig(api, "EC2", "  is_default = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "is_default", "true"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/saved-filters/"+savedFilterOnlyID(t, api))
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						if update.Body["isDefault"] != true {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						for _, unchanged := range []string{"title", "filter"} {
							if _, sent := update.Body[unchanged]; sent {
								return fmt.Errorf("update sent unchanged %s: %s", unchanged, update.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_saved_filter.f",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + savedFilterOnlyID(t, api), nil
				},
				// An import has no configured CQL to keep, so it reads the filter as the stored JSON.
				ImportStateVerifyIgnore: []string{"filter"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["filter"]; got != savedFilterStored {
						return fmt.Errorf("imported filter = %q, want the stored JSON", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/saved-filters"); len(ids) != 0 {
				return fmt.Errorf("saved filters left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A bare ID imports into the provider's workspace, which the read then sends as workspaceId.
func TestSavedFilterImportsABareIDInTheProviderWorkspace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)
	api.PutIn("/v1/saved-filters", testWorkspace, "flt_0000000000000000000099", map[string]any{
		"title": "EC2", "filter": savedFilterStored, "isDefault": false, "createdAt": "2026-10-01T09:30:00Z",
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
resource "costfluent_saved_filter" "f" {
  title  = "EC2"
  filter = "provider = 'aws'"
}
`,
				ResourceName:  "costfluent_saved_filter.f",
				ImportState:   true,
				ImportStateId: "flt_0000000000000000000099",
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 || states[0].Attributes["title"] != "EC2" ||
						states[0].Attributes["filter"] != savedFilterStored {
						return fmt.Errorf("imported = %v", states)
					}
					read := api.LastRequest(http.MethodGet, "/v1/saved-filters/flt_0000000000000000000099")
					if read.Query.Get("workspaceId") != testWorkspace {
						return fmt.Errorf("read query = %v", read.Query)
					}
					return nil
				},
			},
		},
	})
}

func TestSavedFilterDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: savedFilterConfig(api, "EC2", "")},
			{
				PreConfig:          func() { api.Remove("/v1/saved-filters", savedFilterOnlyID(t, api)) },
				Config:             savedFilterConfig(api, "EC2", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestSavedFilterUpdateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: savedFilterConfig(api, "EC2", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/saved-filters/"+savedFilterOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "The filter does not parse"))
				},
				Config:      savedFilterConfig(api, "EC2 renamed", ""),
				ExpectError: regexp.MustCompile(`Failed to update saved filter(.|\n)*The filter does not parse`),
			},
		},
	})
}

func TestSavedFilterRefusesAWorkspaceIDWithoutItsPrefix(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + `
resource "costfluent_saved_filter" "f" {
  workspace_id = "fld_0000000000000000000001"
  title        = "EC2"
  filter       = "provider = 'aws'"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid token format`),
			},
		},
	})
}

// The API refuses a blank filter, so it is refused at plan rather than at apply.
func TestSavedFilterRefusesAnEmptyFilter(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + `
resource "costfluent_saved_filter" "f" {
  title  = "EC2"
  filter = ""
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`string length must be at least 1`),
			},
		},
	})
}

func TestSavedFilterCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/saved-filters", "flt_", "costfluent_saved_filter.f",
		savedFilterConfig(api, "EC2", ""), "Failed to create saved filter")
}

// A filter written as the JSON document in another key order means the stored one, so it is kept as
// written and plans nothing.
func TestSavedFilterJSONFilterInAnotherKeyOrderPlansNoChange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)
	reordered := `[{"values":["aws"],"operator":"In","field":"Provider"},` +
		`{"field":"ServiceName","values":["Amazon EC2"],"operator":"In"}]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: savedFilterConfigWith(api, "EC2", reordered, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "filter", reordered),
					savedFilterStoredAs(t, api, savedFilterStored),
				),
			},
		},
	})
}

// A filter edited outside Terraform no longer means the configured CQL, so a refresh takes the
// API's document into state and the plan shows an update, which writes the configuration back.
func TestSavedFilterChangedOutsideTerraformShowsInStateAndPlansAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: savedFilterConfig(api, "EC2", "")},
			{
				PreConfig: func() {
					api.Update("/v1/saved-filters", savedFilterOnlyID(t, api), func(obj map[string]any) {
						obj["filter"] = savedFilterGCPStored
					})
				},
				RefreshState:       true,
				Check:              resource.TestCheckResourceAttr("costfluent_saved_filter.f", "filter", savedFilterGCPStored),
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{PostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_saved_filter.f", plancheck.ResourceActionUpdate),
				}},
			},
			{
				Config: savedFilterConfig(api, "EC2", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_saved_filter.f", "filter", savedFilterQuery),
					savedFilterStoredAs(t, api, savedFilterStored),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/saved-filters/"+savedFilterOnlyID(t, api))
						if update.Body["filter"] != savedFilterQuery {
							return fmt.Errorf("update body = %s, want the configured filter", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestSavedFilterCreateWhoseFilterTranslationFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savedFilterFake(api)
	createWhoseFilterTranslationFailsIsTainted(t, api, "/v1/saved-filters", "flt_", "costfluent_saved_filter.f",
		savedFilterConfig(api, "EC2", ""), "Failed to compare the saved filter")
}
