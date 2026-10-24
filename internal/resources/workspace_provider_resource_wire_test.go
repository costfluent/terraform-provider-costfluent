package resources_test

import (
	"fmt"
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

const testConnection = "prv_0000000000000000000001"

// workspaceProviderFake serves /v1/workspace-providers with the WorkspaceProviderResponse shape.
// Like the API, a link starts enabled, answers an empty list for the whole connection, and an
// update replaces the accounts only when the body names them. Given the connection's stored
// accounts, it resolves each id as the API does, trimmed and ignoring case, keeps each account
// once, and answers the stored spelling.
func workspaceProviderFake(api *fakeapi.Server, stored ...string) {
	resolve := func(raw any) []any {
		accounts, _ := raw.([]any)
		if accounts == nil {
			return []any{}
		}
		resolved := make([]any, 0, len(accounts))
		for _, v := range accounts {
			id, _ := v.(string)
			if i := slices.IndexFunc(stored, func(s string) bool { return strings.EqualFold(s, strings.TrimSpace(id)) }); i >= 0 {
				v = stored[i]
			}
			if !slices.Contains(resolved, v) {
				resolved = append(resolved, v)
			}
		}
		return resolved
	}
	api.Collection("/v1/workspace-providers", fakeapi.Collection{
		IDPrefix:              "wpr_",
		Scope:                 fakeapi.WorkspaceScoped,
		UpdateWorkspaceInBody: true,
		Create: func(body map[string]any) map[string]any {
			accounts := resolve(body["accountIds"])
			return map[string]any{
				"workspaceId":  body["workspaceId"],
				"providerId":   body["providerId"],
				"providerName": "AWS Organization",
				"providerKey":  "aws",
				"isEnabled":    true,
				"accountIds":   accounts,
				"addedAt":      "2026-10-12T08:00:00Z",
			}
		},
		Apply: func(obj, body map[string]any) {
			if v, ok := body["isEnabled"]; ok && v != nil {
				obj["isEnabled"] = v
			}
			if v, ok := body["accountIds"]; ok && v != nil {
				obj["accountIds"] = resolve(v)
			}
		},
	})
}

func workspaceProviderConfig(api *fakeapi.Server, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_workspace_provider" "w" {
  workspace_id = %q
  provider_id  = %q
%s}
`, testWorkspace, testConnection, extra)
}

func workspaceProviderOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/workspace-providers")
	if len(ids) != 1 {
		t.Fatalf("workspace providers = %v, want one", ids)
	}
	return ids[0]
}

// sentAccounts is the accountIds list a request carried, sorted, and whether it carried one.
func sentAccounts(r fakeapi.Request) ([]string, bool) {
	raw, ok := r.Body["accountIds"].([]any)
	if !ok {
		return nil, false
	}
	ids := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		ids = append(ids, s)
	}
	slices.Sort(ids)
	return ids, true
}

func TestWorkspaceProviderLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	const address = "costfluent_workspace_provider.w"
	path := func() string { return "/v1/workspace-providers/" + workspaceProviderOnlyID(t, api) }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceProviderConfig(api, `  account_ids  = ["222222222222", "111111111111"]`+"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^wpr_.{22}$`)),
					resource.TestCheckResourceAttr(address, "account_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "111111111111"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "222222222222"),
					resource.TestCheckResourceAttr(address, "is_enabled", "true"),
					resource.TestCheckResourceAttr(address, "provider_name", "AWS Organization"),
					resource.TestCheckResourceAttr(address, "provider_key", "aws"),
					resource.TestCheckResourceAttr(address, "added_at", "2026-10-12T08:00:00Z"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/workspace-providers")
						if create.Body["workspaceId"] != testWorkspace || create.Body["providerId"] != testConnection {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						if ids, _ := sentAccounts(create); !slices.Equal(ids, []string{"111111111111", "222222222222"}) {
							return fmt.Errorf("create sent accounts %v: %s", ids, create.Raw)
						}
						if n := len(api.Requests(http.MethodPut, path())); n != 0 {
							return fmt.Errorf("an enabled create sent %d updates", n)
						}
						return nil
					},
				),
			},
			{
				Config: workspaceProviderConfig(api, `  account_ids  = ["333333333333"]`+"\n"+`  is_enabled   = false`+"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "account_ids.#", "1"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "333333333333"),
					resource.TestCheckResourceAttr(address, "is_enabled", "false"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, path())
						if update.Body["workspaceId"] != testWorkspace || update.Body["isEnabled"] != false {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						if ids, _ := sentAccounts(update); !slices.Equal(ids, []string{"333333333333"}) {
							return fmt.Errorf("update sent accounts %v: %s", ids, update.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: workspaceProviderConfig(api, `  is_enabled   = false`+"\n"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr(address, "account_ids"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, path())
						if ids, sent := sentAccounts(update); !sent || len(ids) != 0 {
							return fmt.Errorf("removing account_ids sent %v (present %v), want []: %s", ids, sent, update.Raw)
						}
						if _, sent := update.Body["isEnabled"]; sent {
							return fmt.Errorf("update sent an unchanged isEnabled: %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      address,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + workspaceProviderOnlyID(t, api), nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspace-providers"); len(ids) != 0 {
				return fmt.Errorf("workspace providers left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A link created disabled is created, then switched off, because the create takes no switch.
func TestWorkspaceProviderCreatedDisabledIsSwitchedOff(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceProviderConfig(api, `  is_enabled   = false`+"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_workspace_provider.w", "is_enabled", "false"),
					resource.TestCheckNoResourceAttr("costfluent_workspace_provider.w", "account_ids"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/workspace-providers")
						if _, sent := create.Body["accountIds"]; sent {
							return fmt.Errorf("create sent accountIds for the whole connection: %s", create.Raw)
						}
						update := api.LastRequest(http.MethodPut, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api))
						if update.Body["isEnabled"] != false {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// The API resolves an account id trimmed and ignoring case and answers the spelling it stores, so a
// padded AWS account number or an upper-case Azure subscription id is the account the
// configuration wrote, kept in its spelling on create, update and refresh.
func TestWorkspaceProviderKeepsTheConfiguredSpellingOfAnAccount(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api, "111111111111", "222222222222", "0f8fad5b-d9cb-469f-a165-70867728950e")
	const address = "costfluent_workspace_provider.w"
	created := workspaceProviderConfig(api, `  account_ids  = [" 111111111111", "0F8FAD5B-D9CB-469F-A165-70867728950E"]`+"\n")
	updated := workspaceProviderConfig(api, `  account_ids  = ["111111111111 ", "\t222222222222", "0F8fad5b-D9CB-469F-A165-70867728950E"]`+"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: created,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "account_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", " 111111111111"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "0F8FAD5B-D9CB-469F-A165-70867728950E"),
				),
			},
			{Config: created, PlanOnly: true},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "account_ids.#", "3"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "111111111111 "),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "\t222222222222"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "0F8fad5b-D9CB-469F-A165-70867728950E"),
				),
			},
			{Config: updated, PlanOnly: true},
		},
	})
}

// Two spellings of one account are one account to the API, which answers it once; both stay in
// state, so the configuration plans nothing.
func TestWorkspaceProviderKeepsEverySpellingOfOneAccount(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api, "111111111111")
	const address = "costfluent_workspace_provider.w"
	config := workspaceProviderConfig(api, `  account_ids  = ["111111111111", " 111111111111"]`+"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "account_ids.#", "2"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", "111111111111"),
					resource.TestCheckTypeSetElemAttr(address, "account_ids.*", " 111111111111"),
					func(*terraform.State) error {
						stored, _ := api.Get("/v1/workspace-providers", workspaceProviderOnlyID(t, api))
						if accounts, _ := stored["accountIds"].([]any); len(accounts) != 1 {
							return fmt.Errorf("fake stored accounts %v, want the one account", accounts)
						}
						return nil
					},
				),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// An explicitly empty account_ids means the whole connection, as an omitted one does, and plans
// nothing once applied.
func TestWorkspaceProviderEmptyAccountsIsTheWholeConnection(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	config := workspaceProviderConfig(api, `  account_ids  = []`+"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("costfluent_workspace_provider.w", "account_ids.#", "0"),
			},
			{Config: config, PlanOnly: true},
		},
	})
}

