package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// dashboardEnum spells an enum as the API returns it, camelCase whatever case it was sent in.
func dashboardEnum(v any) any {
	s, ok := v.(string)
	if !ok || s == "" {
		return v
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// dashboardFake serves /v1/dashboards with the DashboardCreatedResponse and DashboardResponse
// shapes, defaulting the date range the way the API does, and PUT /v1/dashboards/{id}/default.
// A dashboard created or made default takes the default from the others, as the API does; every
// dashboard here is in testWorkspace.
func dashboardFake(api *fakeapi.Server) {
	api.Collection("/v1/dashboards", fakeapi.Collection{
		IDPrefix:  "dsh_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			if body["isDefault"] == true {
				takeDefault(api, "")
			}
			obj := map[string]any{
				"title":        body["title"],
				"isDefault":    body["isDefault"] == true,
				"dateInterval": "thisMonth",
				"dateBin":      "day",
				"widgets":      []any{},
				"createdAt":    "2026-10-02T08:00:00Z",
			}
			for _, k := range []string{"dateInterval", "dateBin"} {
				if v, ok := body[k]; ok {
					obj[k] = dashboardEnum(v)
				}
			}
			keepCustomDates(obj, body)
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "title": obj["title"]})
		},
		Apply: func(obj, body map[string]any) {
			if v, ok := body["title"]; ok && v != nil {
				obj["title"] = v
			}
			for _, k := range []string{"dateInterval", "dateBin"} {
				if v, ok := body[k]; ok && v != nil {
					obj[k] = dashboardEnum(v)
				}
			}
			keepCustomDates(obj, body)
		},
	})
	fakeDashboardDefault(api)
}

// keepCustomDates stores a range as the API does: a custom interval takes the dates sent with it,
// and a preset drops both, those sent beside it and those a custom range kept.
func keepCustomDates(obj, body map[string]any) {
	if obj["dateInterval"] != "custom" {
		delete(obj, "startDate")
		delete(obj, "endDate")
		return
	}
	for _, k := range []string{"startDate", "endDate"} {
		if v, ok := body[k]; ok && v != nil {
			obj[k] = v
		}
	}
}

// takeDefault clears the default of every dashboard but keep, bumping only the ones it changes.
func takeDefault(api *fakeapi.Server, keep string) {
	for _, id := range api.IDs("/v1/dashboards") {
		if obj, _ := api.Get("/v1/dashboards", id); id != keep && obj["isDefault"] == true {
			api.Update("/v1/dashboards", id, func(obj map[string]any) { obj["isDefault"] = false })
		}
	}
}

// fakeDashboardDefault answers PUT /v1/dashboards/{id}/default as the API does: the workspace and a
// required isDefault travel in the body, If-Match is honoured, true takes the default from the
// others and false clears it, and only the rows that change get a new version.
func fakeDashboardDefault(api *fakeapi.Server) {
	api.Handle("PUT /v1/dashboards/{id}/default", func(r fakeapi.Request) fakeapi.Response {
		id := r.PathValue("id")
		obj, ok := api.Get("/v1/dashboards", id)
		if !ok || r.Body["workspaceId"] != testWorkspace {
			return fakeapi.NotFound()
		}
		if r.Query.Has("workspaceId") {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "workspaceId belongs in the body")
		}
		isDefault, ok := r.Body["isDefault"].(bool)
		if !ok {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "isDefault is required")
		}
		if match := r.Header.Get("If-Match"); match != "" && match != api.ETag("/v1/dashboards", id) {
			return fakeapi.Problem(http.StatusPreconditionFailed, "precondition_failed", "The resource changed since it was read")
		}
		if isDefault {
			takeDefault(api, id)
		}
		if obj["isDefault"] != isDefault {
			api.Update("/v1/dashboards", id, func(obj map[string]any) { obj["isDefault"] = isDefault })
		}
		obj, _ = api.Get("/v1/dashboards", id)
		resp := fakeapi.JSON(obj)
		resp.Header = http.Header{"Etag": []string{api.ETag("/v1/dashboards", id)}}
		return resp
	})
}

