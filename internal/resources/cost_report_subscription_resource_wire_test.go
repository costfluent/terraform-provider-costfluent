package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	subscriptionReport = "crp_0000000000000000000001"
	subscriptionApp    = "app_0000000000000000000001"
)

// subscriptionZones are the zones the fake knows. The API trims a zone and finds it whatever its
// case, storing its own spelling, and an alias such as US/Eastern is its own id there.
var subscriptionZones = []string{"Europe/Berlin", "America/New_York", "US/Eastern"}

func subscriptionZone(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	for _, zone := range subscriptionZones {
		if strings.EqualFold(zone, strings.TrimSpace(s)) {
			return zone
		}
	}
	return s
}

// subscriptionFake serves /v1/cost-report-subscriptions as the API does: the create answers the
// id, an update is a PATCH naming the workspace in its body and answers the subscription, every
// write is versioned, and the workspace's zone and 9 o'clock fill what a create leaves out.
func subscriptionFake(api *fakeapi.Server) {
	const path = "/v1/cost-report-subscriptions"
	api.Collection(path, fakeapi.Collection{
		IDPrefix:              "crs_",
		Versioned:             true,
		Scope:                 fakeapi.WorkspaceScoped,
		UpdateMethod:          http.MethodPatch,
		UpdateWorkspaceInBody: true,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"costReportId": body["costReportId"], "name": body["name"], "scheduleType": body["scheduleType"],
				"deliveryHour": float64(9), "timeZone": "Europe/Berlin", "appIds": body["appIds"], "isActive": true,
				"createdAt": "2026-10-01T09:30:00Z",
			}
			if hour, ok := body["deliveryHour"]; ok {
				obj["deliveryHour"] = hour
			}
			if zone, ok := body["timeZone"]; ok {
				obj["timeZone"] = subscriptionZone(zone)
			}
			return obj
		},
		Created: fakeapi.IDOnly,
		Apply: func(obj, body map[string]any) {
			for k, v := range body {
				if v != nil && k != "workspaceId" {
					obj[k] = v
				}
			}
			if zone, ok := body["timeZone"]; ok {
				obj["timeZone"] = subscriptionZone(zone)
			}
		},
	})
	// Pausing and resuming answer 204 with the new version, and refuse a stale If-Match.
	toggle := func(active bool) fakeapi.Handler {
		return func(r fakeapi.Request) fakeapi.Response {
			id := r.PathValue("id")
			if _, ok := api.Get(path, id); !ok || r.Query.Get("workspaceId") != testWorkspace {
				return fakeapi.NotFound()
			}
			if match := r.Header.Get("If-Match"); match != "" && match != api.ETag(path, id) {
				return fakeapi.Problem(http.StatusPreconditionFailed, "precondition_failed", "The resource changed since it was read")
			}
			api.Update(path, id, func(obj map[string]any) { obj["isActive"] = active })
			resp := fakeapi.NoContent()
			resp.Header = http.Header{"Etag": []string{api.ETag(path, id)}}
			return resp
		}
	}
	api.Handle("POST "+path+"/{id}/activate", toggle(true))
	api.Handle("POST "+path+"/{id}/deactivate", toggle(false))
}

