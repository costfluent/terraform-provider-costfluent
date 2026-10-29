package resources_test

import (
	"cmp"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// fakeBudgets serves /v1/budgets with the BudgetResponse shape, which create also returns. The API
// returns the actual alerts lowest threshold first, then the forecast ones, a basis left out being
// actual; sent alerts replace them in place, a threshold kept at its percent and basis keeping
// whether it fired. It returns the segment and report but not the workspace. One
// amount becomes two monthly periods from the start date, which defaults to 2026-10-01; periods
// sent are stored as sent, at the API's scale. The measure defaults to cost, and a cost budget
// answers a null unit. Nothing is left uncounted and the whole period is measured: no unconverted
// spend and a null history floor.
func fakeBudgets(api *fakeapi.Server) {
	api.Collection("/v1/budgets", fakeapi.Collection{
		IDPrefix:  "bud_",
		Scope:     fakeapi.CreatedInWorkspace,
		Versioned: true,
		Create: func(body map[string]any) map[string]any {
			alerts := []any{}
			if sent, ok := body["alerts"].([]any); ok {
				alerts = replaceAlerts(nil, sent, channels(body["appIds"]))
			}
			start, _ := body["startDate"].(string)
			if start == "" {
				start = "2026-10-01"
			}
			measure, _ := body["measure"].(string)
			if measure == "" {
				measure = "cost"
			}
			obj := map[string]any{
				"name":              body["name"],
				"currency":          body["currency"],
				"measure":           measure,
				"usageUnit":         body["usageUnit"],
				"startDate":         start,
				"intervalCount":     body["intervalCount"],
				"intervalUnit":      body["intervalUnit"],
				"status":            "active",
				"state":             "onTrack",
				"currentSpend":      "125.50000000",
				"percentUsed":       25.1,
				"spendAvailability": "computed",
				"unconverted":       []any{},
				"historyFloor":      nil,
				"alerts":            alerts,
				"segmentId":         body["segmentId"],
				"costReportId":      body["costReportId"],
				"createdAt":         "2026-10-01T00:00:00Z",
			}
			if sent, ok := body["periods"]; ok {
				setPeriods(obj, sent)
			} else {
				obj["periods"] = []any{
					map[string]any{"startDate": start, "endDate": "2026-10-31", "amount": eightDecimals(body["amount"])},
					map[string]any{"startDate": "2026-11-01", "endDate": "2026-11-30", "amount": eightDecimals(body["amount"])},
				}
				obj["amount"] = eightDecimals(body["amount"])
			}
			return obj
		},
		Apply: func(obj, body map[string]any) {
			for _, k := range []string{"name", "usageUnit"} {
				if v, ok := body[k]; ok {
					obj[k] = v
				}
			}
			// Naming a scope replaces the other; a clear flag returns the budget to the workspace.
			switch {
			case body["segmentId"] != nil:
				obj["segmentId"], obj["costReportId"] = body["segmentId"], nil
			case body["costReportId"] != nil:
				obj["costReportId"], obj["segmentId"] = body["costReportId"], nil
			}
			if body["clearSegment"] == true {
				obj["segmentId"] = nil
			}
			if body["clearCostReport"] == true {
				obj["costReportId"] = nil
			}
			if v, ok := body["amount"]; ok {
				obj["amount"] = eightDecimals(v)
				for _, p := range obj["periods"].([]any) {
					p.(map[string]any)["amount"] = eightDecimals(v)
				}
			}
			if sent, ok := body["periods"]; ok {
				setPeriods(obj, sent)
			}
			if sent, ok := body["alerts"].([]any); ok {
				current := channels(nil)
				if prior, _ := obj["alerts"].([]any); len(prior) > 0 {
					current = prior[0].(map[string]any)["appIds"].([]any)
				}
				obj["alerts"] = replaceAlerts(obj["alerts"].([]any), sent, current)
			}
			if ids, ok := body["appIds"]; ok {
				for _, a := range obj["alerts"].([]any) {
					a.(map[string]any)["appIds"] = channels(ids)
				}
			}
			obj["updatedAt"] = "2026-10-04T12:00:00Z"
		},
	})
}

// replaceAlerts is the sent thresholds in the API's order: a basis left out is actual, one kept at
// the same percent and basis keeps whether it fired, and a new one starts armed on appIDs.
func replaceAlerts(prior, sent, appIDs []any) []any {
	alerts := make([]any, 0, len(sent))
	for _, a := range sent {
		a := a.(map[string]any)
		basis, _ := a["basis"].(string)
		if basis == "" {
			basis = "actual"
		}
		alert := map[string]any{"thresholdPercent": a["thresholdPercent"], "basis": basis, "appIds": appIDs, "isTriggered": false}
		for _, p := range prior {
			if p := p.(map[string]any); p["thresholdPercent"] == a["thresholdPercent"] && p["basis"] == basis {
				alert["isTriggered"] = p["isTriggered"]
			}
		}
		alerts = append(alerts, alert)
	}
	slices.SortFunc(alerts, func(x, y any) int {
		a, b := x.(map[string]any), y.(map[string]any)
		return cmp.Or(cmp.Compare(a["basis"].(string), b["basis"].(string)),
			cmp.Compare(a["thresholdPercent"].(float64), b["thresholdPercent"].(float64)))
	})
	return alerts
}

// setPeriods stores sent periods at the API's scale, with the amount they share or none.
func setPeriods(obj map[string]any, sent any) {
	periods := []any{}
	amounts := map[any]bool{}
	for _, p := range sent.([]any) {
		p := p.(map[string]any)
		amount := eightDecimals(p["amount"])
		amounts[amount] = true
		periods = append(periods, map[string]any{"startDate": p["startDate"], "endDate": p["endDate"], "amount": amount})
	}
	obj["periods"] = periods
	obj["amount"] = nil
	if len(amounts) == 1 {
		obj["amount"] = periods[0].(map[string]any)["amount"]
	}
}

// eightDecimals is a sent amount as the API answers it, at eight decimals, so a test sees the
// provider keep the configured scale rather than drift to the API's.
func eightDecimals(sent any) any {
	text, _ := sent.(string)
	canonical, err := costfluent.Decimal(text).Canonical()
	if err != nil {
		return sent
	}
	return string(canonical)
}

// channels is the per-threshold appIds the API answers: every threshold names the budget's channels.
func channels(sent any) []any {
	if ids, ok := sent.([]any); ok {
		return ids
	}
	return []any{}
}

func budgetOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/budgets")
	if len(ids) != 1 {
		t.Fatalf("budgets = %v, want one", ids)
	}
	return ids[0]
}