func dashboardConfig(api *fakeapi.Server, title, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_dashboard" "d" {
  workspace_id = %q
  title        = %q
%s}
`, testWorkspace, title, extra)
}

func dashboardOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/dashboards")
	if len(ids) != 1 {
		t.Fatalf("dashboards = %v, want one", ids)
	}
	return ids[0]
}

func TestDashboardLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "Overview", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "title", "Overview"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "false"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_interval", "thisMonth"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_bin", "day"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "created_at", "2026-10-02T08:00:00Z"),
					resource.TestMatchResourceAttr("costfluent_dashboard.d", "id", regexp.MustCompile(`^dsh_.{22}$`)),
					func(*terraform.State) error {
						firstID = dashboardOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, "/v1/dashboards")
						if create.Body["workspaceId"] != testWorkspace || create.Body["title"] != "Overview" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, unset := range []string{"isDefault", "dateInterval", "dateBin"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent unset %s: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				// The API answers lastMonth; the configured spelling stays, so the next plan is empty.
				Config: dashboardConfig(api, "Overview", "  date_interval = \"LastMonth\"\n  date_bin = \"week\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_interval", "LastMonth"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_bin", "week"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/dashboards/"+firstID)
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						if update.Body["dateInterval"] != "LastMonth" || update.Body["dateBin"] != "week" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						if _, sent := update.Body["title"]; sent {
							return fmt.Errorf("update sent an unchanged title: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				// is_default is set in place, through the default endpoint, never by recreating.
				Config: dashboardConfig(api, "Overview", "  date_interval = \"LastMonth\"\n  date_bin = \"week\"\n  is_default = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.d", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "true"),
					func(*terraform.State) error {
						if id := dashboardOnlyID(t, api); id != firstID {
							return fmt.Errorf("dashboard %s replaced %s, want it updated in place", id, firstID)
						}
						if creates := api.Requests(http.MethodPost, "/v1/dashboards"); len(creates) != 1 {
							return fmt.Errorf("dashboards created %d times, want once", len(creates))
						}
						return sentDefault(api, firstID, true)
					},
				),
			},
			{
				ResourceName:      "costfluent_dashboard.d",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + dashboardOnlyID(t, api), nil
				},
				// An import has no configuration to keep the configured spelling from, so it takes the
				// API's lastMonth for the configured LastMonth.
				ImportStateVerifyIgnore: []string{"date_interval"},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/dashboards"); len(ids) != 0 {
				return fmt.Errorf("dashboards left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func TestDashboardDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardConfig(api, "Overview", "")},
			{
				PreConfig:          func() { api.Remove("/v1/dashboards", dashboardOnlyID(t, api)) },
				Config:             dashboardConfig(api, "Overview", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestDashboardReadFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardConfig(api, "Overview", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/dashboards/"+dashboardOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read this workspace"))
				},
				Config:      dashboardConfig(api, "Overview", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to read dashboard(.|\n)*The token cannot read this workspace`),
			},
		},
	})
}

func TestDashboardCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/dashboards", "dsh_", "costfluent_dashboard.d",
		dashboardConfig(api, "Overview", ""), "Failed to create dashboard")
}

// fakeDashboardSavedFilter answers PUT /v1/dashboards/{id}/saved-filter as the API does: the
// workspace travels in the body, null detaches the filter, and the dashboard comes back.
func fakeDashboardSavedFilter(api *fakeapi.Server) {
	api.Handle("PUT /v1/dashboards/{id}/saved-filter", func(r fakeapi.Request) fakeapi.Response {
		id := r.PathValue("id")
		if _, ok := api.Get("/v1/dashboards", id); !ok {
			return fakeapi.NotFound()
		}
		if r.Query.Has("workspaceId") {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "workspaceId belongs in the body")
		}
		api.Update("/v1/dashboards", id, func(obj map[string]any) { obj["savedFilterId"] = r.Body["savedFilterId"] })
		obj, _ := api.Get("/v1/dashboards", id)
		return fakeapi.JSON(obj)
	})
}

func dashboardWithFilterConfig(api *fakeapi.Server, savedFilter string) string {
	attr := ""
	if savedFilter != "" {
		attr = fmt.Sprintf("  saved_filter_id = %q\n", savedFilter)
	}
	return dashboardConfig(api, "Overview", attr)
}

func TestDashboardSavedFilterIsAttachedChangedAndDetached(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	fakeDashboardSavedFilter(api)

	sent := func(want any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			put := api.LastRequest(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api)+"/saved-filter")
			if value, ok := put.Body["savedFilterId"]; !ok || value != want {
				return fmt.Errorf("saved-filter body = %s, want savedFilterId %v", put.Raw, want)
			}
			if put.Body["workspaceId"] != testWorkspace {
				return fmt.Errorf("saved-filter body = %s, want the dashboard's workspace", put.Raw)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardWithFilterConfig(api, "flt_0000000000000000000001"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "saved_filter_id", "flt_0000000000000000000001"),
					sent("flt_0000000000000000000001"),
				),
			},
			{
				ResourceName:      "costfluent_dashboard.d",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + dashboardOnlyID(t, api), nil
				},
			},
			{
				Config: dashboardWithFilterConfig(api, "flt_0000000000000000000002"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "saved_filter_id", "flt_0000000000000000000002"),
					sent("flt_0000000000000000000002"),
				),
			},
			{
				Config: dashboardWithFilterConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_dashboard.d", "saved_filter_id"),
					sent(nil),
				),
			},
		},
	})
}

