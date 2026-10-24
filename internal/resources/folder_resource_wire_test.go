package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testWorkspace = "wsp_0000000000000000000001"

// fakeFolders serves /v1/folders with the FolderCreatedResponse and FolderResponse shapes.
func fakeFolders(api *fakeapi.Server) {
	api.Collection("/v1/folders", fakeapi.Collection{
		IDPrefix:              "fld_",
		Versioned:             true,
		Scope:                 fakeapi.WorkspaceScoped,
		UpdateWorkspaceInBody: true,
		Create: func(body map[string]any) map[string]any {
			return map[string]any{"title": body["title"], "parentId": body["parentId"], "reportCount": 0}
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "title": obj["title"]})
		},
		Apply: func(obj, body map[string]any) {
			for _, k := range []string{"title", "parentId"} {
				if v, ok := body[k]; ok && v != nil {
					obj[k] = v
				}
			}
			if body["clearParent"] == true {
				delete(obj, "parentId")
			}
		},
	})
}

func folderConfig(api *fakeapi.Server, title string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_folder" "f" {
  workspace_id = %q
  title        = %q
}
`, testWorkspace, title)
}

// onlyFolderID is the ID of the one folder the fake holds.
func onlyFolderID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/folders")
	if len(ids) != 1 {
		t.Fatalf("folders = %v, want one", ids)
	}
	return ids[0]
}

func TestFolderLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: folderConfig(api, "Finance"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.f", "title", "Finance"),
					resource.TestCheckResourceAttr("costfluent_folder.f", "report_count", "0"),
					resource.TestCheckNoResourceAttr("costfluent_folder.f", "parent_id"),
					resource.TestMatchResourceAttr("costfluent_folder.f", "id", regexp.MustCompile(`^fld_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/folders")
						if create.Header.Get("Authorization") != "Bearer cf_org_test" {
							return fmt.Errorf("authorization = %q", create.Header.Get("Authorization"))
						}
						if create.Body["workspaceId"] != testWorkspace || create.Body["title"] != "Finance" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						if _, sent := create.Body["parentId"]; sent {
							return fmt.Errorf("create sent parentId for a top-level folder: %s", create.Raw)
						}
						read := api.LastRequest(http.MethodGet, "/v1/folders/"+onlyFolderID(t, api))
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						return nil
					},
				),
			},
			{
				Config: folderConfig(api, "Finance EU"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.f", "title", "Finance EU"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/folders/"+onlyFolderID(t, api))
						if update.Body["workspaceId"] != testWorkspace || update.Body["title"] != "Finance EU" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						if _, sent := update.Body["parentId"]; sent {
							return fmt.Errorf("update sent an unchanged parentId: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_folder.f",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + onlyFolderID(t, api), nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/folders"); len(ids) != 0 {
				return fmt.Errorf("folders left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A folder moves under another and back to the top in place, sending clearParent when its parent is
// removed, and keeps its id throughout.
func TestFolderParentMovesInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	const parent = "fld_0000000000000000000077"
	config := func(attrs string) string {
		return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_folder" "f" {
  workspace_id = %q
  title        = "Quarterly"
%s}
`, testWorkspace, attrs)
	}
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config(fmt.Sprintf("  parent_id = %q\n", parent)),
				Check:  func(*terraform.State) error { id = onlyFolderID(t, api); return nil },
			},
			{
				Config: config(""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_folder.f", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_folder.f", "parent_id"),
					func(*terraform.State) error {
						if got := onlyFolderID(t, api); got != id {
							return fmt.Errorf("folder id = %s, want the same %s", got, id)
						}
						update := api.LastRequest(http.MethodPut, "/v1/folders/"+id)
						if update.Body["clearParent"] != true {
							return fmt.Errorf("update body = %s, want clearParent", update.Raw)
						}
						if _, sent := update.Body["parentId"]; sent {
							return fmt.Errorf("update sent parentId beside clearParent: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{Config: config(""), PlanOnly: true},
		},
	})
}

// A folder deleted outside Terraform leaves state on the next refresh, so the plan recreates it
// rather than failing on a 404.
func TestFolderDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: folderConfig(api, "Finance")},
			{
				PreConfig:          func() { api.Remove("/v1/folders", onlyFolderID(t, api)) },
				Config:             folderConfig(api, "Finance"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestFolderCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	api.FailNext(http.MethodPost, "/v1/folders",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "Title must be unique in the workspace"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      folderConfig(api, "Finance"),
				ExpectError: regexp.MustCompile(`Failed to create folder(.|\n)*Title must be unique in the workspace`),
			},
		},
	})
}

func TestFolderCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/folders", "fld_", "costfluent_folder.f",
		folderConfig(api, "Finance"), "Failed to create folder")
}

// fakeFolderSavedFilter answers PUT /v1/folders/{id}/saved-filter as the API does: it stores the
// body's savedFilterId, null detaching it, and returns the folder.
func fakeFolderSavedFilter(api *fakeapi.Server) {
	api.Handle("PUT /v1/folders/{id}/saved-filter", func(r fakeapi.Request) fakeapi.Response {
		id := r.PathValue("id")
		if _, ok := api.Get("/v1/folders", id); !ok {
			return fakeapi.NotFound()
		}
		api.Update("/v1/folders", id, func(obj map[string]any) { obj["savedFilterId"] = r.Body["savedFilterId"] })
		obj, _ := api.Get("/v1/folders", id)
		return fakeapi.JSON(obj)
	})
}

func folderWithFilterConfig(api *fakeapi.Server, savedFilter string) string {
	attr := ""
	if savedFilter != "" {
		attr = fmt.Sprintf("  saved_filter_id = %q\n", savedFilter)
	}
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_folder" "f" {
  workspace_id = %q
  title        = "Finance"
%s}
`, testWorkspace, attr)
}

func TestFolderSavedFilterIsAttachedChangedAndDetached(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	fakeFolderSavedFilter(api)

	sent := func(want any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			put := api.LastRequest(http.MethodPut, "/v1/folders/"+onlyFolderID(t, api)+"/saved-filter")
			if value, ok := put.Body["savedFilterId"]; !ok || value != want {
				return fmt.Errorf("saved-filter body = %s, want savedFilterId %v", put.Raw, want)
			}
			if put.Body["workspaceId"] != testWorkspace {
				return fmt.Errorf("saved-filter body = %s, want the folder's workspace", put.Raw)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: folderWithFilterConfig(api, "flt_0000000000000000000001"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.f", "saved_filter_id", "flt_0000000000000000000001"),
					sent("flt_0000000000000000000001"),
				),
			},
			{
				Config: folderWithFilterConfig(api, "flt_0000000000000000000002"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_folder.f", "saved_filter_id", "flt_0000000000000000000002"),
					sent("flt_0000000000000000000002"),
				),
			},
			{
				Config: folderWithFilterConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_folder.f", "saved_filter_id"),
					sent(nil),
				),
			},
		},
	})
}

