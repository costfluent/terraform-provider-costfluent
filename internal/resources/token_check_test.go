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
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	tfresource "github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// without is an organization token carrying every capability but the ones named.
func without(lacking ...costfluent.Capability) costfluent.Me {
	var held []costfluent.Capability
	for _, c := range costfluent.Capabilities() {
		if !slices.Contains(lacking, c) {
			held = append(held, c)
		}
	}
	return costfluent.Me{TokenType: costfluent.TokenTypeOrganization, TokenName: "narrow", Capabilities: held}
}

// workspaceToken is a workspace token carrying every capability.
func workspaceToken() costfluent.Me {
	workspace := testWorkspace
	return costfluent.Me{TokenType: costfluent.TokenTypeWorkspace, TokenName: "workspace", WorkspaceID: &workspace,
		Capabilities: costfluent.Capabilities()}
}

// prose matches a sentence however Terraform wraps it.
func prose(sentence string) string {
	words := strings.Fields(regexp.QuoteMeta(sentence))
	return strings.Join(words, `\s+`)
}

// refusal matches the plan error naming what a token lacks for an action.
func refusal(name, action string, lacking costfluent.Capability) *regexp.Regexp {
	return regexp.MustCompile(`(?s)` + prose("The API token cannot apply "+name) + `.*` +
		prose("lacks `"+string(lacking)+"`") + `.*` + prose(action+" "+name+" needs") + `.*` + prose("Settings, API tokens."))
}

// lacksCapability matches the diagnostic of a write the API refused for a capability the token
// lacks: the action's title, the capability and where to create a token that carries it.
func lacksCapability(title string, capability costfluent.Capability) *regexp.Regexp {
	return regexp.MustCompile(`(?s)` + prose(title) + `.*` + prose("The API token lacks `"+string(capability)+"`;") + `.*` +
		prose("Settings, API tokens."))
}

// refuseNext makes the next request to method and path a 403 for capability.
func refuseNext(api *fakeapi.Server, method string, path func() string, capability costfluent.Capability) func() {
	return func() { api.FailNext(method, path(), fakeapi.MissingCapability(capability)) }
}

// noWrites fails when the fake received anything but a read.
func noWrites(api *fakeapi.Server) error {
	for _, r := range api.Requests("", "") {
		if r.Method != http.MethodGet {
			return fmt.Errorf("the API received %s %s although the plan was refused", r.Method, r.Path)
		}
	}
	return nil
}

func TestTokenCheckRefusesAPlanBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		organization bool
		fake         func(*fakeapi.Server)
		config       func(*fakeapi.Server) string
		create       costfluent.Capability
	}{
		{"costfluent_team", true, func(api *fakeapi.Server) { teamFake(api) },
			func(api *fakeapi.Server) string { return teamConfig(api, "Platform", "") }, costfluent.CapabilityCreateTeam},
		{"costfluent_team_member", true, func(api *fakeapi.Server) { teamMemberFake(api) },
			func(api *fakeapi.Server) string { return teamMemberConfig(api, "viewer") }, costfluent.CapabilityManageTeam},
		{"costfluent_team_workspace_access", true, func(api *fakeapi.Server) { teamFake(api) },
			func(api *fakeapi.Server) string { return teamConfig(api, "Platform", "canView") }, costfluent.CapabilityManageWorkspaceAccess},
		{"costfluent_app", true, appFake,
			func(api *fakeapi.Server) string {
				return appConfigBlock(api, "Ops", fmt.Sprintf(`{ webhookUrl = %q }`, slackHook))
			}, costfluent.CapabilityCreateApp},
		{"costfluent_workspace_provider", true, func(api *fakeapi.Server) { workspaceProviderFake(api) },
			func(api *fakeapi.Server) string { return workspaceProviderConfig(api, "") }, costfluent.CapabilityAddWorkspaceProvider},
		{"costfluent_workspace", true, fakeWorkspaces,
			func(api *fakeapi.Server) string { return workspaceConfig(api, "Finance", "") }, costfluent.CapabilityCreateWorkspace},
		{"costfluent_budget", false, fakeBudgets,
			func(api *fakeapi.Server) string { return budgetConfig(api, "Platform", "500", "") }, costfluent.CapabilityCreateBudget},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			tc.fake(api)

			steps := []tfresource.TestStep{{
				PreConfig:   func() { api.Me(without(tc.create)) },
				Config:      tc.config(api),
				ExpectError: refusal(tc.name, "creating", tc.create),
			}}
			if tc.organization {
				// The access's team is refused first and nothing depending on it is planned.
				refused := strings.TrimSuffix(tc.name, "_workspace_access")
				steps = append(steps, tfresource.TestStep{
					PreConfig: func() { api.Me(workspaceToken()) },
					Config:    tc.config(api),
					ExpectError: regexp.MustCompile(`(?s)` + prose(refused+" needs an organization token") + `.*` +
						prose("this is a workspace token for "+testWorkspace+".")),
				})
			}
			steps = append(steps, tfresource.TestStep{
				PreConfig: func() {
					if err := noWrites(api); err != nil {
						t.Error(err)
					}
					api.Me(without())
				},
				Config: tc.config(api),
			})
			tfresource.UnitTest(t, tfresource.TestCase{ProtoV6ProviderFactories: factories, Steps: steps})
		})
	}
}