func TestDashboardWithoutASavedFilterNeverCallsTheSavedFilterEndpoint(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	fakeDashboardSavedFilter(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardWithFilterConfig(api, "")},
			{
				Config: dashboardConfig(api, "Renamed", ""),
				Check: func(*terraform.State) error {
					if calls := api.Requests(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api)+"/saved-filter"); len(calls) != 0 {
						return fmt.Errorf("saved-filter called %d times for a dashboard without one", len(calls))
					}
					return nil
				},
			},
		},
	})
}

// A dashboard whose saved filter cannot be attached is still recorded, without the filter, so the
// next apply attaches it instead of creating a second dashboard.
func TestDashboardSavedFilterAttachFailureKeepsTheDashboard(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	api.Handle("PUT /v1/dashboards/{id}/saved-filter", func(fakeapi.Request) fakeapi.Response {
		return fakeapi.Problem(http.StatusNotFound, "NotFound", "SavedFilter flt_0000000000000000000009 was not found")
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      dashboardWithFilterConfig(api, "flt_0000000000000000000009"),
				ExpectError: regexp.MustCompile(`Failed to attach the saved filter to the dashboard(.|\n)*was not found`),
			},
			{
				PreConfig: func() {
					if ids := api.IDs("/v1/dashboards"); len(ids) != 1 {
						t.Fatalf("dashboards = %v, want the one created", ids)
					}
				},
				// The dashboard is in state without the filter, so the plan only attaches it.
				Config:             dashboardWithFilterConfig(api, "flt_0000000000000000000009"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestDashboardSavedFilterUpdateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	fakeDashboardSavedFilter(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardWithFilterConfig(api, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api)+"/saved-filter",
						fakeapi.Problem(http.StatusNotFound, "NotFound", "SavedFilter flt_0000000000000000000009 was not found"))
				},
				Config:      dashboardWithFilterConfig(api, "flt_0000000000000000000009"),
				ExpectError: regexp.MustCompile(`Failed to set the dashboard's saved filter(.|\n)*was not found`),
			},
		},
	})
}

func TestDashboardCustomRangeSendsItsDatesAndAPresetDropsThem(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "August", `  date_interval = "custom"
  start_date    = "2026-08-01"
  end_date      = "2026-08-31"
  date_bin      = "week"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "start_date", "2026-08-01"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "end_date", "2026-08-31"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/dashboards")
						if create.Body["startDate"] != "2026-08-01" || create.Body["endDate"] != "2026-08-31" {
							return fmt.Errorf("create body = %s, want the range's dates", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: dashboardConfig(api, "August", `  date_interval = "lastMonth"
  date_bin      = "week"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_interval", "lastMonth"),
					resource.TestCheckNoResourceAttr("costfluent_dashboard.d", "start_date"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api))
						if update.Body["dateInterval"] != "lastMonth" || update.Body["startDate"] != nil {
							return fmt.Errorf("update body = %s, want the preset without dates", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

const (
	presetWithDates = `  date_interval = "lastMonth"
  start_date    = "2026-08-01"
  end_date      = "2026-08-31"
`
	customWithDates = `  date_interval = "custom"
  start_date    = "2026-08-01"
  end_date      = "2026-08-31"
`
)

// dashboardStoredDates checks the fake holds the range the API would, dates only for custom.
func dashboardStoredDates(t *testing.T, api *fakeapi.Server, start, end any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		obj, _ := api.Get("/v1/dashboards", dashboardOnlyID(t, api))
		if obj["startDate"] != start || obj["endDate"] != end {
			return fmt.Errorf("stored range %v to %v, want %v to %v", obj["startDate"], obj["endDate"], start, end)
		}
		return nil
	}
}

// The API drops the dates sent beside a preset; the configured ones stay in state, so the apply is
// consistent and neither a refresh nor the next plan sees a change.
func TestDashboardPresetKeepsTheConfiguredDatesTheAPIIgnores(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "Overview", presetWithDates),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_interval", "lastMonth"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "start_date", "2026-08-01"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "end_date", "2026-08-31"),
					dashboardStoredDates(t, api, nil, nil),
				),
			},
			{
				Config:   dashboardConfig(api, "Overview", presetWithDates),
				PlanOnly: true,
			},
		},
	})
}

