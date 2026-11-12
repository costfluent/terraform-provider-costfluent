package resources_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	costReportQuery  = "costs.provider = 'aws'"
	costReportStored = `[{"field":"Provider","operator":"In","values":["aws"]}]`
	costReportFolder = "fld_0000000000000000000007"
)

// costReportFake serves /v1/cost-reports with the CostReportCreatedResponse and CostReportResponse
// shapes. Like the API it stores a CQL filter as its JSON document, answers a grouping dimension in
// camelCase, defaults the chart, range, grouping and settings a create leaves out (amortize
// standing for a workspace default that differs from false), leaves out of an update what was
// omitted, settings flags included, removes the filter only on clearFilter, the folder only on
// clearFolder and a date only on its clear flag, refusing that flag beside its date or on a custom
// interval, and draws by date a report whose chart can no longer draw the group axis an update left
// out. It keeps the unit cost calculation and scale beside a metric the settings name and returns
// them to per unit, per 1, without one, and stores a label filter with every key and value trimmed
// and each value once and a grouping slot's empty key as none. A create that names no comparison
// mode compares with the previous period, and an update keeps the mode.
func costReportFake(api *fakeapi.Server) {
	api.Collection("/v1/cost-reports", fakeapi.Collection{
		IDPrefix:  "crp_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"title":        body["title"],
				"chartType":    "stackedBar",
				"chartAxis":    "date",
				"dateBin":      "day",
				"dateInterval": "last30Days",
				"groupBy":      []any{map[string]any{"dimension": "serviceName"}},
				"settings": map[string]any{"amortize": true, "includeCredits": false, "includeRefunds": false,
					"includeTax": false, "showForecast": false, "compareToLastPeriod": false, "measure": "cost",
					"unitCostCalculation": "perUnit", "unitCostScale": 1, "comparisonMode": "previousPeriod"},
				"createdAt": "2026-10-05T09:00:00Z",
			}
			costReportApply(obj, body)
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "title": obj["title"]})
		},
		Refuse: costReportRefusal,
		Apply: func(obj, body map[string]any) {
			costReportApply(obj, body)
			obj["updatedAt"] = "2026-10-06T09:00:00Z"
		},
	})
	api.Handle("GET /v1/cost-filters/translate", func(r fakeapi.Request) fakeapi.Response {
		if r.Query.Get("filter") != costReportQuery {
			return fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "The filter does not parse")
		}
		return fakeapi.JSON(map[string]any{"filter": costReportStored, "query": costReportQuery})
	})
}

func costReportApply(obj, body map[string]any) {
	if sent, ok := body["settings"].(map[string]any); ok {
		// An omitted flag keeps the default on create and the report's own on update; the unit
		// cost metric, its label filter and the comparison window are taken as sent, the filter
		// read back empty when none.
		settings := map[string]any{"unitCostLabelFilter": []any{}}
		if prior, ok := obj["settings"].(map[string]any); ok {
			for k, v := range prior {
				switch k {
				case "unitCostMetricId", "unitCostLabelFilter", "comparisonStartDate", "comparisonEndDate":
				default:
					settings[k] = v
				}
			}
		}
		if sent["unitCostMetricId"] == nil {
			settings["unitCostCalculation"], settings["unitCostScale"] = "perUnit", 1
		}
		for k, v := range sent {
			settings[k] = v
		}
		settings["unitCostLabelFilter"] = normalizedLabelFilter(settings["unitCostLabelFilter"])
		obj["settings"] = settings
	}
	for _, k := range []string{"title", "folderId", "chartType", "chartAxis", "dateBin", "dateInterval", "startDate", "endDate",
		"groupBy"} {
		if v, ok := body[k]; ok && v != nil {
			obj[k] = v
		}
	}
	if chart := obj["chartType"]; obj["chartAxis"] == "group" && chart != "bar" && chart != "stackedBar" {
		obj["chartAxis"] = "date"
	}
	if groups, ok := obj["groupBy"].([]any); ok {
		stored := make([]any, 0, len(groups))
		for _, g := range groups {
			slot := maps.Clone(g.(map[string]any))
			slot["dimension"] = camelCase(slot["dimension"])
			if slot["key"] == "" {
				delete(slot, "key")
			}
			stored = append(stored, slot)
		}
		obj["groupBy"] = stored
	}
	if f, ok := body["filter"]; ok && f != nil {
		if f == costReportQuery {
			f = costReportStored
		}
		obj["filter"] = f
	}
	if body["clearFilter"] == true {
		delete(obj, "filter")
	}
	if body["clearFolder"] == true {
		delete(obj, "folderId")
	}
	if body["clearStartDate"] == true {
		delete(obj, "startDate")
	}
	if body["clearEndDate"] == true {
		delete(obj, "endDate")
	}
}

// costReportRefusal is the API's 400 for a date cleared beside its own value, or cleared where the
// interval the update leaves is custom, which needs both dates.
func costReportRefusal(obj, body map[string]any) *fakeapi.Response {
	interval := obj["dateInterval"]
	if sent, ok := body["dateInterval"]; ok && sent != nil {
		interval = sent
	}
	for _, date := range []string{"startDate", "endDate"} {
		flag := "clear" + strings.ToUpper(date[:1]) + date[1:]
		if body[flag] != true {
			continue
		}
		refusal := fakeapi.Problem(http.StatusBadRequest, "Validation", "Name a date or clear it, not both.")
		if _, sent := body[date]; sent {
			return &refusal
		}
		if strings.EqualFold(fmt.Sprint(interval), "custom") {
			refusal = fakeapi.Problem(http.StatusBadRequest, "Validation", "A custom interval needs both a start and an end date.")
			return &refusal
		}
	}
	return nil
}

// normalizedLabelFilter is a unit cost label filter as the API stores it: every key and value
// trimmed, and each value of a key once, in the order first sent.
func normalizedLabelFilter(sent any) any {
	conditions, ok := sent.([]any)
	if !ok {
		return sent
	}
	stored := make([]any, 0, len(conditions))
	for _, c := range conditions {
		condition, _ := c.(map[string]any)
		values, _ := condition["values"].([]any)
		distinct := []any{}
		for _, v := range values {
			trimmed := strings.TrimSpace(fmt.Sprint(v))
			if !slices.Contains(distinct, any(trimmed)) {
				distinct = append(distinct, trimmed)
			}
		}
		stored = append(stored, map[string]any{"key": strings.TrimSpace(fmt.Sprint(condition["key"])), "values": distinct})
	}
	return stored
}

func costReportConfig(api *fakeapi.Server, body string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_report" "r" {
  workspace_id = %q
%s}
`, testWorkspace, body)
}

const costReportFull = `
  title      = "AWS by service"
  folder_id  = "` + costReportFolder + `"
  chart_type = "line"
  filter     = "` + costReportQuery + `"
  group_by   = [{ dimension = "serviceName" }, { dimension = "Tag", key = "team" }]
  settings   = {
    amortize               = false
    include_credits        = false
    include_refunds        = false
    include_tax            = true
    show_forecast          = false
    compare_to_last_period = true
  }