// Each action is checked against what it sends: an update against the update capability, a
// destroy against the delete one, a replacement against the create one, and a plan that changes
// nothing against none.
func TestTokenCheckNamesWhatEachActionNeeds(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig: func() {
					api.Me(costfluent.Me{TokenType: costfluent.TokenTypeOrganization, Capabilities: []costfluent.Capability{costfluent.CapabilityViewMembers}})
				},
				Config:   teamConfig(api, "Platform", ""),
				PlanOnly: true,
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityManageTeam)) },
				Config:      teamConfig(api, "Renamed", ""),
				ExpectError: refusal("costfluent_team", "updating", costfluent.CapabilityManageTeam),
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityDeleteTeam)) },
				Config:      teamConfig(api, "Platform", ""),
				Destroy:     true,
				ExpectError: refusal("costfluent_team", "destroying", costfluent.CapabilityDeleteTeam),
			},
			{
				PreConfig: func() { api.Me(without()) },
				Config:    teamConfig(api, "Platform", ""),
				Check: func(*terraform.State) error {
					if got := api.Requests(http.MethodPut, ""); len(got) != 0 {
						return fmt.Errorf("renamed although the token could not: %+v", got)
					}
					if len(api.IDs("/v1/teams")) != 1 {
						return fmt.Errorf("teams = %v, want the one created", api.IDs("/v1/teams"))
					}
					return nil
				},
			},
		},
	})
}

// A replacement sends a delete and a create, never an update: it is checked against those, so a
// token without the update capability replaces, and one without the delete or the create one is
// refused before the old object is deleted.
func TestTokenCheckChecksAReplacementAgainstDeleteAndCreate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeBudgets(api)
	alert := func(percent int) string { return fmt.Sprintf("  alerts = [{ threshold_percent = %d }]\n", percent) }

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{Config: budgetConfig(api, "Platform", "500", alert(75))},
			{
				PreConfig: func() { api.Me(without(costfluent.CapabilityUpdateBudget)) },
				Config:    budgetConfig(api, "Platform", "500", alert(80)),
				Check: func(*terraform.State) error {
					if got := api.Requests(http.MethodDelete, ""); len(got) != 1 {
						return fmt.Errorf("deletes = %+v, want the replaced budget's", got)
					}
					return nil
				},
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityDeleteBudget)) },
				Config:      budgetConfig(api, "Platform", "500", alert(90)),
				ExpectError: refusal("costfluent_budget", "replacing", costfluent.CapabilityDeleteBudget),
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityCreateBudget)) },
				Config:      budgetConfig(api, "Platform", "500", alert(90)),
				ExpectError: refusal("costfluent_budget", "replacing", costfluent.CapabilityCreateBudget),
			},
			{
				PreConfig: func() {
					if got := api.Requests(http.MethodDelete, ""); len(got) != 1 {
						t.Errorf("deleted a budget although its replacement was refused: %+v", got)
					}
					api.Me(without())
				},
				Config:   budgetConfig(api, "Platform", "500", alert(80)),
				PlanOnly: true,
			},
		},
	})
}

// A replacement forced by a modifier that replaces only on some changes is told from an update
// the same way: changing a dashboard widget's type replaces it, and respelling the type in
// another case updates it, so a token without updateDashboard is refused each with its own verb.
func TestTokenCheckTellsAConditionalReplacementFromAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	widgetFake(api)
	report := reportWidget(testReport, "Spend", "  type = \"costReport\"\n")
	text := "  type     = \"Text\"\n  title    = \"Spend\"\n  content  = \"Notes\"\n  position = { x = 0, y = 0, w = 6, h = 4 }\n"

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{Config: widgetConfig(api, report)},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityUpdateDashboard)) },
				Config:      widgetConfig(api, text),
				ExpectError: refusal("costfluent_dashboard_widget", "replacing", costfluent.CapabilityUpdateDashboard),
			},
			{
				PreConfig:   func() { api.Me(without(costfluent.CapabilityUpdateDashboard)) },
				Config:      widgetConfig(api, reportWidget(testReport, "Spend", "  type = \"CostReport\"\n")),
				ExpectError: refusal("costfluent_dashboard_widget", "updating", costfluent.CapabilityUpdateDashboard),
			},
			{
				PreConfig: func() { api.Me(without()) },
				Config:    widgetConfig(api, report),
				PlanOnly:  true,
			},
		},
	})
}