func budgetConfig(api *fakeapi.Server, name, amount, attrs string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_budget" "b" {
  workspace_id = %q
  name         = %q
  amount       = %q
  currency     = "EUR"
%s}
`, testWorkspace, name, amount, attrs)
}

func TestBudgetLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	alerts := "  alerts = [{ threshold_percent = 75 }, { threshold_percent = 90 }]\n"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetConfig(api, "Platform", "500", alerts),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_budget.b", "id", regexp.MustCompile(`^bud_.{22}$`)),
					resource.TestCheckResourceAttr("costfluent_budget.b", "amount", "500"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "current_spend", "125.50000000"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "percent_used", "25.1"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "spend_availability", "computed"),
					// An empty list rather than null, so length() works on it.
					resource.TestCheckResourceAttrSet("costfluent_budget.b", "unconverted.#"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "unconverted.#", "0"),
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "history_floor"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "status", "active"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "state", "onTrack"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "start_date", "2026-10-01"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "interval_unit", "month"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "measure", "cost"),
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "usage_unit"),
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "periods.#"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "created_at", "2026-10-01T00:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "updated_at"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.0.threshold_percent", "75"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.1.threshold_percent", "90"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						sent, _ := create.Body["alerts"].([]any)
						switch {
						case create.Body["workspaceId"] != testWorkspace, create.Body["name"] != "Platform",
							create.Body["amount"] != "500", create.Body["currency"] != "EUR",
							create.Body["intervalUnit"] != "month", create.Body["intervalCount"] != float64(1),
							create.Body["measure"] != "cost":
							return fmt.Errorf("create body = %s", create.Raw)
						case len(sent) != 2:
							return fmt.Errorf("create alerts = %v", sent)
						}
						for _, unset := range []string{"segmentId", "usageUnit"} {
							if _, ok := create.Body[unset]; ok {
								return fmt.Errorf("create sent an unset %s: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				Config: budgetConfig(api, "Platform EU", "750", alerts+"  segment_id = \"seg_0000000000000000000009\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "name", "Platform EU"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "amount", "750"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "updated_at", "2026-10-04T12:00:00Z"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api))
						want := map[string]any{"name": "Platform EU", "amount": "750", "segmentId": "seg_0000000000000000000009"}
						if len(update.Body) != len(want) {
							return fmt.Errorf("update body = %s, want %v", update.Raw, want)
						}
						for k, v := range want {
							if update.Body[k] != v {
								return fmt.Errorf("update %s = %v, want %v", k, update.Body[k], v)
							}
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_budget.b",
				ImportState:       true,
				ImportStateVerify: true,
				// BudgetResponse carries neither the workspace nor the segment.
				ImportStateVerifyIgnore: []string{"workspace_id", "segment_id", "cost_report_id"},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/budgets"); len(ids) != 0 {
				return fmt.Errorf("budgets left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func budgetPeriodsConfig(api *fakeapi.Server, unit, octoberAmount string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_budget" "b" {
  workspace_id  = %q
  name          = "Ramp"
  currency      = "EUR"
  start_date    = "2026-10-01"
  interval_unit = %q
  periods = [
    { start_date = "2026-10-01", end_date = "2026-10-31", amount = %q },
    { start_date = "2026-11-01", end_date = "2026-11-30", amount = "600.0" },
  ]
}
`, testWorkspace, unit, octoberAmount)
}

