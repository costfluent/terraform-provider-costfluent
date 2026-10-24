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

// teamAccess is the workspace access the fake holds per team; Terraform refreshes in parallel.
type teamAccess struct {
	mu     sync.Mutex
	levels map[string]map[string]string
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
}

// teamFake serves /v1/teams with TeamResponse on create, list and update, TeamDetailResponse on a
// read, and the workspace access of each team under /v1/teams/{id}/workspaces.
func teamFake(api *fakeapi.Server) *teamAccess {
	access := &teamAccess{levels: map[string]map[string]string{}}
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
		for workspace, level := range access.levels[r.PathValue("id")] {
			items = append(items, map[string]any{"workspaceId": workspace, "workspaceName": "Prod", "accessLevel": level})
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