// Accounts chosen outside Terraform show as drift and are cleared back to the whole connection.
func TestWorkspaceProviderAccountsChosenOutsideTerraformAreCleared(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	config := workspaceProviderConfig(api, "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					api.Update("/v1/workspace-providers", workspaceProviderOnlyID(t, api), func(obj map[string]any) {
						obj["accountIds"] = []any{"444444444444"}
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace_provider.w", plancheck.ResourceActionUpdate),
					},
				},
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api))
					if ids, sent := sentAccounts(update); !sent || len(ids) != 0 {
						return fmt.Errorf("clearing drift sent %v (present %v), want []: %s", ids, sent, update.Raw)
					}
					return nil
				},
			},
		},
	})
}

func TestWorkspaceProviderDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: workspaceProviderConfig(api, "")},
			{
				PreConfig:          func() { api.Remove("/v1/workspace-providers", workspaceProviderOnlyID(t, api)) },
				Config:             workspaceProviderConfig(api, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestWorkspaceProviderFailuresSurfaceTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	api.FailNext(http.MethodPost, "/v1/workspace-providers",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "Account 999999999999 is not part of this connection"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: workspaceProviderConfig(api, `  account_ids  = ["999999999999"]`+"\n"),
				ExpectError: regexp.MustCompile(
					`Failed to create workspace provider(.|\n)*Account 999999999999 is not part of this connection`),
			},
			{Config: workspaceProviderConfig(api, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "Account 888888888888 is not part of this connection"))
				},
				Config:      workspaceProviderConfig(api, `  account_ids  = ["888888888888"]`+"\n"),
				ExpectError: regexp.MustCompile(`Failed to update workspace provider(.|\n)*Account 888888888888`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot view providers"))
				},
				Config:      workspaceProviderConfig(api, ""),
				ExpectError: regexp.MustCompile(`Failed to read workspace provider(.|\n)*The token cannot view providers`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot manage providers"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete workspace provider(.|\n)*The token cannot manage providers`),
			},
		},
	})
}