// Periods are sent as configured and kept at their configured scale; an edit sends the periods
// alone, the same numbers at another scale send nothing, and a new cadence recreates the budget.
func TestBudgetWithPeriodsSendsThemAndKeepsTheirScale(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	var firstID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetPeriodsConfig(api, "month", "500"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "amount"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "periods.#", "2"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "periods.1.amount", "600.0"),
					func(*terraform.State) error {
						firstID = budgetOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						periods, _ := create.Body["periods"].([]any)
						if _, sent := create.Body["amount"]; sent || len(periods) != 2 ||
							create.Body["startDate"] != "2026-10-01" ||
							periods[1].(map[string]any)["amount"] != "600.0" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: budgetPeriodsConfig(api, "month", "550"),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api))
					periods, _ := update.Body["periods"].([]any)
					if len(update.Body) != 1 || len(periods) != 2 || periods[0].(map[string]any)["amount"] != "550" {
						return fmt.Errorf("update body = %s, want the periods alone", update.Raw)
					}
					return nil
				},
			},
			{
				Config: budgetPeriodsConfig(api, "month", "550.00"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "periods.0.amount", "550.00"),
					func(*terraform.State) error {
						if puts := api.Requests(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api)); len(puts) != 1 {
							return fmt.Errorf("the same periods at another scale sent %d more updates", len(puts)-1)
						}
						return nil
					},
				),
			},
			{
				Config: budgetPeriodsConfig(api, "week", "550"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if id := budgetOnlyID(t, api); id == firstID {
						return fmt.Errorf("a new cadence kept budget %s", id)
					}
					return nil
				},
			},
		},
	})
}

// app_ids are sent on create, replace the channels when changed, and removing them clears the
// channels in place rather than recreating the budget.
func TestBudgetChannelsAreSetReplacedAndCleared(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	lastUpdate := func() fakeapi.Request {
		return api.LastRequest(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api))
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetConfig(api, "Platform", "500", "  app_ids = [\"app_email\"]\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "app_ids.#", "1"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "app_ids.0", "app_email"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						if ids, ok := create.Body["appIds"].([]any); !ok || len(ids) != 1 || ids[0] != "app_email" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: budgetConfig(api, "Platform", "500", "  app_ids = [\"app_slack\"]\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "app_ids.0", "app_slack"),
					func(*terraform.State) error {
						update := lastUpdate()
						if ids, ok := update.Body["appIds"].([]any); !ok || len(update.Body) != 1 || len(ids) != 1 || ids[0] != "app_slack" {
							return fmt.Errorf("update body = %s, want only appIds [app_slack]", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: budgetConfig(api, "Platform", "500", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "app_ids.#"),
					func(*terraform.State) error {
						update := lastUpdate()
						if ids, ok := update.Body["appIds"].([]any); !ok || len(ids) != 0 {
							return fmt.Errorf("update body = %s, want appIds []", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:            "costfluent_budget.b",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"workspace_id", "segment_id", "alerts"},
			},
		},
	})
}

// Without alerts in the configuration, the budget is created with the 50, 80 and 100 percent
// defaults.
func TestBudgetDefaultAlertsAreSent(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetConfig(api, "Platform", "500", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.#", "3"),
					func(*terraform.State) error {
						sent, _ := api.LastRequest(http.MethodPost, "/v1/budgets").Body["alerts"].([]any)
						var got []any
						for _, a := range sent {
							got = append(got, a.(map[string]any)["thresholdPercent"])
						}
						if fmt.Sprint(got) != "[50 80 100]" {
							return fmt.Errorf("default alerts sent = %v", got)
						}
						return nil
					},
				),
			},
			{Config: budgetConfig(api, "Platform", "500", ""), PlanOnly: true},
		},
	})
}

