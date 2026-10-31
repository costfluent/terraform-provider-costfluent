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

// fakeProviders serves /v1/providers with the ProviderCreatedResponse and ProviderResponse shapes.
// Credentials and settings are write-only: the API never returns them.
func fakeProviders(api *fakeapi.Server) {
	api.Collection("/v1/providers", fakeapi.Collection{
		IDPrefix:  "prv_",
		Versioned: true,
		Create: func(body map[string]any) map[string]any {
			return map[string]any{
				"type":                 "cloud",
				"key":                  body["key"],
				"displayName":          body["displayName"],
				"description":          body["description"],
				"status":               "active",
				"externalId":           "123456789012",
				"parentProviderId":     body["parentProviderId"],
				"lastSyncAt":           "2026-10-01T06:00:00Z",
				"lastSyncStatus":       "succeeded",
				"nextSyncAt":           "2026-10-02T06:00:00Z",
				"syncFrequencyMinutes": body["syncFrequencyMinutes"],
				"createdAt":            "2026-10-01T05:00:00Z",
			}
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "type": obj["type"], "key": obj["key"], "displayName": obj["displayName"]})
		},
		Apply: func(obj, body map[string]any) {
			// An empty description is stored and returned as the empty string; clearDescription
			// removes it.
			for _, k := range []string{"displayName", "description", "externalId", "syncFrequencyMinutes"} {
				if v, ok := body[k]; ok {
					obj[k] = v
				}
			}
			if body["clearDescription"] == true {
				delete(obj, "description")
			}
			obj["updatedAt"] = "2026-10-03T08:00:00Z"
		},
	})
}

func providerOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/providers")
	if len(ids) != 1 {
		t.Fatalf("providers = %v, want one", ids)
	}
	return ids[0]
}