func TestFolderWithoutASavedFilterNeverCallsTheSavedFilterEndpoint(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	fakeFolderSavedFilter(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: folderWithFilterConfig(api, ""),
				Check: func(*terraform.State) error {
					if calls := api.Requests(http.MethodPut, "/v1/folders/"+onlyFolderID(t, api)+"/saved-filter"); len(calls) != 0 {
						return fmt.Errorf("saved-filter called %d times for a folder without one", len(calls))
					}
					return nil
				},
			},
		},
	})
}

// A folder whose saved filter cannot be attached is still recorded, without the filter, so the next
// apply attaches it instead of creating a second folder.
func TestFolderSavedFilterAttachFailureKeepsTheFolder(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	api.Handle("PUT /v1/folders/{id}/saved-filter", func(r fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusNotFound, "NotFound", "SavedFilter flt_0000000000000000000009 was not found")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      folderWithFilterConfig(api, "flt_0000000000000000000009"),
				ExpectError: regexp.MustCompile(`Failed to attach the saved filter to the folder(.|\n)*was not found`),
			},
			{
				// The folder is in state without the filter, so the plan only attaches it.
				PreConfig: func() {
					if ids := api.IDs("/v1/folders"); len(ids) != 1 {
						t.Fatalf("folders = %v, want the one created", ids)
					}
				},
				Config:             folderWithFilterConfig(api, "flt_0000000000000000000009"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestFolderSavedFilterUpdateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeFolders(api)
	fakeFolderSavedFilter(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: folderWithFilterConfig(api, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/folders/"+onlyFolderID(t, api)+"/saved-filter",
						fakeapi.Problem(http.StatusNotFound, "NotFound", "SavedFilter flt_0000000000000000000009 was not found"))
				},
				Config:      folderWithFilterConfig(api, "flt_0000000000000000000009"),
				ExpectError: regexp.MustCompile(`Failed to set the folder's saved filter(.|\n)*was not found`),
			},
		},
	})
}
