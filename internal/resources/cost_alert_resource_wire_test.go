package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	costAlertProvider = "prv_0000000000000000000042"
	costAlertApp      = "app_0000000000000000000007"

	costAlertQuery     = "costs.service = 'Amazon EC2'"
	costAlertStored    = `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"]}]`
	costAlertAWSQuery  = "costs.provider = 'aws'"
	costAlertAWSStored = `[{"field":"Provider","operator":"In","values":["aws"]}]`
)

// costAlertDocuments is how the API stores an alert filter sent as CQL: as its JSON document.
var costAlertDocuments = map[string]string{costAlertQuery: costAlertStored, costAlertAWSQuery: costAlertAWSStored}

// fakeCostAlerts serves /v1/cost-alerts with the CostAlertCreatedResponse and CostAlertResponse
// shapes: an update answers the alert as stored, the filter is stored as its JSON document, and
// pause and resume are actions. It also serves the translate operation.
func fakeCostAlerts(api *fakeapi.Server) {
	const path = "/v1/cost-alerts"
	api.Collection(path, fakeapi.Collection{
		IDPrefix:  "cal_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"name":                       body["name"],
				"thresholdType":              body["thresholdType"],
				"thresholdValue":             eightDecimals(body["thresholdValue"]),
				"comparisonPeriod":           body["comparisonPeriod"],
				"minimumAmount":              storedFloor(body["minimumAmount"]),
				"providerIds":                body["providerIds"],
				"filter":                     storedFilter(costAlertDocuments, body["filter"]),
				"appIds":                     body["appIds"],
				"evaluationFrequencyMinutes": 60,
				"lastEvaluatedAt":            "2026-10-01T10:00:00Z",
				"status":                     "active",
				"createdAt":                  "2026-10-01T09:00:00Z",
			}
			if m, ok := body["evaluationFrequencyMinutes"]; ok {
				obj["evaluationFrequencyMinutes"] = m
			}
			if obj["appIds"] == nil {
				obj["appIds"] = []any{}
			}
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "name": obj["name"]})
		},
		Apply: func(obj, body map[string]any) {
			clears := map[string]string{
				"clearComparisonPeriod": "comparisonPeriod", "clearMinimumAmount": "minimumAmount", "clearFilter": "filter",
			}
			for k, v := range body {
				if field, isClear := clears[k]; isClear {
					if v == true {
						delete(obj, field)
					}
					continue
				}
				obj[k] = v
			}
			if floor, sent := body["minimumAmount"]; sent {
				obj["minimumAmount"] = storedFloor(floor)
			}
			if filter, sent := body["filter"]; sent && filter != nil {
				obj["filter"] = storedFilter(costAlertDocuments, filter)
			}
			obj["updatedAt"] = "2026-10-05T11:00:00Z"
		},
	})
	translatedFilters(api, costAlertDocuments)
	for action, status := range map[string]string{"pause": "paused", "resume": "active"} {
		api.Handle("POST "+path+"/{id}/"+action, func(r fakeapi.Request) fakeapi.Response {
			if _, ok := api.Get(path, r.PathValue("id")); !ok {
				return fakeapi.NotFound()
			}
			api.Update(path, r.PathValue("id"), func(obj map[string]any) { obj["status"] = status })
			return fakeapi.NoContent()
		})
	}
}

// storedFloor is the minimum amount as the API keeps it: at eight decimals, and a zero as none.
func storedFloor(sent any) any {
	text, _ := sent.(string)
	if text == "" || costfluent.Decimal(text).Equal("0") {
		return nil
	}
	return eightDecimals(text)
}

func costAlertOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/cost-alerts")
	if len(ids) != 1 {
		t.Fatalf("cost alerts = %v, want one", ids)
	}
	return ids[0]
}

// costAlertStoredAs checks the API holds the alert's filter as want.
func costAlertStoredAs(t *testing.T, api *fakeapi.Server, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		stored, _ := api.Get("/v1/cost-alerts", costAlertOnlyID(t, api))
		if stored["filter"] != want {
			return fmt.Errorf("stored filter = %v, want %s", stored["filter"], want)
		}
		return nil
	}
}