func providerLifecycleConfig(api *fakeapi.Server, roleArn, attrs string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_provider" "aws" {
  key         = "aws"
  display_name = "AWS prod"
  credentials = { role_arn = %q }
%s}
`, roleArn, attrs)
}

const (
	providerRoleArn        = "arn:aws:iam::123456789012:role/CostfluentBillingRole"
	providerRotatedRoleArn = "arn:aws:iam::123456789012:role/CostfluentBillingRoleV2"
)

func TestProviderLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerLifecycleConfig(api, providerRoleArn, `  description = "Billing account"
  settings    = { export_bucket = "cur-exports", export_bucket_region = "eu-west-1" }
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr("costfluent_provider.aws", "id", regexp.MustCompile(`^prv_.{22}$`)),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "status", "active"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "description", "Billing account"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "external_id", "123456789012"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "sync_frequency_minutes", "1440"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "last_sync_at", "2026-10-01T06:00:00Z"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "last_sync_status", "succeeded"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "next_sync_at", "2026-10-02T06:00:00Z"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "created_at", "2026-10-01T05:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_provider.aws", "updated_at"),
					resource.TestCheckNoResourceAttr("costfluent_provider.aws", "parent_provider_id"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/providers")
						creds, _ := create.Body["credentials"].(map[string]any)
						settings, _ := create.Body["settings"].(map[string]any)
						switch {
						case create.Body["key"] != "aws", create.Body["displayName"] != "AWS prod",
							create.Body["description"] != "Billing account",
							create.Body["syncFrequencyMinutes"] != float64(1440):
							return fmt.Errorf("create body = %s", create.Raw)
						case len(creds) != 1 || creds["role_arn"] != providerRoleArn:
							return fmt.Errorf("create credentials = %v", creds)
						case len(settings) != 2 || settings["export_bucket"] != "cur-exports":
							return fmt.Errorf("create settings = %v", settings)
						}
						for _, absent := range []string{"externalId", "parentProviderId"} {
							if _, sent := create.Body[absent]; sent {
								return fmt.Errorf("create sent unset %s: %s", absent, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				// Rotating the role, removing the description and syncing hourly is one update in place.
				Config: providerLifecycleConfig(api, providerRotatedRoleArn, `  settings               = { export_bucket = "cur-exports", export_bucket_region = "eu-west-1" }
  sync_frequency_minutes = 60
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_provider.aws", "description"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "sync_frequency_minutes", "60"),
					resource.TestCheckResourceAttr("costfluent_provider.aws", "updated_at", "2026-10-03T08:00:00Z"),
					func(*terraform.State) error {
						if n := len(api.Requests(http.MethodPost, "/v1/providers")); n != 1 {
							return fmt.Errorf("provider created %d times, want an update in place", n)
						}
						update := api.LastRequest(http.MethodPut, "/v1/providers/"+providerOnlyID(t, api))
						creds, _ := update.Body["credentials"].(map[string]any)
						switch {
						case update.Body["clearDescription"] != true:
							return fmt.Errorf("update clearDescription = %v, want true", update.Body["clearDescription"])
						case update.Body["syncFrequencyMinutes"] != float64(60):
							return fmt.Errorf("update body = %s", update.Raw)
						case len(creds) != 1 || creds["role_arn"] != providerRotatedRoleArn:
							return fmt.Errorf("update credentials = %v", creds)
						}
						for _, unchanged := range []string{"settings", "clearSettings", "description", "displayName", "externalId"} {
							if _, sent := update.Body[unchanged]; sent {
								return fmt.Errorf("update sent unchanged %s: %s", unchanged, update.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_provider.aws",
				ImportState:       true,
				ImportStateVerify: true,
				// The API never returns credentials or settings, so an import cannot read them back.
				ImportStateVerifyIgnore: []string{"credentials", "settings"},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/providers"); len(ids) != 0 {
				return fmt.Errorf("providers left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// Removing a description clears it with clearDescription; an empty one configured is stored as the
// empty string. Removing one, configuring an empty one and removing that again each settle without
// a change left to plan.
func TestProviderDescriptionClearedAndEmptiedStaysConsistent(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)
	const res = "costfluent_provider.aws"
	stored := func(want any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			obj, _ := api.Get("/v1/providers", providerOnlyID(t, api))
			if obj["description"] != want {
				return fmt.Errorf("stored description = %#v, want %#v", obj["description"], want)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerLifecycleConfig(api, providerRoleArn, "  description = \"Billing account\"\n"),
				Check:  resource.TestCheckResourceAttr(res, "description", "Billing account"),
			},
			{
				Config: providerLifecycleConfig(api, providerRoleArn, ""),
				Check:  resource.ComposeAggregateTestCheckFunc(resource.TestCheckNoResourceAttr(res, "description"), stored(nil)),
			},
			{
				Config: providerLifecycleConfig(api, providerRoleArn, "  description = \"\"\n"),
				Check:  resource.ComposeAggregateTestCheckFunc(resource.TestCheckResourceAttr(res, "description", ""), stored("")),
			},
			{
				Config: providerLifecycleConfig(api, providerRoleArn, ""),
				Check:  resource.ComposeAggregateTestCheckFunc(resource.TestCheckNoResourceAttr(res, "description"), stored(nil)),
			},
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"credentials", "settings"},
			},
		},
	})
}

// The API never returns settings, so removing or emptying them is sent as clearSettings: an update
// in place that leaves the same connection, rather than a new one.
func TestProviderSettingsRemovedOrEmptiedAreClearedInPlace(t *testing.T) {
	t.Parallel()
	settings := "  settings    = { export_bucket = \"cur-exports\" }\n"
	for name, after := range map[string]string{
		"removed": "",
		"emptied": "  settings    = {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			fakeProviders(api)
			var id string

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: providerLifecycleConfig(api, providerRoleArn, settings),
						Check:  func(*terraform.State) error { id = providerOnlyID(t, api); return nil },
					},
					{
						Config: providerLifecycleConfig(api, providerRoleArn, "  settings    = { export_bucket = \"cur-exports-2\" }\n"),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("costfluent_provider.aws", plancheck.ResourceActionUpdate),
							},
						},
					},
					{
						Config: providerLifecycleConfig(api, providerRoleArn, after),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("costfluent_provider.aws", plancheck.ResourceActionUpdate),
							},
						},
						Check: func(*terraform.State) error {
							if got := providerOnlyID(t, api); got != id {
								return fmt.Errorf("provider id = %s, want the same %s", got, id)
							}
							update := api.LastRequest(http.MethodPut, "/v1/providers/"+id)
							if update.Body["clearSettings"] != true {
								return fmt.Errorf("update body = %s, want clearSettings", update.Raw)
							}
							if _, sent := update.Body["settings"]; sent {
								return fmt.Errorf("update sent settings beside clearSettings: %s", update.Raw)
							}
							return nil
						},
					},
				},
			})
		})
	}
}

// A rename is an update in place: the connection keeps its id, credentials and collected costs.
func TestProviderRenameIsAnUpdateInPlace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)
	var id string
	renamed := api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_provider" "aws" {
  key         = "aws"
  display_name = "AWS prod (EU)"
  credentials = { role_arn = %q }
}
`, providerRoleArn)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: providerLifecycleConfig(api, providerRoleArn, ""),
				Check:  func(*terraform.State) error { id = providerOnlyID(t, api); return nil },
			},
			{
				Config: renamed,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_provider.aws", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_provider.aws", "display_name", "AWS prod (EU)"),
					func(*terraform.State) error {
						if got := providerOnlyID(t, api); got != id {
							return fmt.Errorf("provider id = %s, want the same %s", got, id)
						}
						update := api.LastRequest(http.MethodPut, "/v1/providers/"+id)
						if update.Body["displayName"] != "AWS prod (EU)" {
							return fmt.Errorf("update body = %s, want the new display name", update.Raw)
						}
						if _, sent := update.Body["credentials"]; sent {
							return fmt.Errorf("rename sent unchanged credentials: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{Config: renamed, PlanOnly: true},
		},
	})
}

// The API cannot move a linked account to another parent, and replacing it would drop the
// connection's collected history, so a changed parent is refused when it is planned.
func TestProviderParentChangeIsRefusedAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: providerLifecycleConfig(api, providerRoleArn, "  parent_provider_id = \"prv_aaaaaaaaaaaaaaaaaaaaaa\"\n")},
			{
				Config:      providerLifecycleConfig(api, providerRoleArn, "  parent_provider_id = \"prv_bbbbbbbbbbbbbbbbbbbbbb\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`parent_provider_id cannot change`),
			},
			{
				Config:      providerLifecycleConfig(api, providerRoleArn, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`parent_provider_id cannot change`),
			},
		},
	})
}

func TestProviderDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: providerLifecycleConfig(api, providerRoleArn, "")},
			{
				PreConfig:          func() { api.Remove("/v1/providers", providerOnlyID(t, api)) },
				Config:             providerLifecycleConfig(api, providerRoleArn, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestProviderAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)
	api.FailNext(http.MethodPost, "/v1/providers", fakeapi.Response{
		Status: http.StatusBadRequest,
		Body: map[string]any{
			"title": "Bad Request", "status": 400, "code": "Validation", "retryable": false,
			"errors": []any{map[string]any{"name": "credentials.role_arn", "reason": "Role cannot be assumed", "code": "Provider.AssumeRoleFailed"}},
		},
	})

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// A failure other than access pending is not retried.
				Config:      providerLifecycleConfig(api, providerRoleArn, ""),
				ExpectError: regexp.MustCompile(`Failed to create provider(.|\n)*Role cannot be assumed`),
			},
			{Config: providerLifecycleConfig(api, providerRoleArn, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/providers/"+providerOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "SyncFrequencyMinutes must be between 60 and 1440"))
				},
				Config:      providerLifecycleConfig(api, providerRoleArn, "  sync_frequency_minutes = 61\n"),
				ExpectError: regexp.MustCompile(`Failed to update provider(.|\n)*SyncFrequencyMinutes must be between 60 and 1440`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/providers/"+providerOnlyID(t, api),
						fakeapi.Problem(http.StatusUnauthorized, "Unauthorized", "The API key was revoked"))
				},
				Config:      providerLifecycleConfig(api, providerRoleArn, ""),
				ExpectError: regexp.MustCompile(`Failed to read provider(.|\n)*The API key was revoked`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, "/v1/providers/"+providerOnlyID(t, api),
						fakeapi.Problem(http.StatusConflict, "Conflict", "Provider still has linked accounts"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete provider(.|\n)*Provider still has linked accounts`),
			},
		},
	})
}

func TestProviderCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeProviders(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/providers", "prv_", "costfluent_provider.aws",
		providerLifecycleConfig(api, providerRoleArn, ""), "Failed to read created provider")
}