func subscriptionConfig(api *fakeapi.Server, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_report_subscription" "s" {
  workspace_id   = %q
  cost_report_id = %q
  name           = "Weekly spend"
  schedule_type  = "weekly"
  app_ids        = [%q]
%s}
`, testWorkspace, subscriptionReport, subscriptionApp, extra)
}

func subscriptionOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/cost-report-subscriptions")
	if len(ids) != 1 {
		t.Fatalf("subscriptions = %v, want one", ids)
	}
	return ids[0]
}

func TestCostReportSubscriptionLifecycle(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)
	const res = "costfluent_cost_report_subscription.s"
	var readTag string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "delivery_hour", "9"),
					resource.TestCheckResourceAttr(res, "time_zone", "Europe/Berlin"),
					resource.TestCheckResourceAttr(res, "is_active", "true"),
					resource.TestCheckResourceAttr(res, "app_ids.0", subscriptionApp),
					resource.TestMatchResourceAttr(res, "id", regexp.MustCompile(`^crs_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/cost-report-subscriptions")
						if create.Body["workspaceId"] != testWorkspace || create.Body["costReportId"] != subscriptionReport {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, omitted := range []string{"deliveryHour", "timeZone"} {
							if _, sent := create.Body[omitted]; sent {
								return fmt.Errorf("create sent unset %s: %s", omitted, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				// The PATCH carries the ETag of the last read, and the pause after it the PATCH's.
				PreConfig: func() { readTag = api.ETag("/v1/cost-report-subscriptions", subscriptionOnlyID(t, api)) },
				Config:    subscriptionConfig(api, "  delivery_hour = 7\n  is_active = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "delivery_hour", "7"),
					resource.TestCheckResourceAttr(res, "is_active", "false"),
					func(*terraform.State) error {
						id := subscriptionOnlyID(t, api)
						update := api.LastRequest(http.MethodPatch, "/v1/cost-report-subscriptions/"+id)
						if update.Body["workspaceId"] != testWorkspace || update.Body["deliveryHour"] != float64(7) {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						for _, unchanged := range []string{"name", "scheduleType", "appIds", "timeZone"} {
							if _, sent := update.Body[unchanged]; sent {
								return fmt.Errorf("update sent unchanged %s: %s", unchanged, update.Raw)
							}
						}
						if got := update.Header.Get("If-Match"); got == "" || got != readTag {
							return fmt.Errorf("update sent If-Match %q, want the ETag of the last read %q", got, readTag)
						}
						pauses := api.Requests(http.MethodPost, "/v1/cost-report-subscriptions/"+id+"/deactivate")
						if len(pauses) != 1 {
							return fmt.Errorf("the notification was not paused")
						}
						if got := pauses[0].Header.Get("If-Match"); got == "" || got == readTag {
							return fmt.Errorf("pause sent If-Match %q, want the version the update answered", got)
						}
						return nil
					},
				),
			},
			{
				// The pause changed the version; Terraform kept the one it produced, so the next write
				// carries it rather than being refused for Terraform's own change.
				PreConfig: func() { readTag = api.ETag("/v1/cost-report-subscriptions", subscriptionOnlyID(t, api)) },
				Config:    subscriptionConfig(api, "  delivery_hour = 7\n"),
				Check: func(*terraform.State) error {
					id := subscriptionOnlyID(t, api)
					if len(api.Requests(http.MethodPost, "/v1/cost-report-subscriptions/"+id+"/activate")) != 1 {
						return fmt.Errorf("the notification was not resumed")
					}
					if got := api.LastRequest(http.MethodPatch, "/v1/cost-report-subscriptions/"+id).Header.Get("If-Match"); got != readTag {
						return fmt.Errorf("update sent If-Match %q, want the ETag of the version after the pause %q", got, readTag)
					}
					return nil
				},
			},
			{
				ResourceName:      res,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + subscriptionOnlyID(t, api), nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/cost-report-subscriptions"); len(ids) != 0 {
				return fmt.Errorf("subscriptions left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// The API stores a zone in its own spelling, so state keeps the configured one while it names the
// same zone: a padded or case-variant time_zone neither fails the apply nor plans an update.
func TestCostReportSubscriptionKeepsTheConfiguredSpellingOfItsZone(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)
	const res = "costfluent_cost_report_subscription.s"
	step := func(configured, stored string) resource.TestStep {
		return resource.TestStep{
			Config: subscriptionConfig(api, fmt.Sprintf("  time_zone = %q\n", configured)),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(res, "time_zone", configured),
				func(*terraform.State) error {
					obj, _ := api.Get("/v1/cost-report-subscriptions", subscriptionOnlyID(t, api))
					if obj["timeZone"] != stored {
						return fmt.Errorf("stored zone = %v, want %s", obj["timeZone"], stored)
					}
					return nil
				},
			),
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			step(" europe/berlin", "Europe/Berlin"),
			step("Europe/Berlin  ", "Europe/Berlin"),
			step("AMERICA/NEW_YORK", "America/New_York"),
			step("US/Eastern", "US/Eastern"),
			step("America/New_York", "America/New_York"),
		},
	})
}

func TestCostReportSubscriptionCreatedPausedIsPausedAfterCreate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: subscriptionConfig(api, "  is_active = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_cost_report_subscription.s", "is_active", "false"),
					func(*terraform.State) error {
						obj, _ := api.Get("/v1/cost-report-subscriptions", subscriptionOnlyID(t, api))
						if obj["isActive"] != false {
							return fmt.Errorf("stored = %v", obj)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestCostReportSubscriptionCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/cost-report-subscriptions", "crs_", "costfluent_cost_report_subscription.s",
		subscriptionConfig(api, ""), "Failed to create report notification")
}

func TestCostReportSubscriptionRefusesAnHourOutsideTheDayAndAnEmptyDestination(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      subscriptionConfig(api, "  delivery_hour = 24\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`between 0 and 23`),
			},
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_report_subscription" "s" {
  workspace_id   = %q
  cost_report_id = %q
  name           = "Weekly spend"
  schedule_type  = "weekly"
  app_ids        = []
}
`, testWorkspace, subscriptionReport),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`at least 1`),
			},
		},
	})
}

func TestCostReportSubscriptionAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)
	const path = "/v1/cost-report-subscriptions"
	fail := func(method, suffix, detail string) func() {
		return func() {
			api.FailNext(method, path+"/"+subscriptionOnlyID(t, api)+suffix, fakeapi.Problem(http.StatusBadRequest, "Validation", detail))
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig: func() {
					api.FailNext(http.MethodPost, path, fakeapi.Problem(http.StatusBadRequest, "Validation", "Only email apps receive reports"))
				},
				Config:      subscriptionConfig(api, ""),
				ExpectError: regexp.MustCompile(`Failed to create report notification(.|\n)*Only email apps`),
			},
			{Config: subscriptionConfig(api, "")},
			{
				PreConfig:   fail(http.MethodPatch, "", "Unknown time zone"),
				Config:      subscriptionConfig(api, "  time_zone = \"Mars/Olympus\"\n"),
				ExpectError: regexp.MustCompile(`Failed to update report notification(.|\n)*Unknown time zone`),
			},
			{
				PreConfig:   fail(http.MethodPost, "/deactivate", "Already paused"),
				Config:      subscriptionConfig(api, "  is_active = false\n"),
				ExpectError: regexp.MustCompile(`Failed to pause or resume report notification(.|\n)*Already paused`),
			},
			{
				PreConfig:   fail(http.MethodGet, "", "Reports are not in this plan"),
				Config:      subscriptionConfig(api, ""),
				ExpectError: regexp.MustCompile(`Failed to read report notification(.|\n)*not in this plan`),
			},
			{
				PreConfig:   fail(http.MethodDelete, "", "Delivery in progress"),
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete report notification(.|\n)*in progress`),
			},
		},
	})
}

func TestCostReportSubscriptionPauseFailureAfterCreateIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	subscriptionFake(api)
	first := "crs_" + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodPost, "/v1/cost-report-subscriptions/"+first+"/deactivate",
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "Pausing needs the editor role"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      subscriptionConfig(api, "  is_active = false\n"),
				ExpectError: regexp.MustCompile(`Failed to pause the new report notification(.|\n)*editor role`),
			},
			{
				Config:             subscriptionConfig(api, "  is_active = false\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				Check: func(*terraform.State) error {
					if ids := api.IDs("/v1/cost-report-subscriptions"); len(ids) != 1 {
						return fmt.Errorf("subscriptions = %v", ids)
					}
					return nil
				},
			},
		},
	})
}
