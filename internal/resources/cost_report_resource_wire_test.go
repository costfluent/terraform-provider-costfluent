package resources_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
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
// shapes. Like the API it stores a CQL filter as its JSON document, defaults the chart, range,
// grouping and settings a create leaves out (amortize standing for a workspace default that differs
// from false), leaves out of an update what was omitted, settings flags included, and removes the
// filter only on clearFilter and the folder only on clearFolder.
func costReportFake(api *fakeapi.Server) {
	api.Collection("/v1/cost-reports", fakeapi.Collection{
		IDPrefix:  "crp_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"title":        body["title"],
				"chartType":    "bar",
				"dateBin":      "day",
				"dateInterval": "thisMonth",
				"groupBy":      []any{map[string]any{"dimension": "ServiceName"}},
				"settings": map[string]any{"amortize": true, "includeCredits": false, "includeRefunds": false,
					"includeTax": false, "showForecast": false, "compareToLastPeriod": false, "measure": "cost"},
				"createdAt": "2026-10-05T09:00:00Z",
			}
			costReportApply(obj, body)
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "title": obj["title"]})
		},
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
		for k, v := range sent {
			settings[k] = v
		}
		obj["settings"] = settings
	}
	for _, k := range []string{"title", "folderId", "chartType", "dateBin", "dateInterval", "startDate", "endDate",
		"groupBy"} {
		if v, ok := body[k]; ok && v != nil {
			obj[k] = v
		}
	}
	if groups, ok := obj["groupBy"].([]any); ok {
		for _, g := range groups {
			slot, _ := g.(map[string]any)
			for _, canonical := range []string{"ServiceName", "Tag", "Provider"} {
				if strings.EqualFold(fmt.Sprint(slot["dimension"]), canonical) {
					slot["dimension"] = canonical
				}
			}
		}
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
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "date_interval", "thisMonth"),
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
					resource.TestCheckResourceAttr("costfluent_cost_report.r", "chart_type", "bar"),
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
	if v == nil {
		return tftypes.NewValue(typ, nil)
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

// The API keeps a date an update leaves out and has no way to clear one, so removing start_date or
// end_date from a configuration keeps the stored day rather than failing every apply.
func TestCostReportRemovingTheDatesKeepsTheStoredOnes(t *testing.T) {
	t.Parallel()
	removals := map[string]string{
		"only end_date": costReportDated + "  start_date = \"2026-04-01\"\n",
		"both dates":    "  title         = \"Last month\"\n  date_interval = \"lastMonth\"\n",
	}
	for name, removed := range removals {
		t.Run(name, func(t *testing.T) {
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
						),
					},
					{Config: costReportConfig(api, removed), PlanOnly: true},
				},
			})
		})
	}
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