// Changing the thresholds updates the same budget in place, sending the whole new list with each
// basis, and a forecast threshold reads back, plans nothing more and imports as configured.
func TestBudgetAlertsChangeInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	var id string
	spendOnly := budgetConfig(api, "Platform", "500", "  alerts = [{ threshold_percent = 80 }, { threshold_percent = 100 }]\n")
	withForecast := budgetConfig(api, "Platform", "500", "  alerts = [\n    { threshold_percent = 80 },\n"+
		"    { threshold_percent = 90, basis = \"forecast\" },\n    { threshold_percent = 100, basis = \"forecast\" },\n  ]\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: spendOnly,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.0.basis", "actual"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.1.basis", "actual"),
					func(*terraform.State) error {
						id = budgetOnlyID(t, api)
						return nil
					},
				),
			},
			{
				Config: withForecast,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.#", "3"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.0.threshold_percent", "80"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.0.basis", "actual"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.1.threshold_percent", "90"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.1.basis", "forecast"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.2.basis", "forecast"),
					func(*terraform.State) error {
						if got := budgetOnlyID(t, api); got != id {
							return fmt.Errorf("an alerts change replaced budget %s with %s", id, got)
						}
						update := api.LastRequest(http.MethodPut, "/v1/budgets/"+id)
						sent, _ := update.Body["alerts"].([]any)
						if len(update.Body) != 1 || len(sent) != 3 {
							return fmt.Errorf("update body = %s, want the three thresholds alone", update.Raw)
						}
						var got []string
						for _, a := range sent {
							a := a.(map[string]any)
							got = append(got, fmt.Sprint(a["basis"], " ", a["thresholdPercent"]))
						}
						if want := "[actual 80 forecast 90 forecast 100]"; fmt.Sprint(got) != want {
							return fmt.Errorf("update alerts = %v, want %s", got, want)
						}
						return nil
					},
				),
			},
			{Config: withForecast, PlanOnly: true},
			{
				ResourceName:      "costfluent_budget.b",
				ImportState:       true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return testWorkspace + ":" + id, nil },
				ImportStateVerify: true,
			},
			// Back to the defaults, still in place.
			{
				Config: budgetConfig(api, "Platform", "500", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.#", "3"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.2.threshold_percent", "100"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "alerts.2.basis", "actual"),
					func(*terraform.State) error {
						if got := budgetOnlyID(t, api); got != id {
							return fmt.Errorf("returning to the default alerts replaced budget %s with %s", id, got)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestBudgetOnAReportSendsIt(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)

	report := "  cost_report_id = \"crp_0000000000000000000001\"\n"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetConfig(api, "Platform", "500", report),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "cost_report_id", "crp_0000000000000000000001"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						if create.Body["costReportId"] != "crp_0000000000000000000001" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						if _, ok := create.Body["segmentId"]; ok {
							return fmt.Errorf("create sent an unset segmentId: %s", create.Raw)
						}
						return nil
					},
				),
			},
			{Config: budgetConfig(api, "Platform", "500", report), PlanOnly: true},
		},
	})
}

func usageBudgetAttrs(unit string) string {
	return fmt.Sprintf("  measure = \"usage\"\n  usage_unit = %q\n  cost_report_id = \"crp_0000000000000000000001\"\n", unit)
}

// A usage budget sends its measure and unit beside its report and reads them back with the
// currency it still carries; its unit changes on the same budget, sending the unit alone; it
// imports with nothing to change; and turning it back into a cost budget recreates it, because the
// API fixes the measure at creation.
func TestBudgetUsageMeasure(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	var id string
	importID := func(*terraform.State) (string, error) { return testWorkspace + ":" + budgetOnlyID(t, api), nil }
	tokens := budgetConfig(api, "AI tokens", "5000000", usageBudgetAttrs("Tokens"))
	gpuHours := budgetConfig(api, "AI tokens", "5000000", usageBudgetAttrs("GPU-Hours"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: tokens,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "measure", "usage"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "usage_unit", "Tokens"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "amount", "5000000"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "currency", "EUR"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "cost_report_id", "crp_0000000000000000000001"),
					func(*terraform.State) error {
						id = budgetOnlyID(t, api)
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						if create.Body["measure"] != "usage" || create.Body["usageUnit"] != "Tokens" ||
							create.Body["costReportId"] != "crp_0000000000000000000001" || create.Body["currency"] != "EUR" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
			{Config: tokens, PlanOnly: true},
			{
				Config: gpuHours,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "usage_unit", "GPU-Hours"),
					func(*terraform.State) error {
						if got := budgetOnlyID(t, api); got != id {
							return fmt.Errorf("budget id = %s, want the same %s", got, id)
						}
						update := api.LastRequest(http.MethodPut, "/v1/budgets/"+id)
						if len(update.Body) != 1 || update.Body["usageUnit"] != "GPU-Hours" {
							return fmt.Errorf("update body = %s, want the unit alone", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config:            gpuHours,
				ResourceName:      "costfluent_budget.b",
				ImportState:       true,
				ImportStateKind:   resource.ImportBlockWithID,
				ImportStateIdFunc: importID,
				ImportPlanChecks: resource.ImportPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:      "costfluent_budget.b",
				ImportState:       true,
				ImportStateIdFunc: importID,
				ImportStateVerify: true,
			},
			{
				Config: budgetConfig(api, "AI tokens", "5000000", "  cost_report_id = \"crp_0000000000000000000001\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "measure", "cost"),
					resource.TestCheckNoResourceAttr("costfluent_budget.b", "usage_unit"),
					func(*terraform.State) error {
						if got := budgetOnlyID(t, api); got == id {
							return fmt.Errorf("budget id = %s, want a new budget", got)
						}
						create := api.LastRequest(http.MethodPost, "/v1/budgets")
						if _, sent := create.Body["usageUnit"]; sent || create.Body["measure"] != "cost" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// A usage budget's report is usually created in the same apply, so its ID is unknown when the
// configuration is validated: that is not a missing report.
func TestBudgetUsageOnAReportKnownOnlyAtApply(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: api.ProviderBlock() + fmt.Sprintf(`
resource "terraform_data" "report" {
  input = "crp_0000000000000000000001"
}

resource "costfluent_budget" "b" {
  workspace_id   = %q
  name           = "AI tokens"
  amount         = "5000000"
  currency       = "EUR"
  measure        = "usage"
  usage_unit     = "Tokens"
  cost_report_id = terraform_data.report.output
}
`, testWorkspace),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("costfluent_budget.b", "measure", "usage"),
				resource.TestCheckResourceAttr("costfluent_budget.b", "cost_report_id", "crp_0000000000000000000001"),
			),
		}},
	})
}

// A budget's scope moves between reports, a segment and the whole workspace in place: each step is
// an update of the same budget, sending the scope it names or the clear flag of the one removed.
func TestBudgetScopeMovesInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	var id string

	steps := []struct {
		attrs string
		want  map[string]any
	}{
		{"  cost_report_id = \"crp_0000000000000000000002\"\n", map[string]any{"costReportId": "crp_0000000000000000000002"}},
		{"  segment_id = \"seg_0000000000000000000009\"\n", map[string]any{"segmentId": "seg_0000000000000000000009"}},
		{"", map[string]any{"clearSegment": true}},
		{"  cost_report_id = \"crp_0000000000000000000001\"\n", map[string]any{"costReportId": "crp_0000000000000000000001"}},
		{"", map[string]any{"clearCostReport": true}},
	}

	testSteps := []resource.TestStep{{
		Config: budgetConfig(api, "Platform", "500", "  cost_report_id = \"crp_0000000000000000000001\"\n"),
		Check:  func(*terraform.State) error { id = budgetOnlyID(t, api); return nil },
	}}
	for _, step := range steps {
		testSteps = append(testSteps, resource.TestStep{
			Config: budgetConfig(api, "Platform", "500", step.attrs),
			ConfigPlanChecks: resource.ConfigPlanChecks{
				PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate),
				},
			},
			Check: func(*terraform.State) error {
				if got := budgetOnlyID(t, api); got != id {
					return fmt.Errorf("budget id = %s, want the same %s", got, id)
				}
				update := api.LastRequest(http.MethodPut, "/v1/budgets/"+id)
				for _, key := range []string{"segmentId", "costReportId", "clearSegment", "clearCostReport"} {
					want, named := step.want[key]
					if got, sent := update.Body[key]; sent != named || got != want {
						return fmt.Errorf("update %s = %v (sent %v), want %v (sent %v): %s", key, got, sent, want, named, update.Raw)
					}
				}
				return nil
			},
		}, resource.TestStep{Config: budgetConfig(api, "Platform", "500", step.attrs), PlanOnly: true})
	}

	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: factories, Steps: testSteps})
}