// A custom range keeps its dates through the API; switching it to a preset with the dates still
// configured applies cleanly and plans nothing after.
func TestDashboardCustomToPresetKeepingItsDatesAppliesCleanly(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "August", customWithDates),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "start_date", "2026-08-01"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "end_date", "2026-08-31"),
					dashboardStoredDates(t, api, "2026-08-01", "2026-08-31"),
					func(*terraform.State) error { id = dashboardOnlyID(t, api); return nil },
				),
			},
			{
				Config: dashboardConfig(api, "August", presetWithDates),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.d", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "date_interval", "lastMonth"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "start_date", "2026-08-01"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "end_date", "2026-08-31"),
					dashboardStoredDates(t, api, nil, nil),
					func(*terraform.State) error {
						if got := dashboardOnlyID(t, api); got != id {
							return fmt.Errorf("dashboard %s replaced %s, want it updated in place", got, id)
						}
						update := api.LastRequest(http.MethodPut, "/v1/dashboards/"+id)
						if update.Body["dateInterval"] != "lastMonth" {
							return fmt.Errorf("update body = %s, want the preset", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config:   dashboardConfig(api, "August", presetWithDates),
				PlanOnly: true,
			},
		},
	})
}

// A custom range's dates are read from the API: an import takes them, and a date changed in the app
// plans the configured one back.
func TestDashboardCustomRangeIsReadBackOnImportAndRefresh(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardConfig(api, "August", customWithDates)},
			{
				ResourceName:      "costfluent_dashboard.d",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + dashboardOnlyID(t, api), nil
				},
			},
			{
				PreConfig: func() {
					api.Update("/v1/dashboards", dashboardOnlyID(t, api), func(obj map[string]any) { obj["startDate"] = "2026-08-15" })
				},
				Config:             dashboardConfig(api, "August", customWithDates),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: dashboardConfig(api, "August", customWithDates),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "start_date", "2026-08-01"),
					dashboardStoredDates(t, api, "2026-08-01", "2026-08-31"),
				),
			},
		},
	})
}

// sentDefault checks the last PUT /v1/dashboards/{id}/default carried the workspace and isDefault.
func sentDefault(api *fakeapi.Server, id string, want bool) error {
	put := api.LastRequest(http.MethodPut, "/v1/dashboards/"+id+"/default")
	if put.Body["workspaceId"] != testWorkspace || put.Body["isDefault"] != want {
		return fmt.Errorf("default body = %s, want the workspace and isDefault %v", put.Raw, want)
	}
	return nil
}

func dashboardIsDefault(t *testing.T, api *fakeapi.Server, id string) bool {
	t.Helper()
	obj, ok := api.Get("/v1/dashboards", id)
	if !ok {
		t.Fatalf("dashboard %s is gone", id)
	}
	return obj["isDefault"] == true
}

// Someone makes another dashboard the default in the app; the next apply takes it back in place,
// keeping the dashboard and everything on it.
func TestDashboardDefaultTakenByAnotherDashboardIsRestoredInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var id, other string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "Overview", "  is_default = true\n"),
				Check:  func(*terraform.State) error { id = dashboardOnlyID(t, api); return nil },
			},
			{
				PreConfig: func() {
					other = api.NextID("dsh_")
					takeDefault(api, other)
					api.PutIn("/v1/dashboards", testWorkspace, other, map[string]any{
						"title": "Made default in the app", "isDefault": true, "dateInterval": "thisMonth",
						"dateBin": "day", "widgets": []any{}, "createdAt": "2026-10-02T09:00:00Z",
					})
				},
				Config: dashboardConfig(api, "Overview", "  is_default = true\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.d", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "true"),
					func(*terraform.State) error {
						if _, ok := api.Get("/v1/dashboards", id); !ok {
							return fmt.Errorf("dashboard %s was destroyed", id)
						}
						if creates := api.Requests(http.MethodPost, "/v1/dashboards"); len(creates) != 1 {
							return fmt.Errorf("dashboards created %d times, want once", len(creates))
						}
						if puts := api.Requests(http.MethodPut, "/v1/dashboards/"+id+"/default"); len(puts) != 1 {
							return fmt.Errorf("default set %d times, want once", len(puts))
						}
						if !dashboardIsDefault(t, api, id) || dashboardIsDefault(t, api, other) {
							return fmt.Errorf("the default did not move back to %s", id)
						}
						return sentDefault(api, id, true)
					},
				),
			},
		},
	})
}

