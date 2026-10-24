package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// workspaceStoredSpelling is how the API stores a value it reads in any case: a currency upper
// case, an enum camelCase and a zone under its own id, trimmed. Every other field is stored as sent.
func workspaceStoredSpelling(field string, value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	var spellings []string
	switch field {
	case "currency", "conversionCurrency":
		return strings.ToUpper(s)
	case "conversionMethod":
		spellings = []string{"monthlyAverage", "monthEndRate", "transactionDate"}
	case "dateInterval":
		spellings = []string{"thisMonth", "lastMonth"}
	case "dateBin":
		spellings = []string{"day", "week"}
	case "timeZone":
		s = strings.TrimSpace(s)
		spellings = []string{"UTC", "Europe/Berlin", "Europe/London"}
	}
	for _, stored := range spellings {
		if strings.EqualFold(s, stored) {
			return stored
		}
	}
	return s
}

// fakeWorkspaces serves /v1/workspaces with the WorkspaceCreatedResponse and WorkspaceResponse
// shapes. A new workspace starts on the defaults the API gives it: conversion into its own
// currency by monthly average, automatic syncing on, UTC unless a zone is sent.
func fakeWorkspaces(api *fakeapi.Server) {
	api.Collection("/v1/workspaces", fakeapi.Collection{
		IDPrefix:  "wsp_",
		Versioned: true,
		Create: func(body map[string]any) map[string]any {
			zone, ok := body["timeZone"]
			if !ok {
				zone = "UTC"
			}
			// The API refuses a currency that is not upper case on create; fakeapi.Collection has no
			// hook to refuse a create, and no test here creates with one.
			return map[string]any{
				"timeZone":                       workspaceStoredSpelling("timeZone", zone),
				"name":                           body["name"],
				"currency":                       body["currency"],
				"enableCurrencyConversion":       true,
				"conversionCurrency":             body["currency"],
				"conversionMethod":               "monthlyAverage",
				"enableAutomaticSyncing":         true,
				"anomaliesExcludeMarketplace":    true,
				"anomaliesExcludeCommitmentFees": true,
				"anomalyNotificationThreshold":   "0.00000000",
				"reportDefaults": map[string]any{
					"dateInterval": "thisMonth", "dateBin": "day", "includeCredits": false,
					"includeRefunds": false, "includeTax": true, "amortize": true,
					"showForecast": false, "compareToLastPeriod": true,
				},
				"providerCount": 0,
				"createdAt":     "2026-10-01T09:30:00Z",
			}
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "name": obj["name"]})
		},
		Apply: func(obj, body map[string]any) {
			for k, v := range body {
				if v == nil {
					continue
				}
				// The API keeps every part of the defaults a request leaves out.
				if parts, ok := v.(map[string]any); ok && k == "reportDefaults" {
					current, _ := obj[k].(map[string]any)
					for part, value := range parts {
						current[part] = workspaceStoredSpelling(part, value)
					}
					continue
				}
				obj[k] = workspaceStoredSpelling(k, v)
			}
			obj["updatedAt"] = "2026-10-02T10:00:00Z"
		},
	})
}

func workspaceOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/workspaces")
	if len(ids) != 1 {
		t.Fatalf("workspaces = %v, want one", ids)
	}
	return ids[0]
}