// An imported budget plans nothing against the configuration it was created from: the import ID
// names its workspace, which the API does not return, and the read takes its segment or report.
func TestBudgetImportPlansNothing(t *testing.T) {
	t.Parallel()
	for name, scope := range map[string]string{
		"segment": "  segment_id = \"seg_0000000000000000000009\"\n",
		"report":  "  cost_report_id = \"crp_0000000000000000000001\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			api := fakeapi.New(t)
			fakeBudgets(api)
			config := budgetConfig(api, "Platform", "500", scope)
			importID := func(*terraform.State) (string, error) {
				return testWorkspace + ":" + budgetOnlyID(t, api), nil
			}

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: config},
					{
						ResourceName:      "costfluent_budget.b",
						ImportState:       true,
						ImportStateKind:   resource.ImportBlockWithID,
						ImportStateIdFunc: importID,
						ImportPlanChecks: resource.ImportPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
					{
						ResourceName:      "costfluent_budget.b",
						ImportState:       true,
						ImportStateIdFunc: importID,
						ImportStateVerify: true,
					},
				},
			})
		})
	}
}

// The API sends percentUsed as null when the spend is unknown, unavailable or outside the plan,
// and before the budget starts; state must hold null then, never a measured 0, and 0 only for a
// measured zero.
func TestBudgetPercentUsedIsNullUnlessTheSpendIsMeasured(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		figures map[string]any
		check   resource.TestCheckFunc
	}{
		"unavailable": {
			map[string]any{"spendAvailability": "unavailable", "state": "unknown", "currentSpend": nil, "percentUsed": nil},
			resource.TestCheckNoResourceAttr("costfluent_budget.b", "percent_used"),
		},
		"outside the plan": {
			map[string]any{"spendAvailability": "outsidePlan", "state": "unknown", "currentSpend": nil, "percentUsed": nil},
			resource.TestCheckNoResourceAttr("costfluent_budget.b", "percent_used"),
		},
		"not started": {
			map[string]any{"spendAvailability": "computed", "state": "notStarted", "currentSpend": "0.00000000", "percentUsed": nil},
			resource.TestCheckNoResourceAttr("costfluent_budget.b", "percent_used"),
		},
		"measured zero": {
			map[string]any{"spendAvailability": "computed", "state": "onTrack", "currentSpend": "0.00000000", "percentUsed": 0},
			resource.TestCheckResourceAttr("costfluent_budget.b", "percent_used", "0"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			fakeBudgets(api)
			config := budgetConfig(api, "Platform", "500", "")

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: config},
					{
						PreConfig: func() {
							api.Update("/v1/budgets", budgetOnlyID(t, api), func(obj map[string]any) {
								maps.Copy(obj, tc.figures)
							})
						},
						Config: config,
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("costfluent_budget.b", "spend_availability", tc.figures["spendAvailability"].(string)),
							tc.check,
						),
					},
				},
			})
		})
	}
}