`

func costReportOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/cost-reports")
	if len(ids) != 1 {
		t.Fatalf("cost reports = %v, want one", ids)
	}
	return ids[0]
}

func TestCostReportLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, costReportFull),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "title", "AWS by service"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "folder_id", costReportFolder),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_type", "line"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "date_bin", "day"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "date_interval", "last30Days"),
					// The API stores the JSON and canonical dimension; the configured spellings mean the same.
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "filter", costReportQuery),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.0.dimension", "serviceName"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "group_by.0.key"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.1.key", "team"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.include_tax", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.compare_to_last_period", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.amortize", "false"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "created_at", "2026-10-05T09:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "updated_at"),
					resource.TestMatchResourceAttr("costfluent_cost_report.r", "id", regexp.MustCompile(`^crp_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						var body struct {
							WorkspaceID string              `json:"workspaceId"`
							Title       string              `json:"title"`
							FolderID    string              `json:"folderId"`
							ChartType   string              `json:"chartType"`
							Filter      string              `json:"filter"`
							GroupBy     []map[string]string `json:"groupBy"`
							Settings    map[string]bool     `json:"settings"`
						}
						if err := json.Unmarshal(create.Raw, &body); err != nil {
							return err
						}
						if body.WorkspaceID != testWorkspace || body.Title != "AWS by service" ||
							body.FolderID != costReportFolder || body.ChartType != "line" || body.Filter != costReportQuery ||
							fmt.Sprint(body.GroupBy) != "[map[dimension:serviceName] map[dimension:Tag key:team]]" ||
							!body.Settings["includeTax"] || !body.Settings["compareToLastPeriod"] || body.Settings["amortize"] {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, unset := range []string{"dateBin", "dateInterval", "startDate", "endDate"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent unset %s: %s", unset, create.Raw)
							}
						}
						read := api.LastRequest(http.MethodGet, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						return nil
					},
				),
			},
			{
				// An empty group_by is a single total, so it is sent as an empty list, not omitted.
				Config: costReportConfig(api, `
  title         = "AWS total"
  folder_id     = "`+costReportFolder+`"
  filter        = "`+costReportQuery+`"
  date_interval = "lastMonth"
  group_by      = []
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "title", "AWS total"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "date_interval", "lastMonth"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.#", "0"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "updated_at", "2026-10-06T09:00:00Z"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						groups, sent := update.Body["groupBy"].([]any)
						if !sent || len(groups) != 0 {
							return fmt.Errorf("update groupBy = %s, want []", update.Raw)
						}
						if update.Body["title"] != "AWS total" || update.Body["dateInterval"] != "lastMonth" ||
							update.Body["filter"] != costReportQuery {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_cost_report.r",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + costReportOnlyID(t, api), nil
				},
				// An import has no configured CQL to keep, so it reads the filter as the stored JSON.
				ImportStateVerifyIgnore: []string{"filter"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["filter"]; got != costReportStored {
						return fmt.Errorf("imported filter = %q, want the stored JSON", got)
					}
					return nil
				},
			},
			{
				ResourceName:  "costfluent_cost_report.r",
				ImportState:   true,
				ImportStateId: "crp_0000000000000000000001",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/cost-reports"); len(ids) != 0 {
				return fmt.Errorf("cost reports left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A configured group axis is sent on create and on update, and read back without a diff.
func TestCostReportChartAxisIsSentAndKept(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	grouped := costReportConfig(api, "  title      = \"Side by side\"\n  chart_type = \"bar\"\n  chart_axis = \"group\"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: grouped,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_axis", "group"),
					func(*terraform.State) error {
						if create := api.LastRequest(http.MethodPost, "/v1/cost-reports"); create.Body["chartAxis"] != "group" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{Config: grouped, PlanOnly: true},
			{
				Config: costReportConfig(api, "  title      = \"Side by side\"\n  chart_type = \"stackedBar\"\n  chart_axis = \"group\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_axis", "group"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if update.Body["chartAxis"] != "group" || update.Body["chartType"] != "stackedBar" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// Left out, the axis is the API's: none is sent, a report the app put on the group axis plans no
// drift, and a chart type that cannot draw it reads back by date rather than failing the update.
func TestCostReportChartAxisLeftOutTakesTheAPIDefault(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	bars := costReportConfig(api, "  title      = \"Bars\"\n  chart_type = \"bar\"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: bars,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_axis", "date"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						if _, sent := create.Body["chartAxis"]; sent {
							return fmt.Errorf("create sent an unset chartAxis: %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) { obj["chartAxis"] = "group" })
				},
				Config:   bars,
				PlanOnly: true,
			},
			{
				Config: costReportConfig(api, "  title      = \"Bars\"\n  chart_type = \"line\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_type", "line"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_axis", "date"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if _, sent := update.Body["chartAxis"]; sent {
							return fmt.Errorf("update sent an unset chartAxis: %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// A report that leaves the chart, range and settings to the API takes the API's defaults, sends
// none of them, and plans nothing on the next run.
func TestCostReportTakesTheAPIDefaults(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	minimal := costReportConfig(api, "  title    = \"Everything\"\n  group_by = [{ dimension = \"ServiceName\" }]\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: minimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_type", "stackedBar"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "date_bin", "day"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.amortize", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.include_tax", "false"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "filter"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "folder_id"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						for _, unset := range []string{"settings", "filter", "chartType", "dateBin", "folderId"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent unset %s: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{Config: minimal, PlanOnly: true},
		},
	})
}

// Terraform does not carry an omitted group_by over from state while it holds a required dimension,
// so without a default the plan marks the computed attributes unknown and updates on every run.
// Omitted after a configured grouping, it returns to ServiceName rather than diffing forever.
func TestCostReportWithTheDefaultGroupingPlansNothing(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	minimal := costReportConfig(api, "  title = \"Everything\"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: minimal, Check: resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.0.dimension", "ServiceName")},
			{Config: costReportConfig(api, "  title = \"Everything\"\n  group_by = [{ dimension = \"Tag\", key = \"team\" }]\n")},
			{
				Config: minimal,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.#", "1"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.0.dimension", "ServiceName"),
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "group_by.0.key"),
				),
			},
		},
	})
}

// An import has no configured grouping to take the spelling from, and the API answers serviceName,
// so the import reads the dimension as the group_by default spells it. A configuration that sets
// some settings and no group_by then imports exactly as it applied and plans nothing.
func TestCostReportImportOfAPartlyConfiguredReportPlansNothing(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	const r = "costfluent_cost_report.r"
	partial := costReportConfig(api, "  title    = \"Taxed\"\n  settings = { include_tax = true }\n")
	importID := func(*terraform.State) (string, error) { return testWorkspace + ":" + costReportOnlyID(t, api), nil }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: partial,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "group_by.0.dimension", "ServiceName"),
					resource.TestCheckResourceAttr(r, "settings.include_tax", "true"),
					func(*terraform.State) error {
						stored, _ := api.Get("/v1/cost-reports", costReportOnlyID(t, api))
						if got := fmt.Sprint(stored["groupBy"]); got != "[map[dimension:serviceName]]" {
							return fmt.Errorf("stored groupBy = %s, want the API's camelCase", got)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      r,
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				Config:            partial,
				ResourceName:      r,
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
		},
	})
}

func TestCostReportDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costReportConfig(api, costReportFull)},
			{
				PreConfig:          func() { api.Remove("/v1/cost-reports", costReportOnlyID(t, api)) },
				Config:             costReportConfig(api, costReportFull),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestCostReportReadFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costReportConfig(api, costReportFull)},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/cost-reports/"+costReportOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read this workspace"))
				},
				Config:      costReportConfig(api, costReportFull),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to read cost report(.|\n)*The token cannot read this workspace`),
			},
		},
	})
}

func TestCostReportCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	api.FailNext(http.MethodPost, "/v1/cost-reports",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "The folder does not exist in this workspace"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      costReportConfig(api, costReportFull),
				ExpectError: regexp.MustCompile(`Failed to create cost report(.|\n)*The folder does not exist in this workspace`),
			},
		},
	})
}