func workspaceConfig(api *fakeapi.Server, name, attrs string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_workspace" "w" {
  name     = %q
  currency = "EUR"
%s}
`, name, attrs)
}

func TestWorkspaceLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceConfig(api, "Platform", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_workspace.w", "id", regexp.MustCompile(`^wsp_.{22}$`)),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "name", "Platform"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "currency", "EUR"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "enable_currency_conversion", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_currency", "EUR"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_method", "monthlyAverage"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "enable_automatic_syncing", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "time_zone", "UTC"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "provider_count", "0"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "created_at", "2026-10-01T09:30:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_workspace.w", "updated_at"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/workspaces")
						if len(create.Body) != 3 || create.Body["name"] != "Platform" || create.Body["currency"] != "EUR" ||
							create.Body["timeZone"] != "UTC" {
							return fmt.Errorf("create body = %s, want name, currency and the default zone only", create.Raw)
						}
						// The defaults match what the API starts a workspace on, so no settings update follows.
						if puts := api.Requests(http.MethodPut, ""); len(puts) != 0 {
							return fmt.Errorf("create sent %d updates, want none", len(puts))
						}
						return nil
					},
				),
			},
			{
				Config: workspaceConfig(api, "Platform EU", `  enable_automatic_syncing = false
  conversion_currency      = "EUR"
  conversion_method        = "monthEndRate"
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "name", "Platform EU"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "enable_automatic_syncing", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_method", "monthEndRate"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "updated_at", "2026-10-02T10:00:00Z"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						want := map[string]any{"name": "Platform EU", "enableAutomaticSyncing": false, "conversionMethod": "monthEndRate"}
						if len(update.Body) != len(want) {
							return fmt.Errorf("update body = %s, want only the changed fields", update.Raw)
						}
						for k, v := range want {
							if update.Body[k] != v {
								return fmt.Errorf("update %s = %v, want %v (body %s)", k, update.Body[k], v, update.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_workspace.w",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspaces"); len(ids) != 0 {
				return fmt.Errorf("workspaces left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func TestWorkspaceTimeZoneIsSentOnCreateAndChangedInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceConfig(api, "Platform", "  time_zone = \"Europe/Berlin\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "time_zone", "Europe/Berlin"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/workspaces")
						if create.Body["timeZone"] != "Europe/Berlin" {
							return fmt.Errorf("create body = %s, want the configured zone", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: workspaceConfig(api, "Platform", "  time_zone = \"Europe/London\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace.w", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "time_zone", "Europe/London"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						if len(update.Body) != 1 || update.Body["timeZone"] != "Europe/London" {
							return fmt.Errorf("update body = %s, want the zone only", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// The API reads currencies, enums and zones in any case and stores them in its own spelling, so
// state keeps the configured one, on create and on update, while the stored value is the API's.
func TestWorkspaceKeepsTheConfiguredSpellingOfItsEnumsZoneAndCurrencies(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	const res = "costfluent_workspace.w"
	type spelling struct{ attr, field, configured, stored string }
	step := func(conversion bool, values ...spelling) resource.TestStep {
		var config strings.Builder
		checks := []resource.TestCheckFunc{}
		defaults := map[string]any{}
		for _, v := range values {
			checks = append(checks, resource.TestCheckResourceAttr(res, v.attr, v.configured))
			if name, ok := strings.CutPrefix(v.attr, "report_defaults."); ok {
				defaults[name] = v.configured
				continue
			}
			fmt.Fprintf(&config, "  %s = %q\n", v.attr, v.configured)
		}
		fmt.Fprintf(&config, "  enable_currency_conversion = %t\n", conversion)
		fmt.Fprintf(&config, "  report_defaults = {\n    date_interval = %q\n    date_bin = %q\n  }\n",
			defaults["date_interval"], defaults["date_bin"])
		checks = append(checks, func(*terraform.State) error {
			obj, _ := api.Get("/v1/workspaces", workspaceOnlyID(t, api))
			reportDefaults, _ := obj["reportDefaults"].(map[string]any)
			for _, v := range values {
				got := obj[v.field]
				if _, ok := strings.CutPrefix(v.attr, "report_defaults."); ok {
					got = reportDefaults[v.field]
				}
				if got != v.stored {
					return fmt.Errorf("stored %s = %v, want %s", v.field, got, v.stored)
				}
			}
			return nil
		})
		return resource.TestStep{
			Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_workspace" "w" {
  name = "Platform"
%s}
`, config.String()),
			Check: resource.ComposeAggregateTestCheckFunc(checks...),
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(true,
				spelling{"currency", "currency", "EUR", "EUR"},
				spelling{"conversion_currency", "conversionCurrency", "gbp", "GBP"},
				spelling{"conversion_method", "conversionMethod", "TransactionDate", "transactionDate"},
				spelling{"time_zone", "timeZone", "europe/berlin", "Europe/Berlin"},
				spelling{"report_defaults.date_interval", "dateInterval", "LastMonth", "lastMonth"},
				spelling{"report_defaults.date_bin", "dateBin", "WEEK", "week"},
			),
			step(false,
				spelling{"currency", "currency", "eur", "EUR"},
				spelling{"conversion_currency", "conversionCurrency", "Gbp", "GBP"},
				spelling{"conversion_method", "conversionMethod", "MONTHLYAVERAGE", "monthlyAverage"},
				spelling{"time_zone", "timeZone", " Europe/London ", "Europe/London"},
				spelling{"report_defaults.date_interval", "dateInterval", "thisMonth", "thisMonth"},
				spelling{"report_defaults.date_bin", "dateBin", "Day", "day"},
			),
		},
	})
}

// Create takes a name, a currency and a zone only, so settings that differ from a new workspace's defaults
// follow as one update in the same apply.
func TestWorkspaceCreateAppliesNonDefaultSettingsAsAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceConfig(api, "Platform", `  conversion_currency      = "GBP"
  conversion_method        = "transactionDate"
  enable_automatic_syncing = false
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_currency", "GBP"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_method", "transactionDate"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "enable_automatic_syncing", "false"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						want := map[string]any{"conversionCurrency": "GBP", "conversionMethod": "transactionDate", "enableAutomaticSyncing": false}
						if len(update.Body) != len(want) {
							return fmt.Errorf("settings body = %s, want %v", update.Raw, want)
						}
						for k, v := range want {
							if update.Body[k] != v {
								return fmt.Errorf("settings %s = %v, want %v", k, update.Body[k], v)
							}
						}
						return nil
					},
				),
			},
		},
	})
}

// An update that leaves conversion_currency unset must not send it: the API rejects an empty
// conversionCurrency.
func TestWorkspaceUpdateLeavesAnUnsetConversionCurrencyAlone(t *testing.T) {
	t.Parallel()

	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: workspaceConfig(api, "Platform", "")},
			{
				Config: workspaceConfig(api, "Platform EU", ""),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
					if _, sent := update.Body["conversionCurrency"]; sent {
						return fmt.Errorf("rename sent conversionCurrency: %s", update.Raw)
					}
					return nil
				},
			},
			{
				Config: workspaceConfig(api, "Platform EU", "  enable_automatic_syncing = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "conversion_currency", "EUR"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						if _, sent := update.Body["conversionCurrency"]; sent {
							return fmt.Errorf("settings change sent conversionCurrency: %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestWorkspaceDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: workspaceConfig(api, "Platform", "")},
			{
				PreConfig:          func() { api.Remove("/v1/workspaces", workspaceOnlyID(t, api)) },
				Config:             workspaceConfig(api, "Platform", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestWorkspaceAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	api.FailNext(http.MethodPost, "/v1/workspaces",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "Currency must be an ISO 4217 code"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      workspaceConfig(api, "Platform", ""),
				ExpectError: regexp.MustCompile(`Failed to create workspace(.|\n)*Currency must be an ISO 4217 code`),
			},
			{Config: workspaceConfig(api, "Platform", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation",
							"Currency cannot change while conversion is enabled"))
				},
				Config:      workspaceConfig(api, "Platform EU", ""),
				ExpectError: regexp.MustCompile(`Failed to update workspace(.|\n)*Currency cannot change while conversion is enabled`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/workspaces/"+workspaceOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read this workspace"))
				},
				Config:      workspaceConfig(api, "Platform", ""),
				ExpectError: regexp.MustCompile(`Failed to read workspace(.|\n)*The token cannot read this workspace`),
			},
		},
	})
}