// A budget names the spend in other currencies it does not count, which state and percent_used
// leave out, exactly as the API sends it, and drops it once the API counts nothing more.
func TestBudgetNamesItsUnconvertedSpend(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	config := budgetConfig(api, "Platform", "500", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.Update("/v1/budgets", budgetOnlyID(t, api), func(obj map[string]any) {
						obj["unconverted"] = []any{map[string]any{"currency": "USD", "amount": "7000.12345678"}}
					})
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "unconverted.#", "1"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "unconverted.0.currency", "USD"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "unconverted.0.amount", "7000.12345678"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "current_spend", "125.50000000"),
				),
			},
			{
				PreConfig: func() {
					api.Update("/v1/budgets", budgetOnlyID(t, api), func(obj map[string]any) {
						obj["unconverted"] = []any{}
					})
				},
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("costfluent_budget.b", "unconverted.#"),
					resource.TestCheckResourceAttr("costfluent_budget.b", "unconverted.#", "0"),
				),
			},
		},
	})
}

// A budget whose period starts before the plan's history names the floor its spend counts from.
func TestBudgetNamesItsHistoryFloor(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	config := budgetConfig(api, "Platform", "500", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.Update("/v1/budgets", budgetOnlyID(t, api), func(obj map[string]any) {
						obj["historyFloor"] = "2026-04-06"
					})
				},
				Config: config,
				Check:  resource.TestCheckResourceAttr("costfluent_budget.b", "history_floor", "2026-04-06"),
			},
		},
	})
}

// The API returns alerts lowest threshold first, so a configuration in another order could never
// match the state. It is refused at plan instead, and an ascending one imports with nothing to
// change.
func TestBudgetImportWithAlertsOutOfOrder(t *testing.T) {
	t.Parallel()
	t.Run("descending is refused at plan", func(t *testing.T) {
		api := fakeapi.New(t)
		fakeBudgets(api)

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: factories,
			Steps: []resource.TestStep{{
				Config:      budgetConfig(api, "Platform", "500", "  alerts = [{ threshold_percent = 100 }, { threshold_percent = 50 }]\n"),
				ExpectError: regexp.MustCompile(`(?s)threshold_percent\s+lowest\s+first.*\[actual\s+50,\s+actual\s+100\]`),
			}},
		})
		if ids := api.IDs("/v1/budgets"); len(ids) != 0 {
			t.Fatalf("budgets created = %v, want none", ids)
		}
	})

	t.Run("ascending imports with an empty plan", func(t *testing.T) {
		api := fakeapi.New(t)
		fakeBudgets(api)
		config := budgetConfig(api, "Platform", "500", "  alerts = [{ threshold_percent = 50 }, { threshold_percent = 100 }]\n")

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: factories,
			Steps: []resource.TestStep{
				{Config: config},
				{
					ResourceName:    "costfluent_budget.b",
					ImportState:     true,
					ImportStateKind: resource.ImportBlockWithID,
					ImportStateIdFunc: func(*terraform.State) (string, error) {
						return testWorkspace + ":" + budgetOnlyID(t, api), nil
					},
					ImportPlanChecks: resource.ImportPlanChecks{
						PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
					},
				},
			},
		})
	})
}

// The budget GET is organization-wide and its response has no workspace, so an import ID naming
// a workspace is checked against that workspace's budget list. A budget of another workspace is
// refused rather than imported under the wrong workspace_id.
func TestBudgetImportRefusesAnotherWorkspace(t *testing.T) {
	t.Parallel()
	const otherWorkspace = "wsp_0000000000000000000002"
	for name, tc := range map[string]struct {
		workspace string
		fail      bool
		want      string
	}{
		"another workspace": {workspace: otherWorkspace, want: `(?s)Budget not in workspace.*bud_.{22}.*` + otherWorkspace},
		"list refused":      {workspace: testWorkspace, fail: true, want: `(?s)Failed to import budget.*Budgets are not in this plan`},
	} {
		t.Run(name, func(t *testing.T) {
			api := fakeapi.New(t)
			fakeBudgets(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: budgetConfig(api, "Platform", "500", "")},
					{
						PreConfig: func() {
							if tc.fail {
								api.FailNext(http.MethodGet, "/v1/budgets",
									fakeapi.Problem(http.StatusForbidden, "Forbidden", "Budgets are not in this plan"))
							}
						},
						ResourceName:    "costfluent_budget.b",
						ImportState:     true,
						ImportStateKind: resource.ImportBlockWithID,
						ImportStateIdFunc: func(*terraform.State) (string, error) {
							return tc.workspace + ":" + budgetOnlyID(t, api), nil
						},
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})

			lists := api.Requests(http.MethodGet, "/v1/budgets")
			if len(lists) == 0 {
				t.Fatal("the import listed no budgets")
			}
			for _, r := range lists {
				if got := r.Query.Get("workspaceId"); got != tc.workspace {
					t.Fatalf("list workspaceId = %q, want the import's %q", got, tc.workspace)
				}
			}
		})
	}
}