// ProviderName groups by the FOCUS provider name, which tells custom providers apart where
// Provider groups them all as custom.
func TestCostReportGroupsByProviderName(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	config := costReportConfig(api, "  title    = \"By provider\"\n  group_by = [{ dimension = \"ProviderName\" }]\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "group_by.0.dimension", "ProviderName"),
					func(*terraform.State) error {
						body := api.LastRequest(http.MethodPost, "/v1/cost-reports").Body
						if fmt.Sprint(body["groupBy"]) != "[map[dimension:ProviderName]]" {
							return fmt.Errorf("create groupBy = %v", body["groupBy"])
						}
						return nil
					},
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestCostReportRefusesAnUnknownGroupingDimensionAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "By colour"
  group_by = [{ dimension = "Colour" }]
`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

// The API refuses each of these groupings with a 400 on groupBy; the provider refuses them at plan,
// naming the slot, before a request is sent. The API counts a key in UTF-16 code units, so 65 emoji,
// each two of them, are over its limit.
func TestCostReportRefusesAGroupingTheAPIRefusesAtPlan(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ groupBy, want string }{
		"Tag without a key":                    {`[{ dimension = "Tag" }]`, `group_by\[0\]: grouping by Tag needs the key it groups on`},
		"Tag with an empty key":                {`[{ dimension = "tag", key = "" }]`, `group_by\[0\]: grouping by Tag needs the key it groups on`},
		"Tag with a blank key":                 {`[{ dimension = "ServiceName" }, { dimension = "Tag", key = "  " }]`, `group_by\[1\]: grouping by Tag needs`},
		"VirtualTag without a key":             {`[{ dimension = "VirtualTag" }]`, `group_by\[0\]: grouping by VirtualTag needs the key`},
		"Tag key over the limit":               {`[{ dimension = "Tag", key = "` + strings.Repeat("k", 129) + `" }]`, `group_by\[0\]: a grouping key is at most 128\s+characters`},
		"Tag key of 130 code units":            {`[{ dimension = "Tag", key = "` + strings.Repeat("😀", 65) + `" }]`, `group_by\[0\]: a grouping key is at most 128\s+characters`},
		"VirtualTag naming its key":            {`[{ dimension = "VirtualTag", key = "team" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"VirtualTag with an upper-case prefix": {`[{ dimension = "VirtualTag", key = "VTAG_x" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"VirtualTag with a short name":         {`[{ dimension = "VirtualTag", key = "vtag_team" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"VirtualTag id of 26 characters":       {`[{ dimension = "VirtualTag", key = "vtag_` + strings.Repeat("A", 21) + `" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"VirtualTag id of 28 characters":       {`[{ dimension = "VirtualTag", key = "vtag_` + strings.Repeat("A", 23) + `" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"VirtualTag id with a dot":             {`[{ dimension = "VirtualTag", key = "vtag_` + strings.Repeat("A", 10) + "." + strings.Repeat("A", 11) + `" }]`, `group_by\[0\]: grouping by VirtualTag names the virtual\s+tag's id`},
		"Tag with a tab key":                   {`[{ dimension = "Tag", key = "\t" }]`, `group_by\[0\]: grouping by Tag needs the key it groups on`},
		"Tag with a no-break space key":        {`[{ dimension = "Tag", key = "\u00a0" }]`, `group_by\[0\]: grouping by Tag needs the key it groups on`},
		"Tag with an ideographic space key":    {`[{ dimension = "Tag", key = "\u3000" }]`, `group_by\[0\]: grouping by Tag needs the key it groups on`},
		"a padded Tag key over the limit":      {`[{ dimension = "Tag", key = " ` + strings.Repeat("k", 128) + `" }]`, `group_by\[0\]: a grouping key is at most 128\s+characters`},
		"a blank key on another dimension":     {`[{ dimension = "ServiceName", key = " " }]`, `group_by\[0\]: only Tag and VirtualTag take a key;\s+ServiceName does not`},
		"key on another dimension":             {`[{ dimension = "ServiceName", key = "team" }]`, `group_by\[0\]: only Tag and VirtualTag take a key;\s+ServiceName does not`},
		"key on Segment":                       {`[{ dimension = "Segment", key = "team" }]`, `group_by\[0\]: only Tag and VirtualTag take a key;\s+Segment`},
		"key on an unknown dimension":          {`[{ dimension = "Colour", key = "team" }]`, `group_by\[0\]: only Tag and VirtualTag take a key;\s+Colour does not`},
		"the same slot twice":                  {`[{ dimension = "Tag", key = "team" }, { dimension = "tag", key = "team" }]`, `group_by\[1\] repeats group_by\[0\]`},
		"a dimension twice":                    {`[{ dimension = "ServiceName" }, { dimension = "serviceName" }]`, `group_by\[1\] repeats group_by\[0\]`},
		"a dimension three times":              {`[{ dimension = "Provider" }, { dimension = "provider" }, { dimension = "PROVIDER" }]`, `group_by\[2\] repeats group_by\[0\]`},
		"an empty key and none":                {`[{ dimension = "Region", key = "" }, { dimension = "Region" }]`, `group_by\[1\] repeats group_by\[0\]`},
		"Segment twice":                        {`[{ dimension = "Segment" }, { dimension = "Region" }, { dimension = "segment" }]`, `group_by\[2\] repeats group_by\[0\]`},
		"more than three slots": {`[{ dimension = "Provider" }, { dimension = "Region" }, { dimension = "Segment" },
    { dimension = "ServiceName" }]`, `group_by list must contain at most 3 elements`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config:      costReportConfig(api, "  title    = \"Refused\"\n  group_by = "+c.groupBy+"\n"),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(c.want),
				}},
			})
			if sent := api.Requests(http.MethodPost, "/v1/cost-reports"); len(sent) != 0 {
				t.Fatalf("a refused grouping sent %d creates, want none", len(sent))
			}
		})
	}
}

// costReportUnknownAtPlan is a terraform_data whose output is input, unknown until it is created.
func costReportUnknownAtPlan(name, input string) string {
	return fmt.Sprintf("\nresource \"terraform_data\" %q {\n  input = %s\n}\n", name, input)
}

// What the API takes plans and applies clean, and a plan of the same configuration then changes
// nothing: two Tag slots differing in key or only in padding, which the API keeps as written, three
// dimensions without a key, a key at the limit in ASCII and in characters of two UTF-8 bytes but one
// UTF-16 code unit, an empty key on a dimension that takes none, which the API reads as none, a
// virtual tag id of the prefix and 22 base64url characters, and keys, dimensions and the grouping
// still unknown at plan.
func TestCostReportTakesTheGroupingsTheAPITakes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"two Tag slots with different keys":  `[{ dimension = "Tag", key = "team" }, { dimension = "Tag", key = "Team" }, { dimension = "Segment" }]`,
		"two Tag slots differing in padding": `[{ dimension = "Tag", key = "team" }, { dimension = "Tag", key = " team" }]`,
		"three dimensions without a key":     `[{ dimension = "ServiceName" }, { dimension = "Region" }, { dimension = "Segment" }]`,
		"a key at the limit":                 `[{ dimension = "Tag", key = "` + strings.Repeat("k", 128) + `" }]`,
		"a key of 128 code units":            `[{ dimension = "Tag", key = "` + strings.Repeat("é", 128) + `" }]`,
		"an empty key on ServiceName":        `[{ dimension = "ServiceName", key = "" }, { dimension = "Tag", key = "team" }]`,
		"a virtual tag's id unknown at plan": `[{ dimension = "VirtualTag", key = costfluent_virtual_tag.t.id }]`,
		"a well-formed virtual tag id":       `[{ dimension = "VirtualTag", key = "vtag_AbC-_09xyzAbC-_09xyzaQ" }]`,
		"a dimension unknown at plan":        `[{ dimension = terraform_data.d.output, key = "team" }]`,
		"the grouping unknown at plan":       `terraform_data.g.output`,
	}
	for name, groupBy := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			// The virtual tag only has to exist for its id to be unknown at plan. virtualTagFake
			// would serve the filter translation costReportFake already does, so its one rule is
			// stored as costReportFake translates the rule's filter.
			api.Collection("/v1/virtual-tags", fakeapi.Collection{
				IDPrefix: "vtag_",
				Scope:    fakeapi.WorkspaceScoped,
				Create: func(body map[string]any) map[string]any {
					return map[string]any{"key": body["key"], "collapsedTagKeys": body["collapsedTagKeys"],
						"rules":     []any{map[string]any{"value": "compute", "filter": costReportStored}},
						"createdAt": "2026-10-05T07:00:00Z"}
				},
			})
			config := costReportConfig(api, "  title    = \"Taken\"\n  group_by = "+groupBy+"\n") + `
resource "costfluent_virtual_tag" "t" {
  workspace_id       = "` + testWorkspace + `"
  key                = "team"
  collapsed_tag_keys = ["team"]

  rule {
    value  = "compute"
    filter = "` + costReportQuery + `"
  }
}
` + costReportUnknownAtPlan("d", `"Tag"`) + costReportUnknownAtPlan("g", `[{ dimension = "Tag", key = "team" }]`)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: config},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}

// A slot configured with an empty key that the report now groups by a keyed dimension, changed
// outside Terraform, reads back the API's key, not the configured empty one, and plans the change
// back.
func TestCostReportSlotChangedToAKeyedOneReadsBackItsKey(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	r := "costfluent_cost_report.r"
	config := costReportConfig(api, "  title    = \"Drift\"\n  group_by = [{ dimension = \"ServiceName\", key = \"\" }]\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr(r, "group_by.0.key", ""),
			},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) {
						obj["groupBy"] = []any{map[string]any{"dimension": "tag", "key": "team"}}
					})
				},
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "group_by.0.dimension", "Tag"),
					resource.TestCheckResourceAttr(r, "group_by.0.key", "team"),
				),
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{PostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(r, plancheck.ResourceActionUpdate),
				}},
			},
		},
	})
}

// The API refuses each of these label filters with a 400 on unitCostLabelFilter; the provider
// refuses them at plan, naming the condition, before a request is sent. 51 emoji are 102 UTF-16
// code units, over the API's limit of 100.
func TestCostReportRefusesAUnitCostLabelFilterTheAPIRefusesAtPlan(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("v", 101)
	wide := strings.Repeat("😀", 51)
	values := func(count int) string {
		quoted := make([]string, count)
		for i := range quoted {
			quoted[i] = fmt.Sprintf("%q", fmt.Sprintf("v%03d", i))
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	conditions := func(count int) string {
		listed := make([]string, count)
		for i := range listed {
			listed[i] = fmt.Sprintf(`{ key = "c%d", values = ["a"] }`, i)
		}
		return "[" + strings.Join(listed, ", ") + "]"
	}
	cases := map[string]struct{ filter, want string }{
		"more than five conditions":  {conditions(6), `unit_cost_label_filter list must contain at least 1\s+elements and at\s+most 5`},
		"a blank key":                {`[{ key = "  ", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100\s+characters`},
		"a tab key":                  {`[{ key = "\t", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100\s+characters`},
		"a no-break space key":       {`[{ key = "\u00a0", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100\s+characters`},
		"an ideographic space key":   {`[{ key = "\u3000", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100\s+characters`},
		"a key over the limit":       {`[{ key = "` + long + `", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100`},
		"a key of 102 code units":    {`[{ key = "` + wide + `", values = ["eu"] }]`, `unit_cost_label_filter\[0\]\.key: a label column is named in 1 to 100`},
		"a key twice ignoring case":  {`[{ key = "region", values = ["eu"] }, { key = " Region ", values = ["us"] }]`, `unit_cost_label_filter\[1\]\.key repeats the label column of\s+settings\.unit_cost_label_filter\[0\]`},
		"a blank value":              {`[{ key = "region", values = ["eu", " "] }]`, `unit_cost_label_filter\[0\]\.values\[1\]: a label is 1 to 100 characters`},
		"a tab value":                {`[{ key = "region", values = ["eu", "\t"] }]`, `unit_cost_label_filter\[0\]\.values\[1\]: a label is 1 to 100 characters`},
		"a no-break space value":     {`[{ key = "region", values = ["eu", "\u00a0"] }]`, `unit_cost_label_filter\[0\]\.values\[1\]: a label is 1 to 100 characters`},
		"an ideographic space value": {`[{ key = "region", values = ["eu", "\u3000"] }]`, `unit_cost_label_filter\[0\]\.values\[1\]: a label is 1 to 100 characters`},
		"a null value":               {`[{ key = "region", values = ["eu", null] }]`, `unit_cost_label_filter\[0\]\.values\[1\]: a label is 1 to 100 characters`},
		"a value over the limit":     {`[{ key = "region", values = ["` + long + `"] }]`, `unit_cost_label_filter\[0\]\.values\[0\]: a label is 1 to 100`},
		"a value of 102 code units":  {`[{ key = "region", values = ["` + wide + `"] }]`, `unit_cost_label_filter\[0\]\.values\[0\]: a label is 1 to 100`},
		"more than 100 values":       {`[{ key = "region", values = ` + values(101) + ` }]`, `unit_cost_label_filter\[0\]\.values: a condition counts at most 100\s+different labels`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{{
					Config: costReportConfig(api, `
  title    = "Refused"
  settings = {
    unit_cost_metric_id    = "bmt_0000000000000000000001"
    unit_cost_label_filter = `+c.filter+`
  }
`),
					PlanOnly:    true,
					ExpectError: regexp.MustCompile(c.want),
				}},
			})
			if sent := api.Requests(http.MethodPost, "/v1/cost-reports"); len(sent) != 0 {
				t.Fatalf("a refused label filter sent %d creates, want none", len(sent))
			}
		})
	}
}