// The link exists once the create returns, so a failed switch-off leaves it in state, tainted: the
// next apply replaces it rather than enabling the connection a second time beside it.
func TestWorkspaceProviderDisableFailureAfterCreateIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	// The fake numbers IDs from 1, so the link this test creates is the first.
	first := "wpr_" + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodPut, "/v1/workspace-providers/"+first,
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot manage providers"))
	config := workspaceProviderConfig(api, `  is_enabled   = false`+"\n")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspace-providers"); len(ids) != 0 {
				return fmt.Errorf("workspace providers left after destroy: %v", ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`Failed to disable workspace provider(.|\n)*The token cannot manage providers`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_workspace_provider.w", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if ids := api.IDs("/v1/workspace-providers"); len(ids) != 1 || ids[0] == first {
						return fmt.Errorf("workspace providers = %v, want only the replacement of %s", ids, first)
					}
					return nil
				},
			},
		},
	})
}

func TestWorkspaceProviderImportNeedsTheWorkspace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: workspaceProviderConfig(api, "")},
			{
				ResourceName:  "costfluent_workspace_provider.w",
				ImportState:   true,
				ImportStateId: "wpr_0000000000000000000001",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
	})
}

func TestWorkspaceProviderCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	links := func() string { return "/v1/workspace-providers" }
	link := func() string { return "/v1/workspace-providers/" + workspaceProviderOnlyID(t, api) }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   refuseNext(api, http.MethodPost, links, costfluent.CapabilityAddWorkspaceProvider),
				Config:      workspaceProviderConfig(api, ""),
				ExpectError: lacksCapability("Failed to create workspace provider", costfluent.CapabilityAddWorkspaceProvider),
			},
			{Config: workspaceProviderConfig(api, "")},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, link, costfluent.CapabilityRemoveWorkspaceProvider),
				Config:      workspaceProviderConfig(api, ""),
				Destroy:     true,
				ExpectError: lacksCapability("Failed to delete workspace provider", costfluent.CapabilityRemoveWorkspaceProvider),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspace-providers"); len(ids) != 0 {
				return fmt.Errorf("workspace providers left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// Which accounts a workspace reads is the organization's to decide: creating a link and changing
// its accounts are refused at plan time, before anything is written, for a token without
// manageProviders, and so is any change by a workspace token.
func TestWorkspaceProviderAccountsNeedManageProviders(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	accounts := func(ids string) string { return workspaceProviderConfig(api, "  account_ids  = "+ids+"\n") }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityManageProviders)) },
				Config:      accounts(`["111111111111"]`),
				ExpectError: refusal("costfluent_workspace_provider", "creating", costfluent.CapabilityManageProviders),
			},
			{
				PreConfig: func() {
					if n := len(api.Requests(http.MethodPost, "/v1/workspace-providers")); n != 0 {
						t.Errorf("created %d links although the plan was refused", n)
					}
					api.Me(without())
				},
				Config: accounts(`["111111111111"]`),
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityManageProviders)) },
				Config:      accounts(`["111111111111", "222222222222"]`),
				ExpectError: refusal("costfluent_workspace_provider", "updating", costfluent.CapabilityManageProviders),
			},
			{
				PreConfig:   func() { api.Me(workspaceToken()) },
				Config:      accounts(`["111111111111", "222222222222"]`),
				ExpectError: regexp.MustCompile(`(?s)` + prose("costfluent_workspace_provider needs an organization token")),
			},
			{
				PreConfig: func() {
					if n := len(api.Requests(http.MethodPut, "")); n != 0 {
						t.Errorf("sent %d updates although each plan was refused", n)
					}
					api.Me(without())
				},
				Config: accounts(`["111111111111", "222222222222"]`),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api))
					if ids, _ := sentAccounts(update); !slices.Equal(ids, []string{"111111111111", "222222222222"}) {
						return fmt.Errorf("update sent accounts %v: %s", ids, update.Raw)
					}
					return nil
				},
			},
		},
	})
}

