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
	savingsViewPath       = "/v1/savings-views"
	savingsViewConnection = "prv_0000000000000000000007"
)

// savingsViewFake serves /v1/savings-views as the API does: a create answers the whole view, and an
// update replaces it whole, so a filter the body leaves out is cleared.
func savingsViewFake(api *fakeapi.Server) {
	api.Collection(savingsViewPath, fakeapi.Collection{
		IDPrefix:  "svw_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{"createdAt": "2026-10-01T09:30:00Z"}
			storeSavingsView(obj, body)
			return obj
		},
		Apply: func(obj, body map[string]any) {
			storeSavingsView(obj, body)
			obj["updatedAt"] = "2026-10-02T09:30:00Z"
		},
	})
}

// storeSavingsView writes a create or update body into the stored view as the API answers it: the
// enums it reads in any case in camelCase, the minimum at eight decimals, and a filter the body
// leaves out as none.
func storeSavingsView(obj, body map[string]any) {
	obj["title"] = body["title"]
	obj["tab"] = camelCase(body["tab"])
	obj["source"] = camelCase(body["source"])
	obj["kind"] = camelCase(body["kind"])
	obj["cloudAccountId"] = body["cloudAccountId"]
	obj["minimumMonthlySaving"] = eightDecimals(body["minimumMonthlySaving"])
}

func camelCase(sent any) any {
	text, ok := sent.(string)
	if !ok || text == "" {
		return sent
	}
	return strings.ToLower(text[:1]) + text[1:]
}

func savingsViewConfig(api *fakeapi.Server, title, extra string) string {
	return savingsViewConfigWith(api, title, "toReview", extra)
}

func savingsViewConfigWith(api *fakeapi.Server, title, tab, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_savings_view" "v" {
  workspace_id = %q
  title        = %q
  tab          = %q
%s}
`, testWorkspace, title, tab, extra)
}

// savingsViewFilters sets every filter a view keeps, the source in another case than the API answers
// it, and the minimum at a scale the API does not answer it at.
const savingsViewFilters = `  source                 = "AwsCostOptimizationHub"
  kind                   = "rightsizing"
  cloud_account_id       = "` + savingsViewConnection + `"
  minimum_monthly_saving = "100.5"