// What the API takes plans and applies clean, and a plan of the same configuration then changes
// nothing: five conditions, 100 different values with one repeated once trimmed, a key and a value
// at the limit once trimmed, in ASCII and in characters of one UTF-16 code unit, keys the API's case
// folding keeps apart, keys, values and the filter still unknown at plan, and keys and
// values to trim and values repeated, which the API stores trimmed and once.
func TestCostReportTakesTheUnitCostLabelFiltersTheAPITakes(t *testing.T) {
	t.Parallel()
	hundred := make([]string, 0, 101)
	for i := range 100 {
		hundred = append(hundred, fmt.Sprintf("%q", fmt.Sprintf("v%03d", i)))
	}
	hundred = append(hundred, `" v000 "`)
	cases := map[string]string{
		"five conditions": `[{ key = "a", values = ["1"] }, { key = "b", values = ["1"] }, { key = "c", values = ["1"] },
    { key = "d", values = ["1"] }, { key = "e", values = ["1"] }]`,
		"100 different values and a padded repeat": `[{ key = "region", values = [` + strings.Join(hundred, ", ") + `] }]`,
		"a key and a value at the limit":           `[{ key = " ` + strings.Repeat("k", 100) + ` ", values = [" ` + strings.Repeat("v", 100) + ` "] }]`,
		"a key and a value of 100 code units":      `[{ key = "` + strings.Repeat("é", 100) + `", values = ["` + strings.Repeat("é", 100) + `"] }]`,
		// .NET's OrdinalIgnoreCase upper-cases neither the dotless i nor the long s.
		"keys the API's case folding keeps apart": `[{ key = "ı", values = ["1"] }, { key = "I", values = ["1"] },
    { key = "ſ", values = ["1"] }, { key = "S", values = ["1"] }]`,
		"a key and a value unknown at plan": `[{ key = terraform_data.k.output, values = [terraform_data.k.output, "eu"] }]`,
		"the filter unknown at plan":        `terraform_data.f.output`,
		"a value repeated":                  `[{ key = "region", values = ["eu", "eu", "us"] }]`,
		"a key and values to trim":          `[{ key = " region ", values = [" eu", "eu ", "us"] }]`,
	}
	for name, filter := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			config := costReportConfig(api, `
  title    = "Taken"
  settings = {
    unit_cost_metric_id    = "bmt_0000000000000000000001"
    unit_cost_label_filter = `+filter+`
  }
`) + costReportUnknownAtPlan("k", `"region"`) + costReportUnknownAtPlan("f", `[{ key = "region", values = ["eu"] }]`)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: config},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}

// A slot, a condition, its values or the settings unknown as a whole at plan are left to the API, two
// unknown slots included, which are not the same slot. Terraform itself refuses a nested object
// unknown as a whole as missing its required attributes, so ValidateConfig is called directly to
// show it adds no error of its own.
func TestCostReportValidateConfigLeavesWholeUnknownsToTheAPI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := resources.NewCostReportResource()
	schemaResp := &fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, schemaResp)
	typ := schemaResp.Schema.Type().TerraformType(ctx)
	cases := map[string]map[string]any{
		"two slots": {"group_by": []any{tftypes.UnknownValue, tftypes.UnknownValue}},
		"a condition": {"settings": map[string]any{"unit_cost_label_filter": []any{
			tftypes.UnknownValue, map[string]any{"key": "plan", "values": []any{"pro"}},
		}}},
		"a condition's values": {"settings": map[string]any{"unit_cost_label_filter": []any{
			map[string]any{"key": "region", "values": tftypes.UnknownValue},
		}}},
		"the settings": {"settings": tftypes.UnknownValue},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config["workspace_id"], config["title"] = testWorkspace, "Unknown"
			resp := &fwresource.ValidateConfigResponse{}
			r.(fwresource.ResourceWithValidateConfig).ValidateConfig(ctx, fwresource.ValidateConfigRequest{
				Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: tfValue(typ, config)},
			}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("ValidateConfig = %v, want no error", resp.Diagnostics)
			}
		})
	}
}

const costReportFilterClearing = "  title    = \"AWS\"\n  group_by = [{ dimension = \"ServiceName\" }]\n"

// The API leaves an omitted filter as it is and removes one only on clearFilter, so removing filter
// from a configuration has to send it, and only then.
func TestCostReportRemovingTheFilterClearsIt(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costReportConfig(api, costReportFilterClearing+"  filter = \""+costReportQuery+"\"\n")},
			{
				Config: costReportConfig(api, costReportFilterClearing),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "filter"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if update.Body["clearFilter"] != true {
							return fmt.Errorf("removing the filter sent %s, want clearFilter", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costReportConfig(api, strings.Replace(costReportFilterClearing, `"AWS"`, `"All"`, 1)),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
					if _, sent := update.Body["clearFilter"]; sent {
						return fmt.Errorf("a rename without a filter sent clearFilter: %s", update.Raw)
					}
					return nil
				},
			},
		},
	})
}

// The API answers the folder a report keeps when an update names none, and takes it out only on
// clearFolder, so removing folder_id from a configuration has to send it, and only then.
func TestCostReportRemovingTheFolderTakesItOut(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costReportConfig(api, costReportFilterClearing+"  folder_id = \""+costReportFolder+"\"\n")},
			{
				Config: costReportConfig(api, costReportFilterClearing),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "folder_id"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if update.Body["clearFolder"] != true {
							return fmt.Errorf("removing the folder sent %s, want clearFolder", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costReportConfig(api, strings.Replace(costReportFilterClearing, `"AWS"`, `"All"`, 1)),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
					if _, sent := update.Body["clearFolder"]; sent {
						return fmt.Errorf("a rename without a folder sent clearFolder: %s", update.Raw)
					}
					return nil
				},
			},
		},
	})
}

func TestCostReportCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/cost-reports", "crp_", "costfluent_cost_report.r",
		costReportConfig(api, costReportFull), "Failed to create cost report")
}

func TestCostReportCreateWhoseFilterTranslationFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	createWhoseFilterTranslationFailsIsTainted(t, api, "/v1/cost-reports", "crp_", "costfluent_cost_report.r",
		costReportConfig(api, costReportFull), "Failed to compare the cost report filter")
}

// A usage report sends its measure and reads it back; a settings block that names none keeps it.
func TestCostReportMeasureIsSentAndKept(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "Hours by service"
  settings = { measure = "usage" }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.measure", "usage"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						settings, _ := create.Body["settings"].(map[string]any)
						if settings["measure"] != "usage" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costReportConfig(api, `
  title    = "Cost by service"
  settings = { amortize = false }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.measure", "usage"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						settings, _ := update.Body["settings"].(map[string]any)
						if _, sent := settings["measure"]; sent {
							return fmt.Errorf("update sent a measure the configuration does not name: %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// Settings flags the configuration leaves out are not sent: a new report takes the workspace's
// default for them, here amortize, and an update keeps the report's own (R29 FU3).
func TestCostReportSendsOnlyTheConfiguredSettingsFlags(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	sentFlags := func(method, path string, want ...string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			request := api.LastRequest(method, path)
			settings, _ := request.Body["settings"].(map[string]any)
			if len(settings) != len(want) {
				return fmt.Errorf("settings sent = %v, want only %v", settings, want)
			}
			for _, k := range want {
				if _, sent := settings[k]; !sent {
					return fmt.Errorf("settings sent = %v, want only %v", settings, want)
				}
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "Tax included"
  settings = { include_tax = true }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.include_tax", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.amortize", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.compare_to_last_period", "false"),
					sentFlags(http.MethodPost, "/v1/cost-reports", "includeTax"),
				),
			},
			{
				Config: costReportConfig(api, `
  title    = "Tax included"
  settings = { include_tax = true, show_forecast = true }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.show_forecast", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.amortize", "true"),
					func(s *terraform.State) error {
						return sentFlags(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api),
							"includeTax", "showForecast")(s)
					},
				),
			},
		},
	})
}

