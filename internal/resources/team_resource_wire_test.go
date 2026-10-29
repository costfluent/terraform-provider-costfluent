package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const teamWorkspace = "wsp_0000000000000000000002"

// teamAccess is the workspace access the fake holds per team, and each grant's default dashboard;
// Terraform refreshes in parallel.
type teamAccess struct {
	mu       sync.Mutex
	levels   map[string]map[string]string
	defaults map[string]map[string]string
}

func (a *teamAccess) level(team, workspace string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.levels[team][workspace]
}

func (a *teamAccess) remove(team, workspace string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.levels[team], workspace)
	delete(a.defaults[team], workspace)
}

func (a *teamAccess) defaultDashboard(team, workspace string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.defaults[team][workspace]
}

// setDefault is a default set in the console; an empty dashboard clears it.
func (a *teamAccess) setDefault(team, workspace, dashboard string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.defaults[team] == nil {
		a.defaults[team] = map[string]string{}
	}
	if dashboard == "" {
		delete(a.defaults[team], workspace)
		return
	}
	a.defaults[team][workspace] = dashboard
}

// grantView is a grant as the API answers it, defaultDashboardId null for none. The caller holds mu.
func (a *teamAccess) grantView(team, workspace string) map[string]any {
	var dashboard any
	if id, ok := a.defaults[team][workspace]; ok {
		dashboard = id
	}
	return map[string]any{"workspaceId": workspace, "workspaceName": "Prod", "accessLevel": a.levels[team][workspace],
		"defaultDashboardId": dashboard}
}

// teamFake serves /v1/teams with TeamResponse on create, list and update, TeamDetailResponse on a
// read, and the workspace access of each team under /v1/teams/{id}/workspaces, with its default
// dashboard, which a level change keeps and removing the grant ends.
func teamFake(api *fakeapi.Server) *teamAccess {
	access := &teamAccess{levels: map[string]map[string]string{}, defaults: map[string]map[string]string{}}
	api.Collection("/v1/teams", fakeapi.Collection{
		IDPrefix:  "team_",
		Versioned: true,
		Create: func(body map[string]any) map[string]any {
			return map[string]any{"name": body["name"], "isSystemTeam": false, "memberCount": 0, "createdAt": "2026-10-01T09:30:00Z"}
		},
		View: func(obj map[string]any) any {
			detail := map[string]any{"members": []any{}, "workspaceAccess": []any{}}
			for k, v := range obj {
				detail[k] = v
			}
			return detail
		},
	})
	api.Handle("GET /v1/teams/{id}/workspaces", func(r fakeapi.Request) fakeapi.Response {
		if _, ok := api.Get("/v1/teams", r.PathValue("id")); !ok {
			return fakeapi.NotFound()
		}
		access.mu.Lock()
		defer access.mu.Unlock()
		items := []any{}
		for workspace := range access.levels[r.PathValue("id")] {
			items = append(items, access.grantView(r.PathValue("id"), workspace))
		}
		return fakeapi.Page(r, "/v1/teams/"+r.PathValue("id")+"/workspaces", items)
	})
	api.Handle("PUT /v1/teams/{id}/workspaces/{workspace}", func(r fakeapi.Request) fakeapi.Response {
		if _, ok := api.Get("/v1/teams", r.PathValue("id")); !ok {
			return fakeapi.NotFound()
		}
		access.mu.Lock()
		defer access.mu.Unlock()
		if access.levels[r.PathValue("id")] == nil {
			access.levels[r.PathValue("id")] = map[string]string{}
		}
		access.levels[r.PathValue("id")][r.PathValue("workspace")] = r.Body["accessLevel"].(string)
		return fakeapi.NoContent()
	})
	api.Handle("PUT /v1/teams/{id}/workspaces/{workspace}/default-dashboard", func(r fakeapi.Request) fakeapi.Response {
		team, workspace := r.PathValue("id"), r.PathValue("workspace")
		dashboard, sent := r.Body["dashboardId"]
		if !sent {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "dashboardId is required, null to clear")
		}
		level := access.level(team, workspace)
		if level == "" {
			return fakeapi.NotFound()
		}
		if dashboard != nil && level == "noAccess" {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "A team without access to the workspace has no default dashboard there")
		}
		id, _ := dashboard.(string)
		access.setDefault(team, workspace, id)
		access.mu.Lock()
		defer access.mu.Unlock()
		return fakeapi.JSON(access.grantView(team, workspace))
	})
	api.Handle("DELETE /v1/teams/{id}/workspaces/{workspace}", func(r fakeapi.Request) fakeapi.Response {
		if access.level(r.PathValue("id"), r.PathValue("workspace")) == "" {
			return fakeapi.NotFound()
		}
		access.remove(r.PathValue("id"), r.PathValue("workspace"))
		return fakeapi.NoContent()
	})
	return access
}