// The workspace check pages the list until it finds the budget, and stops there.
func TestBudgetImportFindsTheBudgetInItsWorkspaceList(t *testing.T) {
	t.Parallel()
	seed := func(api *fakeapi.Server, n int) {
		for range n {
			api.PutIn("/v1/budgets", testWorkspace, api.NextID("bud_"), map[string]any{"name": "other"})
		}
	}
	for name, tc := range map[string]struct {
		before, after int
		pages         []string
	}{
		"on the first page": {after: 150, pages: []string{"1"}},
		"on a later page":   {before: 100, pages: []string{"1", "2"}},
	} {
		t.Run(name, func(t *testing.T) {
			api := fakeapi.New(t)
			fakeBudgets(api)
			seed(api, tc.before)
			var id string

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: budgetConfig(api, "Platform", "500", ""),
						Check: func(s *terraform.State) error {
							id = s.RootModule().Resources["costfluent_budget.b"].Primary.ID
							seed(api, tc.after)
							return nil
						},
					},
					{
						ResourceName:    "costfluent_budget.b",
						ImportState:     true,
						ImportStateKind: resource.ImportBlockWithID,
						ImportStateIdFunc: func(*terraform.State) (string, error) {
							return testWorkspace + ":" + id, nil
						},
						ImportPlanChecks: resource.ImportPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
				},
				// The seeded budgets were never Terraform's to destroy.
				CheckDestroy: func(*terraform.State) error {
					if _, ok := api.Get("/v1/budgets", id); ok {
						return fmt.Errorf("budget %s left after destroy", id)
					}
					return nil
				},
			})

			listed := map[string]bool{}
			for _, r := range api.Requests(http.MethodGet, "/v1/budgets") {
				if r.Query.Get("workspaceId") != testWorkspace {
					t.Fatalf("list workspaceId = %q, want %q", r.Query.Get("workspaceId"), testWorkspace)
				}
				listed[r.Query.Get("page")] = true
			}
			if want := tc.pages; len(listed) != len(want) || !listed[want[0]] || !listed[want[len(want)-1]] {
				t.Fatalf("pages listed = %v, want exactly %v", slices.Sorted(maps.Keys(listed)), want)
			}
		})
	}
}

func TestBudgetDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: budgetConfig(api, "Platform", "500", "")},
			{
				PreConfig:          func() { api.Remove("/v1/budgets", budgetOnlyID(t, api)) },
				Config:             budgetConfig(api, "Platform", "500", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestBudgetRejectsInvalidConfigurationAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	for name, tc := range map[string]struct {
		config string
		want   string
	}{
		"workspace without wsp_ prefix": {
			config: `workspace_id = "fld_0000000000000000000001"
  name = "b"
  amount = 1
  currency = "EUR"`,
			want: `Expected token starting with "wsp_"`,
		},
		"currency not three letters": {
			config: `name = "b"
  amount = 1
  currency = "EURO"`,
			want: `string length must be between 3 and 3`,
		},
		"interval unit outside the enum": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  interval_unit = "Quarter"`,
			want: `value must be one of`,
		},
		"interval count of zero": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  interval_count = 0`,
			want: `must be between 1 and 366`,
		},
		"start date not a date": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  start_date = "1 October 2026"`,
			want: `must be a date as YYYY-MM-DD`,
		},
		"neither amount nor periods": {
			config: `name = "b"
  currency = "EUR"`,
			want: `No attribute specified when one \(and only one\) of \[periods\] is required`,
		},
		"amount beside periods": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  periods = [{ start_date = "2026-10-01", end_date = "2026-10-31", amount = "1" }]`,
			want: `(?s)2 attributes specified`,
		},
		"segment and report together": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  segment_id = "seg_0000000000000000000009"
  cost_report_id = "crp_0000000000000000000001"`,
			want: `cannot be specified when`,
		},
		// An empty list is not sent, so the API would apply its default thresholds.
		"empty alerts": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = []`,
			want: `list must contain at least 1 elements`,
		},
		"threshold above 200": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = [{ threshold_percent = 201 }]`,
			want: `must be between 1 and 200`,
		},
		// The API returns alerts lowest first, so no other order matches the state.
		"alerts out of order": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = [{ threshold_percent = 80 }, { threshold_percent = 50 }]`,
			want: `(?s)lowest\s+first.*\[actual\s+50,\s+actual\s+80\]`,
		},
		"repeated threshold": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = [{ threshold_percent = 80 }, { threshold_percent = 80 }]`,
			want: `(?s)lowest\s+first.*\[actual\s+80\]`,
		},
		// The API returns the actual thresholds before the forecast ones.
		"forecast before actual": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = [{ threshold_percent = 100, basis = "forecast" }, { threshold_percent = 80 }]`,
			want: `(?s)lowest\s+first.*\[actual\s+80,\s+forecast\s+100\]`,
		},
		"basis outside the enum": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  alerts = [{ threshold_percent = 80, basis = "Forecast" }]`,
			want: `value must be one of`,
		},
		"forecast threshold on a usage budget": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = "Tokens"
  cost_report_id = "crp_0000000000000000000001"
  alerts = [{ threshold_percent = 80 }, { threshold_percent = 100, basis = "forecast" }]`,
			want: `Unexpected forecast threshold`,
		},
		"measure outside the enum": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "Usage"`,
			want: `value must be one of`,
		},
		"usage without a unit": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  cost_report_id = "crp_0000000000000000000001"`,
			want: `Missing usage_unit`,
		},
		"usage without a report": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = "Tokens"`,
			want: `Missing cost_report_id`,
		},
		"usage on a segment": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = "Tokens"
  segment_id = "seg_0000000000000000000009"`,
			want: `Unexpected segment_id`,
		},
		"unit on a default cost budget": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  usage_unit = "Tokens"`,
			want: `Unexpected usage_unit`,
		},
		"unit on a cost budget": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "cost"
  usage_unit = "Tokens"
  cost_report_id = "crp_0000000000000000000001"`,
			want: `Unexpected usage_unit`,
		},
		"empty unit": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = ""
  cost_report_id = "crp_0000000000000000000001"`,
			want: `character count must be between 1 and 64`,
		},
		"blank unit": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = "   "
  cost_report_id = "crp_0000000000000000000001"`,
			want: `must not be blank`,
		},
		"unit of 65 characters": {
			config: `name = "b"
  amount = 1
  currency = "EUR"
  measure = "usage"
  usage_unit = "` + strings.Repeat("u", 65) + `"
  cost_report_id = "crp_0000000000000000000001"`,
			want: `character count must be between 1 and 64`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config:      api.ProviderBlock() + "resource \"costfluent_budget\" \"b\" {\n  " + tc.config + "\n}\n",
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}
}

func TestBudgetAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	api.FailNext(http.MethodPost, "/v1/budgets",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "Amount must be greater than zero"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      budgetConfig(api, "Platform", "500", ""),
				ExpectError: regexp.MustCompile(`Failed to create budget(.|\n)*Amount must be greater than zero`),
			},
			{Config: budgetConfig(api, "Platform", "500", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api),
						fakeapi.Problem(http.StatusNotFound, "NotFound", "Segment not found"))
				},
				Config:      budgetConfig(api, "Platform", "600", ""),
				ExpectError: regexp.MustCompile(`Failed to update budget(.|\n)*Segment not found`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/budgets/"+budgetOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "Budgets are not in this plan"))
				},
				Config:      budgetConfig(api, "Platform", "500", ""),
				ExpectError: regexp.MustCompile(`Failed to read budget(.|\n)*Budgets are not in this plan`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, "/v1/budgets/"+budgetOnlyID(t, api),
						fakeapi.Problem(http.StatusConflict, "Conflict", "Budget is locked"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete budget(.|\n)*Budget is locked`),
			},
		},
	})
}

// An update sends back, as If-Match, the ETag of the read the plan was made from, so an edit made
// in the product between that read and the apply is refused rather than overwritten; the 412 reads
// as a change outside Terraform. "500" stays "500" in state although the API answers it at eight
// decimals, so the scale never plans an update.
func TestBudgetUpdateSendsTheVersionItReadAndStopsOnAnOutsideChange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	var readTag string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: budgetConfig(api, "Platform", "500", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_budget.b", "amount", "500"),
					func(*terraform.State) error {
						readTag = api.ETag("/v1/budgets", budgetOnlyID(t, api))
						return nil
					},
				),
			},
			{
				Config: budgetConfig(api, "Platform", "500.00", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("costfluent_budget.b", plancheck.ResourceActionUpdate)},
				},
				Check: func(*terraform.State) error {
					if puts := api.Requests(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api)); len(puts) != 0 {
						return fmt.Errorf("the same amount at another scale sent %d updates", len(puts))
					}
					return nil
				},
			},
			{
				Config: budgetConfig(api, "Platform EU", "500.00", ""),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api))
					if got := update.Header.Get("If-Match"); got == "" || got != readTag {
						return fmt.Errorf("update sent If-Match %q, want the ETag of the last read %q", got, readTag)
					}
					return nil
				},
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/budgets/"+budgetOnlyID(t, api),
						fakeapi.Problem(http.StatusPreconditionFailed, "precondition_failed", "The resource changed since it was read"))
				},
				Config:      budgetConfig(api, "Platform US", "500.00", ""),
				ExpectError: regexp.MustCompile(`Budget changed outside Terraform`),
			},
		},
	})
}
