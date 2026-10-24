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

const (
	testDashboard = "dsh_0000000000000000000099"
	testReport    = "crp_0000000000000000000001"
	otherReport   = "crp_0000000000000000000002"
	widgetAddress = "costfluent_dashboard_widget.w"
)

func widgetsOf(dashboard string) string { return "/v1/dashboards/" + dashboard + "/widgets" }

// widgetFake serves the widgets of testDashboard, which it stores in testWorkspace, as the API
// does: the workspace is the query's, a create answers DashboardWidgetCreatedResponse, a read
// answers the type and the display camel-cased, a report widget defaults to chart, and
// each type refuses the other's fields.
func widgetFake(api *fakeapi.Server) {
	api.PutIn("/v1/dashboards", testWorkspace, testDashboard, map[string]any{"title": "Overview"})

	reach := func(r fakeapi.Request) (string, *fakeapi.Response) {
		dashboard := r.PathValue("dashboard")
		if _, ok := api.Get("/v1/dashboards", dashboard); !ok || r.Query.Get("workspaceId") != testWorkspace {
			resp := fakeapi.NotFound()
			return "", &resp
		}
		return widgetsOf(dashboard), nil
	}
	invalid := func(detail string) fakeapi.Response {
		return fakeapi.Problem(http.StatusBadRequest, "Validation", detail)
	}
	// A widget is versioned as the API keeps it: each answer carries its ETag, and a write naming
	// another version is refused.
	tagged := func(collection, id string, resp fakeapi.Response) fakeapi.Response {
		resp.Header = http.Header{"Etag": {api.ETag(collection, id)}}
		return resp
	}
	stale := func(r fakeapi.Request, collection, id string) *fakeapi.Response {
		if match := r.Header.Get("If-Match"); match != "" && match != "*" && match != api.ETag(collection, id) {
			refusal := fakeapi.Problem(http.StatusPreconditionFailed, "precondition_failed", "The resource changed since it was read")
			return &refusal
		}
		return nil
	}

	api.Handle("POST /v1/dashboards/{dashboard}/widgets", func(r fakeapi.Request) fakeapi.Response {
		collection, refused := reach(r)
		if refused != nil {
			return *refused
		}
		obj := map[string]any{"title": r.Body["title"], "position": r.Body["position"], "costReportId": nil, "display": nil, "content": nil}
		switch widgetType, _ := r.Body["type"].(string); {
		case widgetType == "" || strings.EqualFold(widgetType, "costReport"):
			if r.Body["costReportId"] == nil || r.Body["content"] != nil {
				return invalid("A report widget needs costReportId and takes no content")
			}
			obj["type"], obj["costReportId"], obj["display"] = "costReport", r.Body["costReportId"], "chart"
			if display, ok := r.Body["display"].(string); ok {
				obj["display"] = strings.ToLower(display)
			}
		case strings.EqualFold(widgetType, "text"):
			if r.Body["content"] == nil || r.Body["costReportId"] != nil || r.Body["display"] != nil {
				return invalid("A text widget needs content and takes no report or display")
			}
			obj["type"], obj["content"] = "text", r.Body["content"]
		default:
			return invalid("type is not a widget type")
		}
		id := api.NextID("wdg_")
		api.Put(collection, id, obj)
		return tagged(collection, id, fakeapi.Response{Status: http.StatusCreated, Body: map[string]any{"id": id, "title": obj["title"]}})
	})
	api.Handle("GET /v1/dashboards/{dashboard}/widgets/{widget}", func(r fakeapi.Request) fakeapi.Response {
		collection, refused := reach(r)
		if refused != nil {
			return *refused
		}
		obj, ok := api.Get(collection, r.PathValue("widget"))
		if !ok {
			return fakeapi.NotFound()
		}
		return tagged(collection, r.PathValue("widget"), fakeapi.JSON(obj))
	})
	api.Handle("PUT /v1/dashboards/{dashboard}/widgets/{widget}", func(r fakeapi.Request) fakeapi.Response {
		collection, refused := reach(r)
		if refused != nil {
			return *refused
		}
		id := r.PathValue("widget")
		obj, ok := api.Get(collection, id)
		if !ok {
			return fakeapi.NotFound()
		}
		if refusal := stale(r, collection, id); refusal != nil {
			return *refusal
		}
		text := obj["type"] == "text"
		if (text && r.Body["display"] != nil) || (!text && r.Body["content"] != nil) {
			return invalid("display is a report widget's and content a text widget's")
		}
		api.Update(collection, id, func(obj map[string]any) {
			for _, k := range []string{"title", "content", "position"} {
				if v, ok := r.Body[k]; ok && v != nil {
					obj[k] = v
				}
			}
			if display, ok := r.Body["display"].(string); ok {
				obj["display"] = strings.ToLower(display)
			}
		})
		obj, _ = api.Get(collection, id)
		return tagged(collection, id, fakeapi.JSON(obj))
	})
	api.Handle("DELETE /v1/dashboards/{dashboard}/widgets/{widget}", func(r fakeapi.Request) fakeapi.Response {
		collection, refused := reach(r)
		if refused != nil {
			return *refused
		}
		if _, ok := api.Get(collection, r.PathValue("widget")); !ok {
			return fakeapi.NotFound()
		}
		if refusal := stale(r, collection, r.PathValue("widget")); refusal != nil {
			return *refusal
		}
		api.Remove(collection, r.PathValue("widget"))
		return fakeapi.NoContent()
	})
}