func teamConfig(api *fakeapi.Server, name, level string) string {
	config := api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_team" "t" {
  name = %q
}
`, name)
	if level != "" {
		config += fmt.Sprintf(`
resource "costfluent_team_workspace_access" "a" {
  team_id      = costfluent_team.t.id
  workspace_id = %q
  access_level = %q
}
`, teamWorkspace, level)
	}
	return config
}

func teamOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/teams")
	if len(ids) != 1 {
		t.Fatalf("teams = %v, want one", ids)
	}
	return ids[0]
}

func TestTeamAndWorkspaceAccessLifecycle(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	access := teamFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: teamConfig(api, "Platform", "canView"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team.t", "name", "Platform"),
					resource.TestCheckResourceAttr("costfluent_team.t", "created_at", "2026-10-01T09:30:00Z"),
					resource.TestMatchResourceAttr("costfluent_team.t", "id", regexp.MustCompile(`^team_.{22}$`)),
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "access_level", "canView"),
					func(*terraform.State) error {
						team := teamOnlyID(t, api)
						if got := access.level(team, teamWorkspace); got != "canView" {
							return fmt.Errorf("access = %v", access.levels)
						}
						return nil
					},
				),
			},
			{
				Config: teamConfig(api, "Platform engineering", "canEdit"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team.t", "name", "Platform engineering"),
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "access_level", "canEdit"),
					func(*terraform.State) error {
						team := teamOnlyID(t, api)
						rename := api.LastRequest(http.MethodPut, "/v1/teams/"+team)
						if rename.Body["name"] != "Platform engineering" {
							return fmt.Errorf("rename body = %s", rename.Raw)
						}
						if access.level(team, teamWorkspace) != "canEdit" {
							return fmt.Errorf("access = %v", access.levels)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_team.t",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "costfluent_team_workspace_access.a",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return teamOnlyID(t, api) + ":" + teamWorkspace, nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/teams"); len(ids) != 0 {
				return fmt.Errorf("teams left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func TestTeamWorkspaceAccessRemovedOutsideTerraformIsSetAgain(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	access := teamFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamConfig(api, "Platform", "canView")},
			{
				PreConfig:          func() { access.remove(teamOnlyID(t, api), teamWorkspace) },
				Config:             teamConfig(api, "Platform", "canView"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// teamDefaultDashboardConfig is a team granted level on the workspace, with dashboard as its default
// there when not empty.
func teamDefaultDashboardConfig(api *fakeapi.Server, level, dashboard string) string {
	attr := ""
	if dashboard != "" {
		attr = fmt.Sprintf("  default_dashboard_id = %q\n", dashboard)
	}
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_team" "t" {
  name = "Platform"
}

resource "costfluent_team_workspace_access" "a" {
  team_id      = costfluent_team.t.id
  workspace_id = %q
  access_level = %q
%s}
`, teamWorkspace, level, attr)
}