// A comparison window set in the app is not in the schema, and the API takes it as sent whenever
// settings are, so an update carrying settings sends the stored window back; flags set in the app
// and left out of the configuration keep their stored values too.
func TestCostReportSettingsUpdateKeepsTheComparisonWindow(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	withSettings := func(title string) string {
		return costReportConfig(api, fmt.Sprintf("  title    = %q\n  settings = { include_credits = true }\n", title))
	}
	stored := func() map[string]any {
		obj, _ := api.Get("/v1/cost-reports", costReportOnlyID(t, api))
		settings, _ := obj["settings"].(map[string]any)
		return settings
	}
	window := func(want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
			sent, _ := update.Body["settings"].(map[string]any)
			if fmt.Sprint(sent["comparisonStartDate"], " ", sent["comparisonEndDate"]) != want {
				return fmt.Errorf("update settings = %v, want the window %s", sent, want)
			}
			if _, sent := sent["includeTax"]; sent {
				return fmt.Errorf("update sent the unconfigured includeTax: %s", update.Raw)
			}
			if got := fmt.Sprint(stored()["comparisonStartDate"], " ", stored()["comparisonEndDate"]); got != want {
				return fmt.Errorf("stored window = %s, want %s", got, want)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: withSettings("Credits")},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) {
						settings := obj["settings"].(map[string]any)
						settings["comparisonStartDate"] = "2026-09-01"
						settings["comparisonEndDate"] = "2026-09-30"
						settings["includeTax"] = true
					})
				},
				Config: withSettings("Credits renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.include_tax", "true"),
					window("2026-09-01 2026-09-30"),
				),
			},
			{Config: withSettings("Credits renamed"), PlanOnly: true},
			{
				Config: costReportConfig(api, "  title = \"Credits again\"\n"),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
					if _, sent := update.Body["settings"]; sent {
						return fmt.Errorf("an update without settings sent them: %s", update.Raw)
					}
					if got := fmt.Sprint(stored()["comparisonStartDate"], " ", stored()["comparisonEndDate"]); got != "2026-09-01 2026-09-30" {
						return fmt.Errorf("stored window = %s, want it kept", got)
					}
					return nil
				},
			},
			{Config: costReportConfig(api, "  title = \"Credits again\"\n"), PlanOnly: true},
		},
	})
}

// An update whose read of the stored comparison window fails sends nothing, rather than a PUT that
// would clear the window. A step of resource.UnitTest cannot fail that read alone, because the
// refresh before it reads the same path first, so this drives the apply through the protocol.
func TestCostReportUpdateWhoseReadFailsSendsNoUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	id := api.NextID("crp_")
	path := "/v1/cost-reports/" + id
	api.PutIn("/v1/cost-reports", testWorkspace, id, map[string]any{"title": "Credits"})

	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{
		Config: protocolValue(t, schemas.Provider.ValueType(), map[string]any{"api_key": "cf_org_test", "base_url": api.URL}),
	})
	if err != nil || len(configured.Diagnostics) > 0 {
		t.Fatalf("configure: %v %v", err, configured.Diagnostics)
	}

	reportType := schemas.ResourceSchemas["costfluent_cost_report"].ValueType()
	report := func(title string, credits bool) map[string]any {
		return map[string]any{
			"id": id, "workspace_id": testWorkspace, "title": title,
			"group_by": []any{map[string]any{"dimension": "ServiceName"}},
			"settings": map[string]any{
				"amortize": true, "include_credits": credits, "include_refunds": false, "include_tax": false,
				"show_forecast": false, "compare_to_last_period": false, "measure": "cost",
			},
		}
	}
	api.FailNext(http.MethodGet, path, fakeapi.Problem(http.StatusForbidden, "Forbidden", "not this report"))

	applied, err := server.ApplyResourceChange(ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     "costfluent_cost_report",
		PriorState:   protocolValue(t, reportType, report("Credits", false)),
		PlannedState: protocolValue(t, reportType, report("Credits renamed", true)),
		Config:       protocolValue(t, reportType, report("Credits renamed", true)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Diagnostics) != 1 || applied.Diagnostics[0].Summary != "Failed to read cost report before updating it" {
		t.Fatalf("diagnostics = %+v, want the failed read", applied.Diagnostics)
	}
	if puts := api.Requests(http.MethodPut, path); len(puts) != 0 {
		t.Fatalf("sent %d updates after the read failed", len(puts))
	}
}

// protocolValue is values as a value of typ, every attribute values leaves out null.
func protocolValue(t *testing.T, typ tftypes.Type, values map[string]any) *tfprotov6.DynamicValue {
	t.Helper()
	dv, err := tfprotov6.NewDynamicValue(typ, tfValue(typ, values))
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

func tfValue(typ tftypes.Type, v any) tftypes.Value {
	if v == nil || v == tftypes.UnknownValue {
		return tftypes.NewValue(typ, v)
	}
	switch typ := typ.(type) {
	case tftypes.Object:
		given, _ := v.(map[string]any)
		attrs := map[string]tftypes.Value{}
		for name, attrType := range typ.AttributeTypes {
			attrs[name] = tfValue(attrType, given[name])
		}
		return tftypes.NewValue(typ, attrs)
	case tftypes.List:
		var elems []tftypes.Value
		for _, e := range v.([]any) {
			elems = append(elems, tfValue(typ.ElementType, e))
		}
		return tftypes.NewValue(typ, elems)
	default:
		return tftypes.NewValue(typ, v)
	}
}

// A report naming a business metric sends it and reads it back; settings that leave it out clear it,
// as the API takes it as sent.
func TestCostReportUnitCostMetricIsSentAndCleared(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "Cost per customer"
  settings = { unit_cost_metric_id = "bmt_0000000000000000000001" }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_metric_id",
						"bmt_0000000000000000000001"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						settings, _ := create.Body["settings"].(map[string]any)
						if settings["unitCostMetricId"] != "bmt_0000000000000000000001" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costReportConfig(api, `
  title    = "Cost"
  settings = { amortize = false }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "settings.unit_cost_metric_id"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						settings, _ := update.Body["settings"].(map[string]any)
						if _, sent := settings["unitCostMetricId"]; sent {
							return fmt.Errorf("update sent a metric the configuration does not name: %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// A label filter is sent in the configured order and read back; settings that leave it out clear
// it, as the API takes it as sent.
func TestCostReportUnitCostLabelFilterIsSentAndCleared(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "Cost per EU pro customer"
  settings = {
    unit_cost_metric_id    = "bmt_0000000000000000000001"
    unit_cost_label_filter = [
      { key = "region", values = ["eu", "us"] },
      { key = "plan", values = ["pro"] },
    ]
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_label_filter.#", "2"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_label_filter.0.key", "region"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_label_filter.0.values.1", "us"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_label_filter.1.key", "plan"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-reports")
						settings, _ := create.Body["settings"].(map[string]any)
						filter, _ := settings["unitCostLabelFilter"].([]any)
						if len(filter) != 2 {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						first, _ := filter[0].(map[string]any)
						values, _ := first["values"].([]any)
						if first["key"] != "region" || len(values) != 2 || values[0] != "eu" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costReportConfig(api, `
  title    = "Cost per customer"
  settings = { unit_cost_metric_id = "bmt_0000000000000000000001" }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "settings.unit_cost_label_filter"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						settings, _ := update.Body["settings"].(map[string]any)
						if _, sent := settings["unitCostLabelFilter"]; sent {
							return fmt.Errorf("update sent a filter the configuration does not name: %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// The API trims every label key and value and counts each value once, so a filter with a padded key,
// a padded value or a repeated value applies as configured on create and update and plans nothing
// after. A filter changed in the app normalizes to something else and is read back as stored.
func TestCostReportUnitCostLabelFilterKeepsTheConfiguredSpelling(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	const r = "costfluent_cost_report.r"
	charting := func(filter string) string {
		return costReportConfig(api, `
  title    = "Cost per customer"
  settings = {
    unit_cost_metric_id    = "bmt_0000000000000000000001"
    unit_cost_label_filter = `+filter+`
  }
`)
	}
	storedAs := func(want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			stored, _ := api.Get("/v1/cost-reports", costReportOnlyID(t, api))
			settings, _ := stored["settings"].(map[string]any)
			if got := fmt.Sprint(settings["unitCostLabelFilter"]); got != want {
				return fmt.Errorf("stored filter = %s, want %s", got, want)
			}
			return nil
		}
	}
	created := `[{ key = " region", values = ["eu", "eu "] }]`
	updated := `[{ key = "region ", values = ["eu", " us", "eu"] }, { key = "plan", values = ["pro\n"] }]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: charting(created),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.key", " region"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.values.#", "2"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.values.1", "eu "),
					storedAs("[map[key:region values:[eu]]]"),
				),
			},
			{Config: charting(created), PlanOnly: true},
			{
				Config: charting(updated),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.key", "region "),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.values.#", "3"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.1.values.0", "pro\n"),
					storedAs("[map[key:region values:[eu us]] map[key:plan values:[pro]]]"),
				),
			},
			{Config: charting(updated), PlanOnly: true},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) {
						settings, _ := obj["settings"].(map[string]any)
						settings["unitCostLabelFilter"] = []any{
							map[string]any{"key": "region", "values": []any{"us", "eu"}},
							map[string]any{"key": "plan", "values": []any{"pro"}},
						}
					})
				},
				RefreshState: true,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.key", "region"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.values.#", "2"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.0.values.0", "us"),
					resource.TestCheckResourceAttr(r, "settings.unit_cost_label_filter.1.values.0", "pro"),
				),
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{PostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(r, plancheck.ResourceActionUpdate),
				}},
			},
		},
	})
}

// A gross margin and a per thousand scale are sent when configured and read back, and a plan of the
// same configuration changes nothing.
func TestCostReportUnitCostCalculationIsSentAndKept(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	sentSettings := func(method, path string) map[string]any {
		settings, _ := api.LastRequest(method, path).Body["settings"].(map[string]any)
		return settings
	}
	perThousand := costReportConfig(api, `
  title    = "Cost per thousand customers"
  settings = {
    unit_cost_metric_id   = "bmt_0000000000000000000001"
    unit_cost_calculation = "perUnit"
    unit_cost_scale       = 1000
  }
`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, `
  title    = "Margin"
  settings = {
    unit_cost_metric_id   = "bmt_0000000000000000000001"
    unit_cost_calculation = "grossMargin"
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_calculation", "grossMargin"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_scale", "1"),
					func(*terraform.State) error {
						settings := sentSettings(http.MethodPost, "/v1/cost-reports")
						if settings["unitCostCalculation"] != "grossMargin" {
							return fmt.Errorf("create settings = %v, want the gross margin", settings)
						}
						if _, sent := settings["unitCostScale"]; sent {
							return fmt.Errorf("create sent a scale the configuration does not name: %v", settings)
						}
						return nil
					},
				),
			},
			{
				Config: perThousand,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_calculation", "perUnit"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_scale", "1000"),
					func(*terraform.State) error {
						settings := sentSettings(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
						if settings["unitCostCalculation"] != "perUnit" || settings["unitCostScale"] != float64(1000) {
							return fmt.Errorf("update settings = %v, want per unit per thousand", settings)
						}
						return nil
					},
				),
			},
			{Config: perThousand, PlanOnly: true},
		},
	})
}