func TestWorkspaceSettingsFailureAfterCreateIsReported(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	// The fake numbers IDs from 1, so the workspace this test creates is the first.
	api.FailNext(http.MethodPut, "/v1/workspaces/wsp_"+fmt.Sprintf("%022d", 1),
		fakeapi.Problem(http.StatusBadRequest, "Validation", "Unknown conversion method"))

	// The workspace exists once the create returns, so it is in state, tainted, and the destroy
	// at the end removes it rather than leaving an orphan no configuration manages.
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspaces"); len(ids) != 0 {
				return fmt.Errorf("workspaces left after destroy: %v", ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      workspaceConfig(api, "Platform", "  enable_automatic_syncing = false\n"),
				ExpectError: regexp.MustCompile(`Failed to apply workspace settings(.|\n)*Unknown conversion method`),
			},
			{
				Config: workspaceConfig(api, "Platform", "  enable_automatic_syncing = false\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace.w", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
	})
}

// createWhoseReadFailsIsTainted applies config, which declares one resource at address, while the
// read after its create is refused. The object exists once the create returns, so its ID is kept
// in state, tainted: the next apply replaces it, deleting the first object rather than leaving it
// behind, and the destroy at the end removes the second.
func createWhoseReadFailsIsTainted(t *testing.T, api *fakeapi.Server, collection, idPrefix, address, config, summary string) {
	t.Helper()
	// The fake numbers IDs from 1, so the object this test creates is the first.
	first := idPrefix + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodGet, collection+"/"+first,
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read it"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs(collection); len(ids) != 0 {
				return fmt.Errorf("%s left after destroy: %v", collection, ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(summary + `(.|\n)*The token cannot read it`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if ids := api.IDs(collection); len(ids) != 1 || ids[0] == first {
						return fmt.Errorf("%s = %v, want only the replacement of %s", collection, ids, first)
					}
					return nil
				},
			},
		},
	})
}

func TestWorkspaceCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/workspaces", "wsp_", "costfluent_workspace.w",
		workspaceConfig(api, "Platform", ""), "Failed to create workspace")
}

func TestWorkspaceReportDefaultsSendOnlyWhatTheConfigurationNames(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceConfig(api, "Platform", `  report_defaults = {
    date_interval = "lastMonth"
    amortize      = false
  }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "report_defaults.date_interval", "lastMonth"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "report_defaults.amortize", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "report_defaults.include_tax", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "report_defaults.date_bin", "day"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						sent, ok := update.Body["reportDefaults"].(map[string]any)
						if !ok || len(sent) != 2 || sent["dateInterval"] != "lastMonth" || sent["amortize"] != false {
							return fmt.Errorf("update body = %s, want the two named defaults only", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: workspaceConfig(api, "Platform", `  report_defaults = {
    date_interval = "lastMonth"
    date_bin      = "week"
    amortize      = false
  }
`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace.w", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "report_defaults.date_bin", "week"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						if len(update.Body) != 1 || update.Body["reportDefaults"] == nil {
							return fmt.Errorf("update body = %s, want the defaults only", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// The anomaly settings start on the API's defaults, a configuration that names one is applied as
// the create's settings update, and a change later sends only what changed. A threshold the API
// answers with eight decimals keeps the configuration's spelling, so the plan stays empty.
func TestWorkspaceAnomalySettingsAreAppliedAndChangedInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceConfig(api, "Platform", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "anomalies_exclude_marketplace", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "anomalies_exclude_commitment_fees", "true"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "anomaly_notification_threshold", "0"),
				),
			},
			{
				Config: workspaceConfig(api, "Platform",
					"  anomalies_exclude_marketplace = false\n  anomaly_notification_threshold = \"250\"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace.w", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace.w", "anomalies_exclude_marketplace", "false"),
					resource.TestCheckResourceAttr("costfluent_workspace.w", "anomaly_notification_threshold", "250"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, "/v1/workspaces/"+workspaceOnlyID(t, api))
						if len(update.Body) != 2 || update.Body["anomaliesExcludeMarketplace"] != false ||
							update.Body["anomalyNotificationThreshold"] != "250" {
							return fmt.Errorf("update body = %s, want the marketplace flag and the threshold only", update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: workspaceConfig(api, "Platform",
					"  anomalies_exclude_marketplace = false\n  anomaly_notification_threshold = \"250\"\n"),
				PlanOnly: true,
			},
		},
	})
}

func TestWorkspaceCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	workspaces := func() string { return "/v1/workspaces" }
	workspace := func() string { return "/v1/workspaces/" + workspaceOnlyID(t, api) }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   refuseNext(api, http.MethodPost, workspaces, costfluent.CapabilityCreateWorkspace),
				Config:      workspaceConfig(api, "Finance", ""),
				ExpectError: lacksCapability("Failed to create workspace", costfluent.CapabilityCreateWorkspace),
			},
			{Config: workspaceConfig(api, "Finance", "")},
			{
				PreConfig:   refuseNext(api, http.MethodPut, workspace, costfluent.CapabilityUpdateWorkspace),
				Config:      workspaceConfig(api, "Finance EU", ""),
				ExpectError: lacksCapability("Failed to update workspace", costfluent.CapabilityUpdateWorkspace),
			},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, workspace, costfluent.CapabilityDeleteWorkspace),
				Config:      workspaceConfig(api, "Finance", ""),
				Destroy:     true,
				ExpectError: lacksCapability("Failed to delete workspace", costfluent.CapabilityDeleteWorkspace),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspaces"); len(ids) != 0 {
				return fmt.Errorf("workspaces left after destroy: %v", ids)
			}
			return nil
		},
	})
}