func TestDashboardIsDefaultFalseClearsTheDefaultInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "Overview", "  is_default = true\n"),
				Check:  func(*terraform.State) error { id = dashboardOnlyID(t, api); return nil },
			},
			{
				Config: dashboardConfig(api, "Overview", "  is_default = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.d", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "false"),
					func(*terraform.State) error {
						if got := dashboardOnlyID(t, api); got != id {
							return fmt.Errorf("dashboard %s replaced %s, want it updated in place", got, id)
						}
						if dashboardIsDefault(t, api, id) {
							return fmt.Errorf("dashboard %s is still the default", id)
						}
						if puts := api.Requests(http.MethodPut, "/v1/dashboards/"+id); len(puts) != 0 {
							return fmt.Errorf("an unchanged title and range were sent %d times", len(puts))
						}
						return sentDefault(api, id, false)
					},
				),
			},
		},
	})
}

func twoDashboardsConfig(api *fakeapi.Server, firstDefault, secondDefault bool) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_dashboard" "first" {
  workspace_id = %q
  title        = "First"
  is_default   = %t
}

resource "costfluent_dashboard" "second" {
  workspace_id = %q
  title        = "Second"
  is_default   = %t
}
`, testWorkspace, firstDefault, testWorkspace, secondDefault)
}

// Both dashboards change in one apply, in parallel: the one made default bumps the other's row
// version, and the other's write, which carries no If-Match, is not refused for it.
func TestDashboardDefaultMovedBetweenTwoDashboardsInOneApplyUpdatesBothInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var before []string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: twoDashboardsConfig(api, true, false),
				Check:  func(*terraform.State) error { before = api.IDs("/v1/dashboards"); return nil },
			},
			{
				Config: twoDashboardsConfig(api, false, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_dashboard.first", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("costfluent_dashboard.second", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.first", "is_default", "false"),
					resource.TestCheckResourceAttr("costfluent_dashboard.second", "is_default", "true"),
					func(*terraform.State) error {
						if after := api.IDs("/v1/dashboards"); fmt.Sprint(after) != fmt.Sprint(before) {
							return fmt.Errorf("dashboards %v became %v, want both kept", before, after)
						}
						return nil
					},
				),
			},
		},
	})
}

// Without is_default the dashboard follows the workspace: a default moved to it elsewhere plans
// nothing, and a later change never sends the default.
func TestDashboardWithoutIsDefaultFollowsTheServer(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardConfig(api, "Overview", "")},
			{
				PreConfig: func() {
					api.Update("/v1/dashboards", dashboardOnlyID(t, api), func(obj map[string]any) { obj["isDefault"] = true })
				},
				Config:   dashboardConfig(api, "Overview", ""),
				PlanOnly: true,
			},
			{
				Config: dashboardConfig(api, "Renamed", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "true"),
					func(*terraform.State) error {
						if puts := api.Requests(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api)+"/default"); len(puts) != 0 {
							return fmt.Errorf("default sent %d times for a dashboard that does not set it", len(puts))
						}
						return nil
					},
				),
			},
		},
	})
}

// A default that cannot be set after a rename surfaces the API's error, and the next apply converges.
func TestDashboardDefaultFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: dashboardConfig(api, "Overview", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/dashboards/"+dashboardOnlyID(t, api)+"/default",
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot update dashboards"))
				},
				Config:      dashboardConfig(api, "Renamed", "  is_default = true\n"),
				ExpectError: regexp.MustCompile(`Failed to set whether the dashboard is the default(.|\n)*The token cannot update dashboards`),
			},
			{
				Config: dashboardConfig(api, "Renamed", "  is_default = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "title", "Renamed"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "true"),
				),
			},
		},
	})
}

// An update the API refuses surfaces its error before the default is touched, and the next apply
// sends both.
func TestDashboardUpdateFailureSurfacesTheAPIErrorAndSkipsTheDefault(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: dashboardConfig(api, "Overview", ""),
				Check:  func(*terraform.State) error { id = dashboardOnlyID(t, api); return nil },
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/dashboards/"+id,
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot update dashboards"))
				},
				Config:      dashboardConfig(api, "Renamed", "  is_default = true\n"),
				ExpectError: regexp.MustCompile(`Failed to update dashboard(.|\n)*The token cannot update dashboards`),
			},
			{
				PreConfig: func() {
					if puts := api.Requests(http.MethodPut, "/v1/dashboards/"+id+"/default"); len(puts) != 0 {
						t.Errorf("default sent %d times after the update failed, want none", len(puts))
					}
				},
				Config: dashboardConfig(api, "Renamed", "  is_default = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "title", "Renamed"),
					resource.TestCheckResourceAttr("costfluent_dashboard.d", "is_default", "true"),
					func(*terraform.State) error { return sentDefault(api, id, true) },
				),
			},
		},
	})
}