// A call only some configurations send is required exactly when the plan sends it: a dashboard
// created with a saved filter attaches it with updateDashboard, one created without does not.
func TestTokenCheckRequiresTheConditionalCallsThePlanSends(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		config  func(*fakeapi.Server) string
		lacking costfluent.Capability
	}{
		{"costfluent_dashboard", func(api *fakeapi.Server) string {
			return dashboardWithFilterConfig(api, "flt_0000000000000000000001")
		}, costfluent.CapabilityUpdateDashboard},
		{"costfluent_folder", func(api *fakeapi.Server) string {
			return folderWithFilterConfig(api, "flt_0000000000000000000001")
		}, costfluent.CapabilityUpdateFolder},
		{"costfluent_workspace", func(api *fakeapi.Server) string {
			return workspaceConfig(api, "Finance", "  enable_automatic_syncing = false\n")
		}, costfluent.CapabilityUpdateWorkspace},
		{"costfluent_workspace_provider", func(api *fakeapi.Server) string {
			return workspaceProviderConfig(api, "  is_enabled = false\n")
		}, costfluent.CapabilityRemoveWorkspaceProvider},
		{"costfluent_cost_report", func(api *fakeapi.Server) string {
			return costReportConfig(api, fmt.Sprintf("  title  = \"EC2\"\n  filter = %q\n", virtualTagQuery))
		}, costfluent.CapabilityViewCosts},
		{"costfluent_allocation_rule", func(api *fakeapi.Server) string {
			return allocationRuleConfig(api, virtualTagQuery, "")
		}, costfluent.CapabilityViewCosts},
		{"costfluent_virtual_tag", func(api *fakeapi.Server) string {
			return virtualTagConfig(api, "")
		}, costfluent.CapabilityViewCosts},
		{"costfluent_saved_filter", func(api *fakeapi.Server) string {
			return savedFilterConfig(api, "EC2", "")
		}, costfluent.CapabilityViewCosts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			api.Me(without(tc.lacking))

			tfresource.UnitTest(t, tfresource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []tfresource.TestStep{{
					Config:      tc.config(api),
					ExpectError: refusal(tc.name, "creating", tc.lacking),
				}},
			})
			if err := noWrites(api); err != nil {
				t.Error(err)
			}
		})
	}

	t.Run("costfluent_dashboard without a saved filter", func(t *testing.T) {
		t.Parallel()
		api := fakeapi.New(t)
		dashboardFake(api)
		api.Me(without(costfluent.CapabilityUpdateDashboard))

		tfresource.UnitTest(t, tfresource.TestCase{
			ProtoV6ProviderFactories: factories,
			Steps:                    []tfresource.TestStep{{Config: dashboardWithFilterConfig(api, "")}},
		})
	})
}

// A workspace token reaches its own workspace only, so a plan for a resource in another one is
// refused before anything is sent; an organization token reaches every workspace.
func TestTokenCheckRefusesAWorkspaceTokenForAnotherWorkspace(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	dashboardFake(api)
	other := otherWorkspace
	elsewhere := workspaceToken()
	elsewhere.WorkspaceID = &other

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{
			{
				PreConfig: func() { api.Me(elsewhere) },
				Config:    dashboardConfig(api, "Overview", ""),
				ExpectError: regexp.MustCompile(`(?s)` + prose("The API token cannot reach the workspace of costfluent_dashboard") + `.*` +
					prose("costfluent_dashboard is in workspace "+testWorkspace+"; this is a workspace token for "+otherWorkspace+".")),
			},
			{
				PreConfig: func() {
					if err := noWrites(api); err != nil {
						t.Error(err)
					}
					api.Me(workspaceToken())
				},
				Config: dashboardConfig(api, "Overview", ""),
			},
			{
				PreConfig: func() { api.Me(without()) },
				Config:    dashboardConfig(api, "Renamed", ""),
			},
		},
	})
}

// A call only some configurations send is not required at plan: a workspace on the default
// settings is created without updateWorkspace, which only applying other settings needs.
func TestTokenCheckDoesNotRequireWhatOnlySomeConfigurationsSend(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	fakeWorkspaces(api)
	api.Me(without(costfluent.CapabilityUpdateWorkspace))

	tfresource.UnitTest(t, tfresource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []tfresource.TestStep{{
			Config: workspaceConfig(api, "Finance", ""),
			Check: func(*terraform.State) error {
				if len(api.IDs("/v1/workspaces")) != 1 {
					return fmt.Errorf("workspaces = %v, want the one created", api.IDs("/v1/workspaces"))
				}
				return nil
			},
		}},
	})
}

// A resource whose provider was not configured, as during validation, checks nothing.
func TestTokenCheckSkipsAnUnconfiguredProvider(t *testing.T) {
	t.Parallel()
	resp := &resource.ModifyPlanResponse{}
	resources.CheckToken(t.Context(), nil, "costfluent_team", resources.Needs{Create: []costfluent.Capability{costfluent.CapabilityCreateTeam}},
		resource.ModifyPlanRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("diagnostics = %v", resp.Diagnostics)
	}
}

func TestEveryResourceChecksItsToken(t *testing.T) {
	t.Parallel()
	for _, newResource := range provider.New("test")().Resources(t.Context()) {
		if _, ok := newResource().(resource.ResourceWithModifyPlan); !ok {
			t.Errorf("%T does not check its token at plan", newResource())
		}
	}
}