`

func savingsViewOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs(savingsViewPath)
	if len(ids) != 1 {
		t.Fatalf("savings views = %v, want one", ids)
	}
	return ids[0]
}

func TestSavingsViewLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savingsViewFake(api)
	const address = "costfluent_savings_view.v"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// The API answers the source in camelCase and the minimum at eight decimals; state keeps
				// both as configured, so the plan the step runs after apply stays empty.
				Config: savingsViewConfig(api, "Rightsizing over 100", savingsViewFilters),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestMatchResourceAttr(address, "id", regexp.MustCompile(`^svw_.{22}$`)),
					resource.TestCheckResourceAttr(address, "workspace_id", testWorkspace),
					resource.TestCheckResourceAttr(address, "title", "Rightsizing over 100"),
					resource.TestCheckResourceAttr(address, "tab", "toReview"),
					resource.TestCheckResourceAttr(address, "source", "AwsCostOptimizationHub"),
					resource.TestCheckResourceAttr(address, "kind", "rightsizing"),
					resource.TestCheckResourceAttr(address, "cloud_account_id", savingsViewConnection),
					resource.TestCheckResourceAttr(address, "minimum_monthly_saving", "100.5"),
					resource.TestCheckResourceAttr(address, "created_at", "2026-10-01T09:30:00Z"),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, savingsViewPath)
						want := map[string]any{
							"workspaceId": testWorkspace, "title": "Rightsizing over 100", "tab": "toReview",
							"source": "AwsCostOptimizationHub", "kind": "rightsizing",
							"cloudAccountId": savingsViewConnection, "minimumMonthlySaving": "100.5",
						}
						if fmt.Sprint(create.Body) != fmt.Sprint(want) {
							return fmt.Errorf("create body = %s, want %v", create.Raw, want)
						}
						return nil
					},
				),
			},
			{
				// Removing every filter replaces the view with none: the update sends the title and tab
				// alone, and the API clears the rest.
				Config: savingsViewConfigWith(api, "Everything in progress", "inProgress", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(address, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "title", "Everything in progress"),
					resource.TestCheckResourceAttr(address, "tab", "inProgress"),
					resource.TestCheckNoResourceAttr(address, "source"),
					resource.TestCheckNoResourceAttr(address, "kind"),
					resource.TestCheckNoResourceAttr(address, "cloud_account_id"),
					resource.TestCheckNoResourceAttr(address, "minimum_monthly_saving"),
					func(*terraform.State) error {
						id := savingsViewOnlyID(t, api)
						read := api.LastRequest(http.MethodGet, savingsViewPath+"/"+id)
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						update := api.LastRequest(http.MethodPut, savingsViewPath+"/"+id)
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						want := map[string]any{"title": "Everything in progress", "tab": "inProgress"}
						if fmt.Sprint(update.Body) != fmt.Sprint(want) {
							return fmt.Errorf("update body = %s, want only %v", update.Raw, want)
						}
						stored, _ := api.Get(savingsViewPath, id)
						for _, cleared := range []string{"source", "kind", "cloudAccountId", "minimumMonthlySaving"} {
							if stored[cleared] != nil {
								return fmt.Errorf("stored %s = %v after the update, want it cleared", cleared, stored[cleared])
							}
						}
						return nil
					},
				),
			},
			{
				// Setting the filters again sends the whole view, the unchanged title and tab included.
				Config: savingsViewConfigWith(api, "Everything in progress", "inProgress", savingsViewFilters),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(address, "source", "AwsCostOptimizationHub"),
					resource.TestCheckResourceAttr(address, "minimum_monthly_saving", "100.5"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPut, savingsViewPath+"/"+savingsViewOnlyID(t, api))
						want := map[string]any{
							"title": "Everything in progress", "tab": "inProgress", "source": "AwsCostOptimizationHub",
							"kind": "rightsizing", "cloudAccountId": savingsViewConnection, "minimumMonthlySaving": "100.5",
						}
						if fmt.Sprint(update.Body) != fmt.Sprint(want) {
							return fmt.Errorf("update body = %s, want %v", update.Raw, want)
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
					return testWorkspace + ":" + savingsViewOnlyID(t, api), nil
				},
				// An import has no configured spelling to keep, so it reads the source as the API answers it.
				ImportStateVerifyIgnore: []string{"source"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["source"]; got != "awsCostOptimizationHub" {
						return fmt.Errorf("imported source = %q, want the API's awsCostOptimizationHub", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs(savingsViewPath); len(ids) != 0 {
				return fmt.Errorf("savings views left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A bare ID imports into the provider's workspace, which the read then sends as workspaceId. An
// import has no configured spelling to keep, so it reads the minimum in its shortest form, the way it
// is usually written.
func TestSavingsViewImportsABareIDInTheProviderWorkspace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savingsViewFake(api)
	const id = "svw_0000000000000000000099"
	api.PutIn(savingsViewPath, testWorkspace, id, map[string]any{
		"title": "Idle", "tab": "toReview", "kind": "idle", "minimumMonthlySaving": "250.00000000",
		"createdAt": "2026-10-01T09:30:00Z",
	})
	config := api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + `
resource "costfluent_savings_view" "v" {
  title                  = "Idle"
  tab                    = "toReview"
  kind                   = "idle"
  minimum_monthly_saving = "250"
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:        config,
				ResourceName:  "costfluent_savings_view.v",
				ImportState:   true,
				ImportStateId: id,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if len(states) != 1 {
						return fmt.Errorf("imported %d states, want one", len(states))
					}
					got := states[0].Attributes
					if got["workspace_id"] != testWorkspace || got["kind"] != "idle" || got["minimum_monthly_saving"] != "250" {
						return fmt.Errorf("imported = %v", got)
					}
					if _, set := got["source"]; set {
						return fmt.Errorf("imported source = %q, want none", got["source"])
					}
					read := api.LastRequest(http.MethodGet, savingsViewPath+"/"+id)
					if read.Query.Get("workspaceId") != testWorkspace {
						return fmt.Errorf("read query = %v", read.Query)
					}
					return nil
				},
			},
		},
	})
}

// A view deleted outside Terraform reads as 404, leaves the state, and is planned again.
func TestSavingsViewDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savingsViewFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: savingsViewConfig(api, "Idle", "")},
			{
				PreConfig: func() { api.Remove(savingsViewPath, savingsViewOnlyID(t, api)) },
				Config:    savingsViewConfig(api, "Idle", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_savings_view.v", plancheck.ResourceActionCreate),
					},
				},
				Check: func(*terraform.State) error {
					if creates := api.Requests(http.MethodPost, savingsViewPath); len(creates) != 2 {
						return fmt.Errorf("creates = %d, want the view created again", len(creates))
					}
					return nil
				},
			},
		},
	})
}