func costAlertConfig(api *fakeapi.Server, name string, value float64, attrs string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_alert" "a" {
  workspace_id    = %q
  name            = %q
  threshold_type  = "percentageIncrease"
  threshold_value = %v
%s}
`, testWorkspace, name, value, attrs)
}

func TestCostAlertLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	scope := fmt.Sprintf(`  comparison_period = "previousWeek"
  provider_ids      = [%q]
  app_ids           = [%q]
  filter            = %q
`, costAlertProvider, costAlertApp, costAlertQuery)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costAlertConfig(api, "EC2 spike", 20, scope+"  is_paused = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_cost_alert.a", "id", regexp.MustCompile(`^cal_.{22}$`)),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "threshold_value", "20"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "comparison_period", "previousWeek"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "provider_ids.0", costAlertProvider),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "app_ids.0", costAlertApp),
					// The API answers with the filter's JSON document; the configured CQL means the same.
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "filter", costAlertQuery),
					costAlertStoredAs(t, api, costAlertStored),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "evaluation_frequency_minutes", "60"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "status", "paused"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "is_paused", "true"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "last_evaluated_at", "2026-10-01T10:00:00Z"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "created_at", "2026-10-01T09:00:00Z"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-alerts")
						switch {
						case create.Body["workspaceId"] != testWorkspace, create.Body["name"] != "EC2 spike",
							create.Body["thresholdType"] != "percentageIncrease", create.Body["thresholdValue"] != "20",
							create.Body["comparisonPeriod"] != "previousWeek", create.Body["filter"] != costAlertQuery,
							fmt.Sprint(create.Body["providerIds"]) != "["+costAlertProvider+"]",
							fmt.Sprint(create.Body["appIds"]) != "["+costAlertApp+"]":
							return fmt.Errorf("create body = %s", create.Raw)
						}
						if _, sent := create.Body["evaluationFrequencyMinutes"]; sent {
							return fmt.Errorf("create sent an unset evaluationFrequencyMinutes: %s", create.Raw)
						}
						pause := api.LastRequest(http.MethodPost, "/v1/cost-alerts/"+costAlertOnlyID(t, api)+"/pause")
						if pause.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("pause query = %v", pause.Query)
						}
						return nil
					},
				),
			},
			{
				Config: costAlertConfig(api, "EC2 surge", 35, scope+"  evaluation_frequency_minutes = 30\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "name", "EC2 surge"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "evaluation_frequency_minutes", "30"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "status", "active"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "is_paused", "false"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "updated_at", "2026-10-05T11:00:00Z"),
					func(*terraform.State) error {
						id := costAlertOnlyID(t, api)
						update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+id)
						want := map[string]any{"name": "EC2 surge", "thresholdValue": "35", "evaluationFrequencyMinutes": float64(30)}
						if len(update.Body) != len(want) {
							return fmt.Errorf("update body = %s, want only %v", update.Raw, want)
						}
						for k, v := range want {
							if update.Body[k] != v {
								return fmt.Errorf("update %s = %v, want %v", k, update.Body[k], v)
							}
						}
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						if n := len(api.Requests(http.MethodPost, "/v1/cost-alerts/"+id+"/resume")); n != 1 {
							return fmt.Errorf("resume sent %d times, want once", n)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_cost_alert.a",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + costAlertOnlyID(t, api), nil
				},
				// An import has no configured CQL to keep, so it reads the filter as the stored JSON.
				ImportStateVerifyIgnore: []string{"filter"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["filter"]; got != costAlertStored {
						return fmt.Errorf("imported filter = %q, want the stored JSON", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/cost-alerts"); len(ids) != 0 {
				return fmt.Errorf("cost alerts left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// The floor is sent on create, changed in place, and removed with clearMinimumAmount; a configured
// zero is no floor and no drift.
func TestCostAlertMinimumAmountIsSetChangedAndRemovedInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	period := "  comparison_period = \"previousWeek\"\n"
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costAlertConfig(api, "Spike", 20, period+"  minimum_amount = \"50\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "minimum_amount", "50"),
					func(*terraform.State) error {
						id = costAlertOnlyID(t, api)
						if got := api.LastRequest(http.MethodPost, "/v1/cost-alerts").Body["minimumAmount"]; got != "50" {
							return fmt.Errorf("create minimumAmount = %v, want 50", got)
						}
						return nil
					},
				),
			},
			{
				Config: costAlertConfig(api, "Spike", 20, period+"  minimum_amount = \"75.5\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "minimum_amount", "75.5"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+id)
						if len(update.Body) != 1 || update.Body["minimumAmount"] != "75.5" {
							return fmt.Errorf("update body = %s, want only minimumAmount 75.5", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: costAlertConfig(api, "Spike", 20, period),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.a", "minimum_amount"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+id)
						if len(update.Body) != 1 || update.Body["clearMinimumAmount"] != true {
							return fmt.Errorf("removal body = %s, want only clearMinimumAmount", update.Raw)
						}
						if obj, _ := api.Get("/v1/cost-alerts", id); obj["minimumAmount"] != nil {
							return fmt.Errorf("stored floor = %v after removal", obj["minimumAmount"])
						}
						return nil
					},
				),
			},
			{
				Config: costAlertConfig(api, "Spike", 20, period+"  minimum_amount = \"0\"\n"),
				Check:  resource.TestCheckResourceAttr("costfluent_cost_alert.a", "minimum_amount", "0"),
			},
			{
				Config:   costAlertConfig(api, "Spike", 20, period+"  minimum_amount = \"0\"\n"),
				PlanOnly: true,
			},
		},
	})
}

// Removing the comparison period or the filter clears it on the same alert, with its clear flag.
func TestCostAlertRemovingTheComparisonPeriodAndFilterClearsThemInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	var id string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costAlertConfig(api, "EC2 spike", 20, fmt.Sprintf("  comparison_period = \"previousDay\"\n  filter = %q\n", costAlertAWSQuery)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "filter", costAlertAWSQuery),
					func(*terraform.State) error { id = costAlertOnlyID(t, api); return nil },
				),
			},
			{
				Config: costAlertConfig(api, "EC2 spike", 20, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.a", "comparison_period"),
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.a", "filter"),
					func(*terraform.State) error {
						if got := costAlertOnlyID(t, api); got != id {
							return fmt.Errorf("alert id = %s, want the same %s", got, id)
						}
						update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+id)
						if update.Body["clearComparisonPeriod"] != true || update.Body["clearFilter"] != true {
							return fmt.Errorf("update body = %s, want both clear flags", update.Raw)
						}
						for _, absent := range []string{"comparisonPeriod", "filter"} {
							if _, sent := update.Body[absent]; sent {
								return fmt.Errorf("update sent %s beside its clear flag: %s", absent, update.Raw)
							}
						}
						return nil
					},
				),
			},
			{Config: costAlertConfig(api, "EC2 spike", 20, ""), PlanOnly: true},
		},
	})
}

// Changing, emptying or removing provider_ids or app_ids updates the same alert: an empty or
// removed list is sent as [], a list left as it was is not sent at all.
func TestCostAlertListsAreChangedEmptiedAndRemovedInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	const otherProvider, otherApp = "prv_0000000000000000000043", "app_0000000000000000000008"
	lists := func(attrs string) string { return costAlertConfig(api, "Spike", 20, attrs) }
	var id string
	inPlace := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate),
	}}
	sent := func(want string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if got := costAlertOnlyID(t, api); got != id {
				return fmt.Errorf("alert id = %s, want the same %s", got, id)
			}
			if n := len(api.Requests(http.MethodDelete, "/v1/cost-alerts/"+id)); n != 0 {
				return fmt.Errorf("alert deleted %d times, want an update in place", n)
			}
			if update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+id); string(update.Raw) != want {
				return fmt.Errorf("update body = %s, want %s", update.Raw, want)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: lists(fmt.Sprintf("  provider_ids = [%q]\n  app_ids = [%q]\n", costAlertProvider, costAlertApp)),
				Check:  func(*terraform.State) error { id = costAlertOnlyID(t, api); return nil },
			},
			{
				Config:           lists(fmt.Sprintf("  provider_ids = [%q]\n  app_ids = [%q]\n", otherProvider, costAlertApp)),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "provider_ids.0", otherProvider),
					sent(`{"providerIds":["`+otherProvider+`"]}`),
				),
			},
			{
				Config:           lists(fmt.Sprintf("  provider_ids = [%q]\n  app_ids = []\n", otherProvider)),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "app_ids.#", "0"),
					sent(`{"appIds":[]}`),
				),
			},
			{
				Config:           lists(fmt.Sprintf("  app_ids = [%q]\n", otherApp)),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.a", "provider_ids.#"),
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "app_ids.0", otherApp),
					sent(`{"providerIds":[],"appIds":["`+otherApp+`"]}`),
				),
			},
			{
				Config:           lists(""),
				ConfigPlanChecks: inPlace,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_cost_alert.a", "app_ids.#"),
					sent(`{"appIds":[]}`),
				),
			},
			{Config: lists(""), PlanOnly: true},
		},
	})
}

func TestCostAlertDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: costAlertConfig(api, "EC2 spike", 20, "")},
			{
				PreConfig:          func() { api.Remove("/v1/cost-alerts", costAlertOnlyID(t, api)) },
				Config:             costAlertConfig(api, "EC2 spike", 20, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestCostAlertRejectsInvalidConfigurationAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	for name, tc := range map[string]struct {
		attrs string
		want  string
	}{
		"threshold type outside the enum": {`threshold_type = "relative"`, `value must be one of`},
		"comparison period outside the enum": {`threshold_type = "absolute"
  comparison_period = "lastYear"`, `value must be one of`},
		"evaluation more often than every 15 minutes": {`threshold_type = "absolute"
  evaluation_frequency_minutes = 5`, `must be between 15 and 1440`},
		// An empty filter is no filter to the API, which would answer none and fail the apply as an
		// inconsistent result; leaving filter out is how an alert has none.
		"empty filter": {`threshold_type = "absolute"
  filter = ""`, `string length must be at least 1`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_alert" "a" {
  name            = "a"
  threshold_value = 1
  %s
}
`, tc.attrs),
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}
}

func TestCostAlertAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	// As the API's command validator answers: a detail that names nothing, beside the field it refused.
	refused := fakeapi.Problem(http.StatusBadRequest, "validation_failed", "One or more validation errors occurred")
	refused.Body.(map[string]any)["errors"] = []any{map[string]any{
		"name": "comparisonPeriod", "reason": "'comparison Period' must not be empty.",
	}}
	api.FailNext(http.MethodPost, "/v1/cost-alerts", refused)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      costAlertConfig(api, "EC2 spike", 20, ""),
				ExpectError: regexp.MustCompile(`Failed to create cost alert(.|\n)*comparisonPeriod:\s+'comparison\s+Period'\s+must\s+not\s+be\s+empty`),
			},
			{Config: costAlertConfig(api, "EC2 spike", 20, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/cost-alerts/"+costAlertOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "Unknown app"))
				},
				Config:      costAlertConfig(api, "EC2 surge", 20, ""),
				ExpectError: regexp.MustCompile(`Failed to update cost alert(.|\n)*Unknown app`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPost, "/v1/cost-alerts/"+costAlertOnlyID(t, api)+"/pause",
						fakeapi.Problem(http.StatusConflict, "Conflict", "Alert is already paused"))
				},
				Config:      costAlertConfig(api, "EC2 spike", 20, "  is_paused = true\n"),
				ExpectError: regexp.MustCompile(`Failed to change cost alert pause state(.|\n)*Alert is already paused`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/cost-alerts/"+costAlertOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "Alerts are not in this plan"))
				},
				Config:      costAlertConfig(api, "EC2 spike", 20, ""),
				ExpectError: regexp.MustCompile(`Failed to read cost alert(.|\n)*Alerts are not in this plan`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, "/v1/cost-alerts/"+costAlertOnlyID(t, api),
						fakeapi.Problem(http.StatusConflict, "Conflict", "Alert is evaluating"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete cost alert(.|\n)*Alert is evaluating`),
			},
		},
	})
}

// The alert exists once the create returns, so a failed pause leaves it in state, tainted: the next
// apply replaces it rather than creating a second alert beside one no configuration manages.
func TestCostAlertPauseFailureAfterCreateIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	// The fake numbers IDs from 1, so the alert this test creates is the first.
	first := "cal_" + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodPost, "/v1/cost-alerts/"+first+"/pause",
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "Pausing needs the editor role"))
	config := costAlertConfig(api, "EC2 spike", 20, "  is_paused = true\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/cost-alerts"); len(ids) != 0 {
				return fmt.Errorf("cost alerts left after destroy: %v", ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Failed to pause cost alert(.|\n)*Pausing needs the editor role`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if ids := api.IDs("/v1/cost-alerts"); len(ids) != 1 || ids[0] == first {
						return fmt.Errorf("cost alerts = %v, want only the replacement of %s", ids, first)
					}
					return nil
				},
			},
		},
	})
}

func TestCostAlertCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/cost-alerts", "cal_", "costfluent_cost_alert.a",
		costAlertConfig(api, "EC2 spike", 20, ""), "Failed to create cost alert")
}

// An update changing only the scale of the threshold has nothing to send, so the alert is read
// back rather than sent an empty PUT; a pause alone is sent as the pause, carrying the ETag of the
// last read, with no PUT before it.
func TestCostAlertUpdateWithNothingToSendReadsBackAndAPauseCarriesTheVersion(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	config := func(threshold, attrs string) string {
		return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_alert" "a" {
  workspace_id    = %q
  name            = "Spend"
  threshold_type  = "percentageIncrease"
  threshold_value = %q
%s}
`, testWorkspace, threshold, attrs)
	}
	noPut := func(*terraform.State) error {
		if puts := api.Requests(http.MethodPut, "/v1/cost-alerts/"+costAlertOnlyID(t, api)); len(puts) != 0 {
			return fmt.Errorf("sent %d updates, want none", len(puts))
		}
		return nil
	}
	var readTag string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config("10", "")},
			{
				Config: config("10.00", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					noPut,
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "threshold_value", "10.00"),
				),
			},
			{
				PreConfig: func() { readTag = api.ETag("/v1/cost-alerts", costAlertOnlyID(t, api)) },
				Config:    config("10.00", "  is_paused = true\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					noPut,
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "status", "paused"),
					func(*terraform.State) error {
						pause := api.LastRequest(http.MethodPost, "/v1/cost-alerts/"+costAlertOnlyID(t, api)+"/pause")
						if got := pause.Header.Get("If-Match"); got == "" || got != readTag {
							return fmt.Errorf("pause sent If-Match %q, want the ETag of the last read %q", got, readTag)
						}
						return nil
					},
				),
			},
		},
	})
}

// A filter edited outside Terraform no longer means the configured CQL, so a refresh takes the
// API's document into state and the plan shows an update, which writes the configuration back.
func TestCostAlertFilterChangedOutsideTerraformShowsInStateAndPlansAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	config := costAlertConfig(api, "EC2 spike", 20, fmt.Sprintf("  filter = %q\n", costAlertQuery))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.Update("/v1/cost-alerts", costAlertOnlyID(t, api), func(obj map[string]any) {
						obj["filter"] = costAlertAWSStored
					})
				},
				RefreshState:       true,
				Check:              resource.TestCheckResourceAttr("costfluent_cost_alert.a", "filter", costAlertAWSStored),
				ExpectNonEmptyPlan: true,
				RefreshPlanChecks: resource.RefreshPlanChecks{PostRefresh: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_cost_alert.a", plancheck.ResourceActionUpdate),
				}},
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "filter", costAlertQuery),
					costAlertStoredAs(t, api, costAlertStored),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/cost-alerts/"+costAlertOnlyID(t, api))
						if string(update.Raw) != `{"filter":"`+costAlertQuery+`"}` {
							return fmt.Errorf("update body = %s, want only the configured filter", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// A filter written as the JSON document is kept as written while it means the stored one.
func TestCostAlertJSONFilterInAnotherKeyOrderPlansNoChange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	reordered := `[{"values":["Amazon EC2"],"operator":"In","field":"ServiceName"}]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: costAlertConfig(api, "EC2 spike", 20, fmt.Sprintf("  filter = %q\n", reordered)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_alert.a", "filter", reordered),
					costAlertStoredAs(t, api, costAlertStored),
				),
			},
		},
	})
}

func TestCostAlertCreateWhoseFilterTranslationFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeCostAlerts(api)
	createWhoseFilterTranslationFailsIsTainted(t, api, "/v1/cost-alerts", "cal_", "costfluent_cost_alert.a",
		costAlertConfig(api, "EC2 spike", 20, fmt.Sprintf("  filter = %q\n", costAlertQuery)),
		"Failed to compare the cost alert filter")
}