// The API keeps the calculation and scale an update naming the metric leaves out, so a configuration
// that does not name them sends neither and adopts the report's own, here a margin set in the app;
// settings without the metric return both to per unit, per 1.
func TestCostReportUnitCostCalculationLeftOutKeepsTheReportsOwn(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	perCustomer := func(title string) string {
		return costReportConfig(api, fmt.Sprintf("  title    = %q\n  settings = { unit_cost_metric_id = %q }\n",
			title, "bmt_0000000000000000000001"))
	}
	sentNeither := func(method, path string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			request := api.LastRequest(method, path)
			settings, _ := request.Body["settings"].(map[string]any)
			for _, k := range []string{"unitCostCalculation", "unitCostScale"} {
				if _, sent := settings[k]; sent {
					return fmt.Errorf("sent %s the configuration does not name: %s", k, request.Raw)
				}
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: perCustomer("Per customer"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_calculation", "perUnit"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_scale", "1"),
					sentNeither(http.MethodPost, "/v1/cost-reports"),
				),
			},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) {
						obj["settings"].(map[string]any)["unitCostCalculation"] = "grossMargin"
					})
				},
				Config: perCustomer("Margin"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "title", "Margin"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_calculation", "grossMargin"),
					func(s *terraform.State) error {
						return sentNeither(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))(s)
					},
				),
			},
			{Config: perCustomer("Margin"), PlanOnly: true},
			{
				Config: costReportConfig(api, "  title    = \"Cost\"\n  settings = { amortize = false }\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_report.r", "settings.unit_cost_metric_id"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_calculation", "perUnit"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.unit_cost_scale", "1"),
				),
			},
		},
	})
}

func TestCostReportRefusesAnUnknownUnitCostCalculationOrScale(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	withSetting := func(setting string) string {
		return costReportConfig(api, fmt.Sprintf("  title    = \"Per customer\"\n  settings = {\n"+
			"    unit_cost_metric_id = \"bmt_0000000000000000000001\"\n    %s\n  }\n", setting))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      withSetting(`unit_cost_calculation = "margin"`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				Config:      withSetting(`unit_cost_scale = 10`),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

// A comparison mode the configuration names is sent; one it leaves out is not, so a new report takes
// the previous period and an update keeps the report's own, here the year-ago mode set before.
func TestCostReportComparisonModeIsSentAndKept(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	withSettings := func(title, settings string) string {
		return costReportConfig(api, fmt.Sprintf("  title    = %q\n  settings = {\n    compare_to_last_period = true\n%s  }\n",
			title, settings))
	}
	sentMode := func(method string, want any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			path := "/v1/cost-reports"
			if method == http.MethodPut {
				path += "/" + costReportOnlyID(t, api)
			}
			request := api.LastRequest(method, path)
			settings, _ := request.Body["settings"].(map[string]any)
			if mode, sent := settings["comparisonMode"]; mode != want || (want == nil && sent) {
				return fmt.Errorf("sent comparisonMode %v, want %v: %s", mode, want, request.Raw)
			}
			return nil
		}
	}
	yearAgo := "    comparison_mode        = \"samePeriodLastYear\"\n"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: withSettings("Versus last month", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.comparison_mode", "previousPeriod"),
					sentMode(http.MethodPost, nil),
				),
			},
			{
				Config: withSettings("Versus last year", yearAgo),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.comparison_mode", "samePeriodLastYear"),
					sentMode(http.MethodPut, "samePeriodLastYear"),
				),
			},
			{Config: withSettings("Versus last year", yearAgo), PlanOnly: true},
			{
				Config: withSettings("Year on year", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "title", "Year on year"),
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "settings.comparison_mode", "samePeriodLastYear"),
					sentMode(http.MethodPut, nil),
				),
			},
			{Config: withSettings("Year on year", ""), PlanOnly: true},
		},
	})
}

func TestCostReportRefusesAnUnknownComparisonMode(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      costReportConfig(api, "  title    = \"Year on year\"\n  settings = { comparison_mode = \"lastYear\" }\n"),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`value must be one of`),
		}},
	})
}

func TestCostReportRefusesALabelFilterWithoutValues(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: costReportConfig(api, `
  title    = "Cost per customer"
  settings = {
    unit_cost_metric_id    = "bmt_0000000000000000000001"
    unit_cost_label_filter = [{ key = "region", values = [] }]
  }
`),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`must contain at\s+least 1 elements`),
		}},
	})
}

func TestCostReportRefusesAUnitCostMetricWithoutItsPrefix(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: costReportConfig(api, `
  title    = "Cost per customer"
  settings = { unit_cost_metric_id = "flt_0000000000000000000001" }
`),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile(`Invalid token format`),
		}},
	})
}

func TestCostReportRefusesAnUnknownMeasure(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: costReportConfig(api, `
  title    = "Tokens"
  settings = { measure = "tokens" }
`),
			ExpectError: regexp.MustCompile(`value must be one of`),
		}},
	})
}

const costReportDated = "  title         = \"Q1\"\n  date_interval = \"custom\"\n"

func costReportSentDates(api *fakeapi.Server, method, path, start, end string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		body := api.LastRequest(method, path).Body
		for key, want := range map[string]string{"startDate": start, "endDate": end} {
			got, sent := body[key]
			if want == "" && sent {
				return fmt.Errorf("%s %s sent %s = %v, want it left out", method, path, key, got)
			}
			if want != "" && got != want {
				return fmt.Errorf("%s %s sent %s = %v, want %s", method, path, key, got, want)
			}
		}
		return nil
	}
}

// costReportSentClears checks the latest update of the only report sent clearStartDate and
// clearEndDate as wanted, and neither flag where it is not.
func costReportSentClears(t *testing.T, api *fakeapi.Server, start, end bool) resource.TestCheckFunc {
	return func(*terraform.State) error {
		update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
		for flag, want := range map[string]bool{"clearStartDate": start, "clearEndDate": end} {
			if got, sent := update.Body[flag]; (got == true) != want || (sent && got != true) {
				return fmt.Errorf("update sent %s = %v, want %t: %s", flag, got, want, update.Raw)
			}
		}
		return nil
	}
}

// The API keeps a date an update leaves out and refuses to clear one of a custom window, which needs
// both, so removing start_date or end_date from a custom report's configuration keeps the stored day
// rather than failing every apply. The interval is matched in any case, as the API reads it.
func TestCostReportRemovingTheDatesKeepsTheStoredOnes(t *testing.T) {
	t.Parallel()
	removals := map[string]string{
		"only end_date":            costReportDated + "  start_date = \"2026-04-01\"\n",
		"both dates":               costReportDated,
		"both dates, Custom cased": "  title         = \"Q1\"\n  date_interval = \"Custom\"\n",
	}
	for name, removed := range removals {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			const r = "costfluent_cost_report.r"

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: costReportConfig(api, costReportDated+"  start_date = \"2026-01-01\"\n  end_date = \"2026-03-31\"\n"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r, "start_date", "2026-01-01"),
							resource.TestCheckResourceAttr(r, "end_date", "2026-03-31"),
							costReportSentDates(api, http.MethodPost, "/v1/cost-reports", "2026-01-01", "2026-03-31"),
						),
					},
					{
						Config: costReportConfig(api, costReportDated+"  start_date = \"2026-04-01\"\n  end_date = \"2026-06-30\"\n"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r, "start_date", "2026-04-01"),
							resource.TestCheckResourceAttr(r, "end_date", "2026-06-30"),
							func(s *terraform.State) error {
								path := "/v1/cost-reports/" + costReportOnlyID(t, api)
								return costReportSentDates(api, http.MethodPut, path, "2026-04-01", "2026-06-30")(s)
							},
						),
					},
					{
						Config: costReportConfig(api, removed),
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectKnownValue(r, tfjsonpath.New("start_date"), knownvalue.StringExact("2026-04-01")),
							plancheck.ExpectKnownValue(r, tfjsonpath.New("end_date"), knownvalue.StringExact("2026-06-30")),
						}},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r, "start_date", "2026-04-01"),
							resource.TestCheckResourceAttr(r, "end_date", "2026-06-30"),
							costReportSentClears(t, api, false, false),
						),
					},
					{Config: costReportConfig(api, removed), PlanOnly: true},
				},
			})
		})
	}
}