// An update or a deletion sends back, as If-Match, the ETag of the read the plan was made from, so a
// change made in the product after it is refused rather than overwritten, and reads as a change
// outside Terraform.
func TestSavingsViewUpdateSendsTheVersionItReadAndStopsOnAnOutsideChange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savingsViewFake(api)
	var readTag string
	// elsewhere renames the view just before Terraform's next write, after the read it planned from.
	elsewhere := func(method string) func() {
		return func() {
			id := savingsViewOnlyID(t, api)
			api.BeforeNext(method, savingsViewPath+"/"+id, func() {
				api.Update(savingsViewPath, id, func(obj map[string]any) { obj["title"] = "Renamed in Costfluent" })
			})
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: savingsViewConfig(api, "Idle", ""),
				Check: func(*terraform.State) error {
					readTag = api.ETag(savingsViewPath, savingsViewOnlyID(t, api))
					return nil
				},
			},
			{
				Config: savingsViewConfig(api, "Idle renamed", ""),
				Check: func(*terraform.State) error {
					update := api.LastRequest(http.MethodPut, savingsViewPath+"/"+savingsViewOnlyID(t, api))
					if got := update.Header.Get("If-Match"); got == "" || got != readTag {
						return fmt.Errorf("update sent If-Match %q, want the ETag of the last read %q", got, readTag)
					}
					return nil
				},
			},
			{
				PreConfig:   elsewhere(http.MethodPut),
				Config:      savingsViewConfig(api, "Idle renamed again", ""),
				ExpectError: regexp.MustCompile(`Savings view changed outside Terraform`),
			},
			{
				PreConfig:   elsewhere(http.MethodDelete),
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Savings view changed outside Terraform(.|\n)*deleting`),
			},
			{
				// The next plan reads the change and the deletion applies.
				Config: api.ProviderBlock(),
				Check: func(*terraform.State) error {
					if ids := api.IDs(savingsViewPath); len(ids) != 0 {
						return fmt.Errorf("savings views = %v, want the next apply to delete it", ids)
					}
					return nil
				},
			},
		},
	})
}

// The API's refusal of a view, such as a tab it does not know, is the apply's error.
func TestSavingsViewAPIValidationErrorsSurface(t *testing.T) {
	t.Parallel()
	for _, step := range []struct{ name, method, detail, summary string }{
		{"create", http.MethodPost, "The tab 'pending' is unknown", "Failed to create savings view"},
		{"update", http.MethodPut, "The minimum must not be negative", "Failed to update savings view"},
	} {
		t.Run(step.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			savingsViewFake(api)
			failNext := func() {
				path := savingsViewPath
				if step.method != http.MethodPost {
					path += "/" + savingsViewOnlyID(t, api)
				}
				api.FailNext(step.method, path, fakeapi.Problem(http.StatusBadRequest, "Validation", step.detail))
			}
			steps := []resource.TestStep{{Config: savingsViewConfig(api, "Idle", "")}}
			if step.method == http.MethodPost {
				steps = nil
			}
			steps = append(steps, resource.TestStep{
				PreConfig:   failNext,
				Config:      savingsViewConfig(api, "Idle renamed", ""),
				ExpectError: regexp.MustCompile(step.summary + `(.|\n)*` + regexp.QuoteMeta(step.detail)),
			})

			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: factories, Steps: steps})
		})
	}
}

func TestSavingsViewReadFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	savingsViewFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: savingsViewConfig(api, "Idle", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, savingsViewPath+"/"+savingsViewOnlyID(t, api),
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot read it"))
				},
				Config:      savingsViewConfig(api, "Idle", ""),
				ExpectError: regexp.MustCompile(`Failed to read savings view(.|\n)*The token cannot read it`),
			},
		},
	})
}

func TestSavingsViewRejectsInvalidConfigurationAtPlan(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ title, extra, want string }{
		"a workspace without its prefix":  {"Idle", "  workspace_id = \"fld_0000000000000000000001\"\n", `Invalid token format`},
		"a connection without its prefix": {"Idle", "  cloud_account_id = \"wsp_0000000000000000000001\"\n", `Invalid token format`},
		"a minimum that is no amount":     {"Idle", "  minimum_monthly_saving = \"100 EUR\"\n", `Invalid amount`},
		"a minimum with nine decimals":    {"Idle", "  minimum_monthly_saving = \"0.123456789\"\n", `Invalid amount`},
		"an empty title":                  {"", "", `string length must be between 1 and 200`},
		"a title over 200 characters":     {strings.Repeat("x", 201), "", `string length must be between 1 and 200`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			config := api.ProviderBlock(fmt.Sprintf("workspace = %q", testWorkspace)) + fmt.Sprintf(`
resource "costfluent_savings_view" "v" {
  title = %q
  tab   = "toReview"
%s}
`, tc.title, tc.extra)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps:                    []resource.TestStep{{Config: config, PlanOnly: true, ExpectError: regexp.MustCompile(tc.want)}},
			})
		})
	}
}