func TestTeamWorkspaceAccessDefaultDashboard(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	access := teamFake(api)
	const first, second, console = "dsh_0000000000000000000001", "dsh_0000000000000000000002", "dsh_0000000000000000000003"
	grant := func() string { return "/v1/teams/" + teamOnlyID(t, api) + "/workspaces/" + teamWorkspace }

	// sent checks every default-dashboard call so far, by the dashboardId each sent, nil for a clear,
	// and how many times the access level was set.
	sent := func(levelCalls int, dashboards ...any) resource.TestCheckFunc {
		return func(*terraform.State) error {
			if calls := api.Requests(http.MethodPut, grant()); len(calls) != levelCalls {
				return fmt.Errorf("access level set %d times, want %d", len(calls), levelCalls)
			}
			calls := api.Requests(http.MethodPut, grant()+"/default-dashboard")
			if len(calls) != len(dashboards) {
				return fmt.Errorf("default-dashboard called %d times, want %d", len(calls), len(dashboards))
			}
			for i, call := range calls {
				if value, ok := call.Body["dashboardId"]; !ok || value != dashboards[i] {
					return fmt.Errorf("default-dashboard call %d sent %s, want dashboardId %v", i, call.Raw, dashboards[i])
				}
			}
			return nil
		}
	}
	stored := func(level, dashboard string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			team := teamOnlyID(t, api)
			if got := access.level(team, teamWorkspace); got != level {
				return fmt.Errorf("access level = %q, want %q", got, level)
			}
			if got := access.defaultDashboard(team, teamWorkspace); got != dashboard {
				return fmt.Errorf("default dashboard = %q, want %q", got, dashboard)
			}
			return nil
		}
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// The API refuses a default on a noAccess grant, so the plan does, before the grant is written.
				Config:      teamDefaultDashboardConfig(api, "noAccess", first),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`No default dashboard without access`),
			},
			{
				Config: teamDefaultDashboardConfig(api, "canView", first),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "default_dashboard_id", first),
					sent(1, first),
					stored("canView", first),
				),
			},
			{
				ResourceName:      "costfluent_team_workspace_access.a",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return teamOnlyID(t, api) + ":" + teamWorkspace, nil
				},
			},
			{
				Config: teamDefaultDashboardConfig(api, "canView", second),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "default_dashboard_id", second),
					sent(1, first, second),
					stored("canView", second),
				),
			},
			{
				// The access call keeps the default, so a level change sends no default call.
				Config: teamDefaultDashboardConfig(api, "canEdit", second),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "access_level", "canEdit"),
					resource.TestCheckResourceAttr("costfluent_team_workspace_access.a", "default_dashboard_id", second),
					sent(2, first, second),
					stored("canEdit", second),
				),
			},
			{
				PreConfig:          func() { access.setDefault(teamOnlyID(t, api), teamWorkspace, console) },
				Config:             teamDefaultDashboardConfig(api, "canEdit", second),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: teamDefaultDashboardConfig(api, "canEdit", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_team_workspace_access.a", "default_dashboard_id"),
					sent(2, first, second, nil),
					stored("canEdit", ""),
				),
			},
		},
	})
}

// A grant whose default cannot be set, created or updated, is still recorded with the level
// written and the default it had, so the next apply sets the default rather than losing the grant.
func TestTeamWorkspaceAccessDefaultDashboardFailureKeepsTheGrant(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	access := teamFake(api)
	const missing = "dsh_0000000000000000000009"
	failNext := func() {
		api.FailNext(http.MethodPut, "/v1/teams/"+teamOnlyID(t, api)+"/workspaces/"+teamWorkspace+"/default-dashboard",
			fakeapi.Problem(http.StatusNotFound, "NotFound", "Dashboard "+missing+" was not found"))
	}
	written := func(level string) func() {
		return func() {
			team := teamOnlyID(t, api)
			if got := access.level(team, teamWorkspace); got != level {
				t.Fatalf("access level = %q, want the %s written before the default failed", got, level)
			}
			if got := access.defaultDashboard(team, teamWorkspace); got != "" {
				t.Fatalf("default dashboard = %q, want none", got)
			}
		}
	}
	failed := regexp.MustCompile(`Failed to set the team's default dashboard(.|\n)*was not found`)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:   failNext,
				Config:      teamDefaultDashboardConfig(api, "canView", missing),
				ExpectError: failed,
			},
			{
				PreConfig:          written("canView"),
				Config:             teamDefaultDashboardConfig(api, "canView", missing),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{Config: teamDefaultDashboardConfig(api, "canView", "")},
			{
				PreConfig:   failNext,
				Config:      teamDefaultDashboardConfig(api, "canEdit", missing),
				ExpectError: failed,
			},
			{
				PreConfig:          written("canEdit"),
				Config:             teamDefaultDashboardConfig(api, "canEdit", missing),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestTeamDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:          func() { api.Remove("/v1/teams", teamOnlyID(t, api)) },
				Config:             teamConfig(api, "Platform", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestTeamCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)
	api.FailNext(http.MethodPost, "/v1/teams", fakeapi.Problem(http.StatusConflict, "Conflict", "Team 'Platform' already exists"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      teamConfig(api, "Platform", ""),
				ExpectError: regexp.MustCompile(`Failed to create team(.|\n)*already exists`),
			},
		},
	})
}

func TestTeamCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)
	teams := func() string { return "/v1/teams" }
	team := func() string { return "/v1/teams/" + teamOnlyID(t, api) }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   refuseNext(api, http.MethodPost, teams, costfluent.CapabilityCreateTeam),
				Config:      teamConfig(api, "Platform", ""),
				ExpectError: lacksCapability("Failed to create team", costfluent.CapabilityCreateTeam),
			},
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:   refuseNext(api, http.MethodPut, team, costfluent.CapabilityManageTeam),
				Config:      teamConfig(api, "Renamed", ""),
				ExpectError: lacksCapability("Failed to update team", costfluent.CapabilityManageTeam),
			},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, team, costfluent.CapabilityDeleteTeam),
				Config:      teamConfig(api, "Platform", ""),
				Destroy:     true,
				ExpectError: lacksCapability("Failed to delete team", costfluent.CapabilityDeleteTeam),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/teams"); len(ids) != 0 {
				return fmt.Errorf("teams left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func TestTeamWorkspaceAccessCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	access := teamFake(api)
	grant := func() string { return "/v1/teams/" + teamOnlyID(t, api) + "/workspaces/" + teamWorkspace }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:   refuseNext(api, http.MethodPut, grant, costfluent.CapabilityManageWorkspaceAccess),
				Config:      teamConfig(api, "Platform", "canView"),
				ExpectError: lacksCapability("Failed to set team workspace access", costfluent.CapabilityManageWorkspaceAccess),
			},
			{Config: teamConfig(api, "Platform", "canView")},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, grant, costfluent.CapabilityManageWorkspaceAccess),
				Config:      teamConfig(api, "Platform", ""),
				ExpectError: lacksCapability("Failed to remove team workspace access", costfluent.CapabilityManageWorkspaceAccess),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/teams"); len(ids) != 0 {
				return fmt.Errorf("teams left after destroy: %v", ids)
			}
			access.mu.Lock()
			defer access.mu.Unlock()
			for team, levels := range access.levels {
				if len(levels) != 0 {
					return fmt.Errorf("team %s keeps workspace access after destroy: %v", team, levels)
				}
			}
			return nil
		},
	})
}

func TestTeamWorkspaceAccessRefusesAnUnknownLevelAndABadImportID(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      teamConfig(api, "Platform", "admin"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				Config:        teamConfig(api, "Platform", "canView"),
				ResourceName:  "costfluent_team_workspace_access.a",
				ImportState:   true,
				ImportStateId: "team_only",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}

func TestTeamAPIErrorsBecomeDiagnostics(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamFake(api)
	fail := func(method, path, detail string) func() {
		return func() { api.FailNext(method, path, fakeapi.Problem(http.StatusConflict, "Conflict", detail)) }
	}
	team := func() string { return "/v1/teams/" + teamOnlyID(t, api) }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:   func() { fail(http.MethodPut, team(), "Cannot rename system team")() },
				Config:      teamConfig(api, "Renamed", ""),
				ExpectError: regexp.MustCompile(`Failed to update team(.|\n)*Cannot rename`),
			},
			{
				PreConfig:   func() { fail(http.MethodPut, team()+"/workspaces/"+teamWorkspace, "Workspace is gone")() },
				Config:      teamConfig(api, "Platform", "canView"),
				ExpectError: regexp.MustCompile(`Failed to set team workspace access(.|\n)*Workspace is gone`),
			},
			{Config: teamConfig(api, "Platform", "canView")},
			{
				PreConfig:   func() { fail(http.MethodPut, team()+"/workspaces/"+teamWorkspace, "Workspace is gone")() },
				Config:      teamConfig(api, "Platform", "canEdit"),
				ExpectError: regexp.MustCompile(`Failed to set team workspace access(.|\n)*Workspace is gone`),
			},
			{
				PreConfig:   func() { fail(http.MethodGet, team()+"/workspaces", "Access is being rebuilt")() },
				Config:      teamConfig(api, "Platform", "canView"),
				ExpectError: regexp.MustCompile(`Failed to read team workspace access(.|\n)*being rebuilt`),
			},
			{
				PreConfig:   func() { fail(http.MethodGet, team(), "Team is locked")() },
				Config:      teamConfig(api, "Platform", "canView"),
				ExpectError: regexp.MustCompile(`Failed to read team(.|\n)*Team is locked`),
			},
			{
				PreConfig:   func() { fail(http.MethodDelete, team()+"/workspaces/"+teamWorkspace, "Access is in use")() },
				Config:      teamConfig(api, "Platform", ""),
				ExpectError: regexp.MustCompile(`Failed to remove team workspace access(.|\n)*in use`),
			},
			{Config: teamConfig(api, "Platform", "")},
			{
				PreConfig:   func() { fail(http.MethodDelete, team(), "Cannot delete system team")() },
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to delete team(.|\n)*Cannot delete`),
			},
		},
	})
}