// A relative window needs no dates, so a date removed from its configuration plans null and the
// update clears it with clearStartDate or clearEndDate, sending no date in its place, whether the
// report moves off a custom window or was relative already and the removal is the only change.
func TestCostReportRemovingTheDatesOfARelativeWindowClearsThem(t *testing.T) {
	t.Parallel()
	const (
		bothDates = "  start_date = \"2026-01-01\"\n  end_date = \"2026-03-31\"\n"
		relative  = "  title         = \"Last 30 days\"\n  date_interval = \"last30Days\"\n"
	)
	cases := map[string]struct {
		before, after string
		start         string
		clearStart    bool
	}{
		"moved from custom to last month": {
			before:     costReportDated + bothDates,
			after:      "  title         = \"Q1\"\n  date_interval = \"lastMonth\"\n",
			clearStart: true,
		},
		"both removed, nothing else changing": {before: relative + bothDates, after: relative, clearStart: true},
		// The update sends no interval, so the API keeps its own, this month, though the plan reads
		// the interval unknown beside the rename.
		"both removed, interval left out, renamed": {
			before:     "  title = \"Dated\"\n" + bothDates,
			after:      "  title = \"Renamed\"\n",
			clearStart: true,
		},
		"only end_date removed": {
			before: relative + bothDates,
			after:  relative + "  start_date = \"2026-01-01\"\n",
			start:  "2026-01-01",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			const r = "costfluent_cost_report.r"
			var startPlanned knownvalue.Check = knownvalue.Null()
			if c.start != "" {
				startPlanned = knownvalue.StringExact(c.start)
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: costReportConfig(api, c.before),
						Check:  resource.TestCheckResourceAttr(r, "end_date", "2026-03-31"),
					},
					{
						Config: costReportConfig(api, c.after),
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction(r, plancheck.ResourceActionUpdate),
							plancheck.ExpectKnownValue(r, tfjsonpath.New("start_date"), startPlanned),
							plancheck.ExpectKnownValue(r, tfjsonpath.New("end_date"), knownvalue.Null()),
						}},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckNoResourceAttr(r, "end_date"),
							func(s *terraform.State) error {
								if c.start == "" {
									return resource.TestCheckNoResourceAttr(r, "start_date")(s)
								}
								return resource.TestCheckResourceAttr(r, "start_date", c.start)(s)
							},
							func(s *terraform.State) error {
								path := "/v1/cost-reports/" + costReportOnlyID(t, api)
								return costReportSentDates(api, http.MethodPut, path, c.start, "")(s)
							},
							costReportSentClears(t, api, c.clearStart, true),
						),
					},
					{Config: costReportConfig(api, c.after), PlanOnly: true},
				},
			})
		})
	}
}

// The console moves a custom report to a relative window without clearing its dates, so the API
// answers the relative interval with both dates stored. Importing that report with a configuration
// that sets no dates plans clearing them, as it would for a report Terraform managed all along.
func TestCostReportImportOfARelativeWindowWithStoredDatesPlansClearingThem(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	const r = "costfluent_cost_report.r"
	relative := costReportConfig(api, "  title         = \"Q1\"\n  date_interval = \"last30Days\"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costReportConfig(api, costReportDated+"  start_date = \"2026-01-01\"\n  end_date = \"2026-03-31\"\n")},
			{
				PreConfig: func() {
					api.Update("/v1/cost-reports", costReportOnlyID(t, api), func(obj map[string]any) {
						obj["dateInterval"] = "last30Days"
					})
				},
				Config:             relative,
				ResourceName:       r,
				ImportState:        true,
				ImportStateKind:    resource.ImportBlockWithID,
				ImportStateIdFunc:  func(*terraform.State) (string, error) { return testWorkspace + ":" + costReportOnlyID(t, api), nil },
				ExpectNonEmptyPlan: true,
				ImportPlanChecks: resource.ImportPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(r, plancheck.ResourceActionUpdate),
					plancheck.ExpectKnownValue(r, tfjsonpath.New("date_interval"), knownvalue.StringExact("last30Days")),
					plancheck.ExpectKnownValue(r, tfjsonpath.New("start_date"), knownvalue.Null()),
					plancheck.ExpectKnownValue(r, tfjsonpath.New("end_date"), knownvalue.Null()),
				}},
			},
			// Applying that plan clears both dates.
			{
				Config: relative,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(r, "start_date"),
					resource.TestCheckNoResourceAttr(r, "end_date"),
					costReportSentClears(t, api, true, true),
				),
			},
		},
	})
}

// A report that never names a date sends none, stores none, and plans nothing on the next run or
// after a change elsewhere.
func TestCostReportWithoutDatesSendsNone(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	costReportFake(api)
	const r = "costfluent_cost_report.r"
	renamed := costReportConfig(api, "  title = \"Renamed\"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costReportConfig(api, "  title = \"Undated\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(r, "start_date"),
					resource.TestCheckNoResourceAttr(r, "end_date"),
					costReportSentDates(api, http.MethodPost, "/v1/cost-reports", "", ""),
				),
			},
			{
				Config: renamed,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectKnownValue(r, tfjsonpath.New("start_date"), knownvalue.Null()),
					plancheck.ExpectKnownValue(r, tfjsonpath.New("end_date"), knownvalue.Null()),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(r, "start_date"),
					resource.TestCheckNoResourceAttr(r, "end_date"),
					func(s *terraform.State) error {
						return costReportSentDates(api, http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api), "", "")(s)
					},
				),
			},
			{Config: renamed, PlanOnly: true},
		},
	})
}

// costReportChartingMetricConfig is a business metric and a report charting its unit cost, as
// charting sets it, counting the metric's eu values; columns are the metric's label columns, which
// replace it when they change.
func costReportChartingMetricConfig(api *fakeapi.Server, title, columns, charting string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_business_metric" "m" {
  workspace_id  = %[1]q
  name          = "Customers"
  unit          = "customer"
  label_columns = %[3]s
}

resource "costfluent_cost_report" "r" {
  workspace_id = %[1]q
  title        = %[2]q
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.m.id
    unit_cost_label_filter = [{ key = "region", values = ["eu"] }]
    %[4]s
  }
}
`, testWorkspace, title, columns, charting)
}

const costReportPerThousand = `unit_cost_scale = 1000`

// costReportMovedToAnotherMetricConfig keeps the metric costReportChartingMetricConfig charts with
// the columns ["region"] and moves the report to a second metric, which the apply creates.
func costReportMovedToAnotherMetricConfig(api *fakeapi.Server) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_business_metric" "m" {
  workspace_id  = %[1]q
  name          = "Customers"
  unit          = "customer"
  label_columns = ["region"]
}

resource "costfluent_business_metric" "other" {
  workspace_id  = %[1]q
  name          = "Accounts"
  unit          = "account"
  label_columns = ["region"]
}

resource "costfluent_cost_report" "r" {
  workspace_id = %[1]q
  title        = "Cost per customer"
  settings = {
    unit_cost_metric_id    = costfluent_business_metric.other.id
    unit_cost_label_filter = [{ key = "region", values = ["eu"] }]
    %[2]s
  }
}
`, testWorkspace, costReportPerThousand)
}

// costReportUnitCostClearedBefore lands, as the next request for method and path is served, what
// the API holds for a report charting no metric, whether the deletion of its metric reset it or
// someone turned its unit cost off: cost alone, no label filter, per unit and per 1, under a new
// version. elsewhere is a further change made to the report outside Terraform at the same moment;
// the ETag after both is returned through tag.
func costReportUnitCostClearedBefore(
	t *testing.T, api *fakeapi.Server, method, path string, elsewhere func(obj map[string]any), tag *string,
) {
	t.Helper()
	report := costReportOnlyID(t, api)
	api.BeforeNext(method, path, func() { costReportUnitCostCleared(api, report, elsewhere, tag) })
}

// costReportUnitCostCleared lands what costReportUnitCostClearedBefore does, at once.
func costReportUnitCostCleared(api *fakeapi.Server, report string, elsewhere func(obj map[string]any), tag *string) {
	api.Update("/v1/cost-reports", report, func(obj map[string]any) {
		settings, _ := obj["settings"].(map[string]any)
		delete(settings, "unitCostMetricId")
		settings["unitCostLabelFilter"] = []any{}
		settings["unitCostCalculation"], settings["unitCostScale"] = "perUnit", 1
		if elsewhere != nil {
			elsewhere(obj)
		}
	})
	*tag = api.ETag("/v1/cost-reports", report)
}