func widgetConfig(api *fakeapi.Server, body string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_dashboard_widget" "w" {
  workspace_id = %q
  dashboard_id = %q
%s}
`, testWorkspace, testDashboard, body)
}

func reportWidget(report, title, extra string) string {
	return fmt.Sprintf(`  cost_report_id = %q
  title          = %q
  position       = { x = 0, y = 0, w = 6, h = 4 }
%s`, report, title, extra)
}

func widgetOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs(widgetsOf(testDashboard))
	if len(ids) != 1 {
		t.Fatalf("widgets = %v, want one", ids)
	}
	return ids[0]
}

func noWidgetsLeft(api *fakeapi.Server) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if ids := api.IDs(widgetsOf(testDashboard)); len(ids) != 0 {
			return fmt.Errorf("widgets left after destroy: %v", ids)
		}
		return nil
	}
}

func TestDashboardWidgetReportLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             noWidgetsLeft(api),
		Steps: []resource.TestStep{
			{
				Config: widgetConfig(api, reportWidget(testReport, "Spend", "")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(widgetAddress, "id", regexp.MustCompile(`^wdg_.{22}$`)),
					resource.TestCheckResourceAttr(widgetAddress, "type", "costReport"),
					resource.TestCheckResourceAttr(widgetAddress, "cost_report_id", testReport),
					resource.TestCheckResourceAttr(widgetAddress, "display", "chart"),
					resource.TestCheckNoResourceAttr(widgetAddress, "content"),
					resource.TestCheckResourceAttr(widgetAddress, "position.w", "6"),
					func(*terraform.State) error {
						firstID = widgetOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, widgetsOf(testDashboard))
						if create.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("create query = %v", create.Query)
						}
						if create.Body["type"] != "costReport" || create.Body["costReportId"] != testReport ||
							create.Body["title"] != "Spend" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, unset := range []string{"display", "content", "workspaceId"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent %s: %s", unset, create.Raw)
							}
						}
						if pos, _ := create.Body["position"].(map[string]any); pos["w"] != 6.0 || pos["h"] != 4.0 {
							return fmt.Errorf("create position = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				// The API answers table; the configured spelling stays, so the next plan is empty.
				Config: widgetConfig(api, `  cost_report_id = "`+testReport+`"
  title          = "Spend by service"
  display        = "Table"
  position       = { x = 6, y = 0, w = 6, h = 4 }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(widgetAddress, "display", "Table"),
					resource.TestCheckResourceAttr(widgetAddress, "position.x", "6"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, widgetsOf(testDashboard)+"/"+firstID)
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						if update.Body["display"] != "table" || update.Body["title"] != "Spend by service" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						if pos, _ := update.Body["position"].(map[string]any); pos["x"] != 6.0 {
							return fmt.Errorf("update position = %s", update.Raw)
						}
						if _, sent := update.Body["content"]; sent {
							return fmt.Errorf("update sent content for a report widget: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      widgetAddress,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + testDashboard + ":" + widgetOnlyID(t, api), nil
				},
				// An import has no configuration to keep the configured spelling from.
				ImportStateVerifyIgnore: []string{"display"},
			},
			{
				// A different report is a new widget: the API cannot change a widget's report.
				Config: widgetConfig(api, reportWidget(otherReport, "Spend by service", "  display = \"Table\"\n")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if id := widgetOnlyID(t, api); id == firstID {
						return fmt.Errorf("widget %s was updated in place, want replaced", id)
					}
					if create := api.LastRequest(http.MethodPost, widgetsOf(testDashboard)); create.Body["costReportId"] != otherReport ||
						create.Body["display"] != "table" {
						return fmt.Errorf("create body = %s", create.Raw)
					}
					return nil
				},
			},
		},
	})
}