// Linking a connection and choosing its accounts are the organization's, but switching a link and
// removing it are the workspace's: a workspace token, or an organization token without
// manageProviders, turns a link off and on and destroys it, while a workspace token is refused,
// before anything is written, creating one or changing its accounts.
func TestWorkspaceProviderWorkspaceTokenSwitchesAndRemovesButNeverLinks(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	workspaceProviderFake(api)
	config := func(extra string) string {
		return workspaceProviderConfig(api, `  account_ids  = ["111111111111"]`+"\n"+extra)
	}
	needsOrganization := regexp.MustCompile(`(?s)` + prose("costfluent_workspace_provider needs an organization token") + `.*` +
		prose("this is a workspace token for "+testWorkspace+"."))
	switched := func(want bool) resource.TestCheckFunc {
		return func(*terraform.State) error {
			update := api.LastRequest(http.MethodPut, "/v1/workspace-providers/"+workspaceProviderOnlyID(t, api))
			if update.Body["isEnabled"] != want {
				return fmt.Errorf("update body = %s, want isEnabled %v", update.Raw, want)
			}
			if _, sent := update.Body["accountIds"]; sent {
				return fmt.Errorf("switching the link sent accountIds: %s", update.Raw)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   func() { api.Me(workspaceToken()) },
				Config:      config(""),
				ExpectError: needsOrganization,
			},
			{
				PreConfig: func() {
					if n := len(api.Requests(http.MethodPost, "")); n != 0 {
						t.Errorf("created %d links although the plan was refused", n)
					}
					api.Me(without())
				},
				Config: config(""),
			},
			{
				PreConfig: func() { api.Me(workspaceToken()) },
				Config:    config("  is_enabled   = false\n"),
				Check:     switched(false),
			},
			{
				PreConfig: func() { api.Me(without(costfluent.CapabilityManageProviders)) },
				Config:    config("  is_enabled   = true\n"),
				Check:     switched(true),
			},
			{
				PreConfig:   func() { api.Me(workspaceToken()) },
				Config:      workspaceProviderConfig(api, `  account_ids  = ["111111111111", "222222222222"]`+"\n"),
				ExpectError: needsOrganization,
			},
			{
				// A replacement deletes the link and creates it again, which is linking.
				PreConfig: func() { api.Me(workspaceToken()) },
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_workspace_provider" "w" {
  workspace_id = %q
  provider_id  = "prv_0000000000000000000002"
  account_ids  = ["111111111111"]
}
`, testWorkspace),
				ExpectError: needsOrganization,
			},
			{
				// Accounts not known until apply may change, so they count as changed.
				PreConfig: func() { api.Me(workspaceToken()) },
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "terraform_data" "account" {
  input = "111111111111"
}

resource "costfluent_workspace_provider" "w" {
  workspace_id = %q
  provider_id  = %q
  account_ids  = [terraform_data.account.output]
}
`, testWorkspace, testConnection),
				ExpectError: needsOrganization,
			},
			{
				PreConfig: func() {
					if n := len(api.Requests(http.MethodPut, "")); n != 2 {
						t.Errorf("sent %d updates, want the two switches only", n)
					}
					if n := len(api.Requests(http.MethodDelete, "")) + len(api.Requests(http.MethodPost, "")); n != 1 {
						t.Errorf("sent %d creates and deletes, want the one create", n)
					}
					api.Me(workspaceToken())
				},
				Config:  config(""),
				Destroy: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/workspace-providers"); len(ids) != 0 {
				return fmt.Errorf("links left after a workspace token destroyed them: %v", ids)
			}
			return nil
		},
	})
}