// costReportResetOnMetricDelete lands the API's reset of the report, and elsewhere, as the
// deletion of the report's metric is served, which is what the API's foreign key does to every
// report charting it.
func costReportResetOnMetricDelete(t *testing.T, api *fakeapi.Server, elsewhere func(obj map[string]any), tag *string) func() {
	return func() {
		costReportUnitCostClearedBefore(t, api, http.MethodDelete, "/v1/business-metrics/"+businessMetricOnlyID(t, api), elsewhere, tag)
	}
}

// Replacing the metric a report charts deletes the old metric before the report's update, and the
// API's reset of the report under that deletion is the apply's own doing, not a change made
// elsewhere: the update sends the version the reset left, charts the new metric with the label
// filter, calculation and scale, and the next plan changes nothing. A title changed in the same
// apply is sent with it; a calculation other than per unit is set again over the reset's.
func TestCostReportChartingAReplacedMetricUpdatesOverTheAPIsReset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, title, charting, calculation, scale string }{
		{"per thousand", "Cost per customer", costReportPerThousand, "perUnit", "1000"},
		{"retitled in the same apply", "Cost per EU customer", costReportPerThousand, "perUnit", "1000"},
		{"charting the metric's value", "Cost per customer", `unit_cost_calculation = "metricValue"`, "metricValue", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			businessMetricFake(api)
			var first, resetTag string
			replaced := costReportChartingMetricConfig(api, tc.title, `["region", "plan"]`, tc.charting)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: costReportChartingMetricConfig(api, "Cost per customer", `["region"]`, tc.charting),
						Check: func(*terraform.State) error {
							first = businessMetricOnlyID(t, api)
							return nil
						},
					},
					{
						PreConfig: costReportResetOnMetricDelete(t, api, nil, &resetTag),
						Config:    replaced,
						ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
							plancheck.ExpectResourceAction("costfluent_business_metric.m", plancheck.ResourceActionReplace),
							plancheck.ExpectResourceAction("costfluent_cost_report.r", plancheck.ResourceActionUpdate),
						}},
						Check: func(*terraform.State) error {
							metric := businessMetricOnlyID(t, api)
							if metric == first {
								return fmt.Errorf("business metric %s was not replaced", metric)
							}
							report := costReportOnlyID(t, api)
							update := api.LastRequest(http.MethodPut, "/v1/cost-reports/"+report)
							if got := update.Header.Get("If-Match"); resetTag == "" || got != resetTag {
								return fmt.Errorf("update sent If-Match %q, want the version the reset left %q", got, resetTag)
							}
							obj, _ := api.Get("/v1/cost-reports", report)
							settings, _ := obj["settings"].(map[string]any)
							filter, _ := json.Marshal(settings["unitCostLabelFilter"])
							if obj["title"] != tc.title || settings["unitCostMetricId"] != metric ||
								string(filter) != `[{"key":"region","values":["eu"]}]` ||
								settings["unitCostCalculation"] != tc.calculation || fmt.Sprint(settings["unitCostScale"]) != tc.scale {
								return fmt.Errorf("report after the apply = %v, want %q charting %s as %s per %s on eu values",
									obj, tc.title, metric, tc.calculation, tc.scale)
							}
							return nil
						},
					},
					{Config: replaced, PlanOnly: true},
				},
			})
		})
	}
}

// A report that charts no metric when the update reads it is still refused unless the metric state
// charts is gone and the deletion's reset is all that sets the report apart from state: the update
// then sends the version Terraform last read, so a change made elsewhere is kept, whether to any
// field the provider manages, to the metric charted, or the unit cost turned off in the app while
// the metric still exists, and the next apply reads it and applies over it. A read that carries no
// version sends the one Terraform last read too, never an unconditional write.
func TestCostReportResetAndChangedOutsideTerraformIsStillRefused(t *testing.T) {
	t.Parallel()
	inSettings := func(change func(settings map[string]any)) func(obj map[string]any) {
		return func(obj map[string]any) { change(obj["settings"].(map[string]any)) }
	}
	for _, tc := range []struct {
		name      string
		elsewhere func(obj map[string]any)
		// metricKept moves the report to a second metric while the one state charts stays, the
		// report's unit cost turned off elsewhere as the second metric is created.
		metricKept bool
		// unversioned serves the update's read of the reset report without an ETag.
		unversioned bool
		// metricUnreadable is metricKept with the metric state charts failing to be read once the
		// apply has started: the update is refused with that error, and nothing is written.
		metricUnreadable bool
	}{
		{name: "renamed", elsewhere: func(obj map[string]any) { obj["title"] = "Changed elsewhere" }},
		{name: "filed in a folder", elsewhere: func(obj map[string]any) { obj["folderId"] = costReportFolder }},
		{name: "drawn as a line", elsewhere: func(obj map[string]any) { obj["chartType"] = "line" }},
		{name: "drawn along its grouping", elsewhere: func(obj map[string]any) { obj["chartAxis"] = "group" }},
		{name: "binned by month", elsewhere: func(obj map[string]any) { obj["dateBin"] = "month" }},
		{name: "over last month", elsewhere: func(obj map[string]any) { obj["dateInterval"] = "lastMonth" }},
		{name: "starting on a date", elsewhere: func(obj map[string]any) { obj["startDate"] = "2026-09-01" }},
		{name: "ending on a date", elsewhere: func(obj map[string]any) { obj["endDate"] = "2026-09-30" }},
		{name: "filtered", elsewhere: func(obj map[string]any) { obj["filter"] = costReportStored }},
		{name: "grouped by provider", elsewhere: func(obj map[string]any) {
			obj["groupBy"] = []any{map[string]any{"dimension": "Provider"}}
		}},
		{name: "no longer amortized", elsewhere: inSettings(func(s map[string]any) { s["amortize"] = false })},
		{name: "charting another metric", elsewhere: inSettings(func(s map[string]any) {
			s["unitCostMetricId"] = "bmt_0000000000000000000099"
		})},
		{name: "unit cost turned off while its metric exists", metricKept: true},
		{name: "reset read back without a version", unversioned: true},
		{name: "charted metric unreadable", metricKept: true, metricUnreadable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			costReportFake(api)
			businessMetricFake(api)
			var resetTag string
			moved, charts := costReportChartingMetricConfig(api, "Cost per customer", `["region", "plan"]`, costReportPerThousand),
				"costfluent_business_metric.m"
			land := costReportResetOnMetricDelete(t, api, tc.elsewhere, &resetTag)
			if tc.metricKept {
				moved, charts = costReportMovedToAnotherMetricConfig(api), "costfluent_business_metric.other"
				land = func() {
					costReportUnitCostClearedBefore(t, api, http.MethodPost, "/v1/business-metrics", nil, &resetTag)
				}
			}
			refused := regexp.MustCompile(`Cost report changed outside Terraform`)
			if tc.metricUnreadable {
				refused = regexp.MustCompile(`Failed to read the business metric the cost report charted`)
				land = func() {
					// Installed as the second metric is created, after the plan has read the
					// first, so only the update's read of it fails.
					report, charted := costReportOnlyID(t, api), businessMetricOnlyID(t, api)
					api.BeforeNext(http.MethodPost, "/v1/business-metrics", func() {
						costReportUnitCostCleared(api, report, nil, &resetTag)
						api.FailNext(http.MethodGet, "/v1/business-metrics/"+charted,
							fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read business metrics"))
					})
				}
			}
			if tc.unversioned {
				reset := land
				land = func() {
					reset()
					// The replacement's create comes after the deletion's reset and before the
					// report's update reads it.
					report := costReportOnlyID(t, api)
					api.BeforeNext(http.MethodPost, "/v1/business-metrics", func() {
						obj, _ := api.Get("/v1/cost-reports", report)
						api.FailNext(http.MethodGet, "/v1/cost-reports/"+report, fakeapi.JSON(obj))
					})
				}
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: costReportChartingMetricConfig(api, "Cost per customer", `["region"]`, costReportPerThousand)},
					{
						PreConfig:   land,
						Config:      moved,
						ExpectError: refused,
					},
					{
						// The next apply reads the change made elsewhere and applies over it.
						PreConfig: func() {
							puts := api.Requests(http.MethodPut, "/v1/cost-reports/"+costReportOnlyID(t, api))
							if tc.metricUnreadable {
								if len(puts) != 0 {
									t.Errorf("updates = %d, want none sent when the charted metric cannot be read", len(puts))
								}
								return
							}
							if len(puts) != 1 || puts[0].Header.Get("If-Match") == "" || puts[0].Header.Get("If-Match") == resetTag {
								t.Errorf("updates = %d, want one refused update sending the version Terraform last read", len(puts))
							}
						},
						Config: moved,
						Check: func(s *terraform.State) error {
							obj, _ := api.Get("/v1/cost-reports", costReportOnlyID(t, api))
							settings, _ := obj["settings"].(map[string]any)
							metric := s.RootModule().Resources[charts].Primary.ID
							if obj["title"] != "Cost per customer" || settings["unitCostMetricId"] != metric {
								return fmt.Errorf("report after the next apply = %v, want it to chart %s", obj, metric)
							}
							return nil
						},
					},
				},
			})
		})
	}
}