func TestDashboardWidgetTextLifecycle(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	textWidget := func(content string) string {
		return fmt.Sprintf("  type     = \"Text\"\n  title    = \"Notes\"\n  content  = %q\n  position = { x = 0, y = 4, w = 12, h = 2 }\n", content)
	}
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             noWidgetsLeft(api),
		Steps: []resource.TestStep{
			{
				Config: widgetConfig(api, textWidget("Spend is *up* this month")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(widgetAddress, "type", "Text"),
					resource.TestCheckResourceAttr(widgetAddress, "content", "Spend is *up* this month"),
					resource.TestCheckNoResourceAttr(widgetAddress, "display"),
					resource.TestCheckNoResourceAttr(widgetAddress, "cost_report_id"),
					func(*terraform.State) error {
						firstID = widgetOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, widgetsOf(testDashboard))
						if create.Body["type"] != "text" || create.Body["content"] != "Spend is *up* this month" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, unset := range []string{"display", "costReportId"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent %s for a text widget: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				Config: widgetConfig(api, textWidget("Spend is flat")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, widgetsOf(testDashboard)+"/"+firstID)
					if update.Body["content"] != "Spend is flat" {
						return fmt.Errorf("update body = %s", update.Raw)
					}
					for _, unset := range []string{"display", "title", "position"} {
						if _, sent := update.Body[unset]; sent {
							return fmt.Errorf("update sent unchanged %s: %s", unset, update.Raw)
						}
					}
					return nil
				},
			},
			{
				// A widget's type cannot change, so a text widget becoming a report is a new widget.
				Config: widgetConfig(api, reportWidget(testReport, "Notes", "")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(widgetAddress, "type", "costReport"),
					resource.TestCheckNoResourceAttr(widgetAddress, "content"),
				),
			},
		},
	})
}

func TestDashboardWidgetTypeInAnotherCaseIsNotAReplacement(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: widgetConfig(api, reportWidget(testReport, "Spend", "  type = \"costReport\"\n"))},
			{
				Config: widgetConfig(api, reportWidget(testReport, "Spend", "  type = \"CostReport\"\n")),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionUpdate)},
				},
			},
		},
	})
}

func TestDashboardWidgetConfigHoldsEachTypeToItsFields(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	position := "  title = \"W\"\n  position = { x = 0, y = 0, w = 1, h = 1 }\n"

	for name, tc := range map[string]struct{ body, want string }{
		"report without a report": {position, `A CostReport widget needs the cost report it renders`},
		"report with content":     {position + "  cost_report_id = \"" + testReport + "\"\n  content = \"x\"\n", `Unexpected content`},
		"text without content":    {position + "  type = \"Text\"\n", `A Text widget needs content`},
		"text with a report":      {position + "  type = \"Text\"\n  content = \"x\"\n  cost_report_id = \"" + testReport + "\"\n", `Unexpected cost_report_id`},
		"text with a display":     {position + "  type = \"Text\"\n  content = \"x\"\n  display = \"kpi\"\n", `Unexpected display`},
		"unknown type":            {position + "  type = \"Usage\"\n  content = \"x\"\n", `value must be one of`},
		"unknown display":         {position + "  cost_report_id = \"" + testReport + "\"\n  display = \"pie\"\n", `value must be one of`},
		"zero width":              {"  title = \"W\"\n  cost_report_id = \"" + testReport + "\"\n  position = { x = 0, y = 0, w = 0, h = 1 }\n", `must be at least 1`},
	} {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      widgetConfig(api, tc.body),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(tc.want),
				}},
			})
		})
	}
	if calls := api.Requests("", widgetsOf(testDashboard)); len(calls) != 0 {
		t.Fatalf("an invalid configuration reached the API: %d requests", len(calls))
	}
}

func TestDashboardWidgetImportInTheProviderWorkspaceAndRefusesAMalformedID(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	config := api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
resource "costfluent_dashboard_widget" "w" {
  dashboard_id   = "` + testDashboard + `"
` + reportWidget(testReport, "Spend", "") + `}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				ResourceName:      widgetAddress,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testDashboard + ":" + widgetOnlyID(t, api), nil
				},
			},
			{
				ResourceName:  widgetAddress,
				ImportState:   true,
				ImportStateId: "wdg_0000000000000000000001",
				ExpectError:   regexp.MustCompile(`Expected "<workspace ID>:<dashboard ID>:<widget ID>"`),
			},
		},
	})
}

func TestDashboardWidgetDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	config := widgetConfig(api, reportWidget(testReport, "Spend", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig:          func() { api.Remove(widgetsOf(testDashboard), widgetOnlyID(t, api)) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestDashboardWidgetReadFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	config := widgetConfig(api, reportWidget(testReport, "Spend", ""))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, widgetsOf(testDashboard)+"/"+widgetOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read this workspace"))
				},
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to read dashboard widget(.|\n)*The token cannot read this workspace`),
			},
		},
	})
}

// A widget whose read back fails is kept as tainted with its dashboard, so the next apply deletes
// it through that dashboard and creates its replacement rather than leaving a stray widget.
func TestDashboardWidgetCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	config := widgetConfig(api, reportWidget(testReport, "Spend", ""))
	first := "wdg_" + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodGet, widgetsOf(testDashboard)+"/"+first,
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read it"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             noWidgetsLeft(api),
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Failed to create dashboard widget(.|\n)*The token cannot read it`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(widgetAddress, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if ids := api.IDs(widgetsOf(testDashboard)); len(ids) != 1 || ids[0] == first {
						return fmt.Errorf("widgets = %v, want only the replacement of %s", ids, first)
					}
					return nil
				},
			},
		},
	})
}
