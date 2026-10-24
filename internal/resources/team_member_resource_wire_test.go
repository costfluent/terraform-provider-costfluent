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
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	memberTeam = "team_0000000000000000000005"
	memberUser = "usr_0000000000000000000009"
)

// teamMembers is the membership the fake holds, user ID to role; Terraform refreshes in parallel.
type teamMembers struct {
	mu    sync.Mutex
	roles map[string]string
}

func (m *teamMembers) role(user string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	role, ok := m.roles[user]
	return role, ok
}

func (m *teamMembers) remove(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.roles, user)
}

// teamMemberFake serves one team as TeamDetailResponse and its members under
// /v1/teams/{id}/members, as the API does: a user already in the team is a conflict, and a role
// change or removal of someone not in it is not found.
func teamMemberFake(api *fakeapi.Server) *teamMembers {
	members := &teamMembers{roles: map[string]string{}}
	api.Handle("GET /v1/teams/{id}", func(r fakeapi.Request) fakeapi.Response {
		if r.PathValue("id") != memberTeam {
			return fakeapi.NotFound()
		}
		members.mu.Lock()
		defer members.mu.Unlock()
		list := []any{}
		for user, role := range members.roles {
			list = append(list, map[string]any{"userId": user, "email": "lena@example.com", "firstName": "Lena",
				"lastName": "Hoffmann", "role": role, "joinedAt": "2026-10-01T09:00:00Z"})
		}
		return fakeapi.JSON(map[string]any{"id": memberTeam, "name": "Platform", "isSystemTeam": false,
			"members": list, "workspaceAccess": []any{}, "createdAt": "2026-10-01T09:00:00Z"})
	})
	api.Handle("POST /v1/teams/{id}/members", func(r fakeapi.Request) fakeapi.Response {
		members.mu.Lock()
		defer members.mu.Unlock()
		user, _ := r.Body["userId"].(string)
		if _, ok := members.roles[user]; ok {
			return fakeapi.Problem(http.StatusConflict, "TeamMember.Conflict", "User is already a member of this team")
		}
		members.roles[user], _ = r.Body["role"].(string)
		return fakeapi.NoContent()
	})
	api.Handle("PUT /v1/teams/{id}/members/{userId}", func(r fakeapi.Request) fakeapi.Response {
		members.mu.Lock()
		defer members.mu.Unlock()
		if _, ok := members.roles[r.PathValue("userId")]; !ok {
			return fakeapi.NotFound()
		}
		members.roles[r.PathValue("userId")], _ = r.Body["role"].(string)
		return fakeapi.NoContent()
	})
	api.Handle("DELETE /v1/teams/{id}/members/{userId}", func(r fakeapi.Request) fakeapi.Response {
		members.mu.Lock()
		defer members.mu.Unlock()
		if _, ok := members.roles[r.PathValue("userId")]; !ok {
			return fakeapi.NotFound()
		}
		delete(members.roles, r.PathValue("userId"))
		return fakeapi.NoContent()
	})
	return members
}

func teamMemberConfig(api *fakeapi.Server, role string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_team_member" "m" {
  team_id = %q
  user_id = %q
  role    = %q
}
`, memberTeam, memberUser, role)
}

func TestTeamMemberIsAddedChangedImportedAndRemoved(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	members := teamMemberFake(api)
	membersPath := "/v1/teams/" + memberTeam + "/members"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: teamMemberConfig(api, "viewer"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team_member.m", "id", memberTeam+":"+memberUser),
					resource.TestCheckResourceAttr("costfluent_team_member.m", "role", "viewer"),
					func(*terraform.State) error {
						add := api.LastRequest(http.MethodPost, membersPath)
						if len(add.Body) != 2 || add.Body["userId"] != memberUser || add.Body["role"] != "viewer" {
							return fmt.Errorf("add body = %s", add.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: teamMemberConfig(api, "editor"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_team_member.m", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_team_member.m", "role", "editor"),
					func(*terraform.State) error {
						if role, _ := members.role(memberUser); role != "editor" {
							return fmt.Errorf("stored role = %q, want editor", role)
						}
						if body := api.LastRequest(http.MethodPut, membersPath+"/"+memberUser).Body; len(body) != 1 || body["role"] != "editor" {
							return fmt.Errorf("role change body = %v", body)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_team_member.m",
				ImportState:       true,
				ImportStateId:     memberTeam + ":" + memberUser,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if _, ok := members.role(memberUser); ok {
				return fmt.Errorf("member still in the team after destroy")
			}
			return nil
		},
	})
}

// A member removed outside Terraform is added again.
func TestTeamMemberRemovedOutsideTerraformIsAddedAgain(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	members := teamMemberFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamMemberConfig(api, "viewer")},
			{
				PreConfig: func() { members.remove(memberUser) },
				Config:    teamMemberConfig(api, "viewer"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_team_member.m", plancheck.ResourceActionCreate),
				}},
				Check: func(*terraform.State) error {
					if role, ok := members.role(memberUser); !ok || role != "viewer" {
						return fmt.Errorf("member not added again: %q %v", role, ok)
					}
					return nil
				},
			},
		},
	})
}

func TestTeamMemberSurfacesAPIErrorsAndRefusesBadInput(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	members := teamMemberFake(api)
	members.roles[memberUser] = "viewer"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      teamMemberConfig(api, "viewer"),
				ExpectError: regexp.MustCompile(`Failed to add team member(.|\n)*already a member`),
			},
			{
				Config:      teamMemberConfig(api, "admin"),
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_team_member" "m" {
  team_id = %q
  user_id = "lena@example.com"
  role    = "viewer"
}
`, memberTeam),
				ExpectError: regexp.MustCompile(`usr_`),
			},
			{
				Config:        teamMemberConfig(api, "viewer"),
				ResourceName:  "costfluent_team_member.m",
				ImportState:   true,
				ImportStateId: memberUser,
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
		},
	})
}

// A failed read, role change or removal surfaces the API's problem, and a team gone outside
// Terraform drops the membership from state, so the next apply adds it again.
func TestTeamMemberFailuresSurfaceAndAGoneTeamIsForgotten(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	teamMemberFake(api)
	teamPath := "/v1/teams/" + memberTeam
	memberPath := teamPath + "/members/" + memberUser

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: teamMemberConfig(api, "viewer")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, teamPath, fakeapi.Problem(http.StatusConflict, "Conflict", "Team is locked"))
				},
				Config:      teamMemberConfig(api, "viewer"),
				ExpectError: regexp.MustCompile(`Failed to read team member(.|\n)*Team is locked`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, memberPath, fakeapi.Problem(http.StatusConflict, "Conflict", "Role is in use"))
				},
				Config:      teamMemberConfig(api, "editor"),
				ExpectError: regexp.MustCompile(`Failed to change team member role(.|\n)*Role is in use`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodDelete, memberPath, fakeapi.Problem(http.StatusConflict, "Conflict", "Last owner"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to remove team member(.|\n)*Last owner`),
			},
			{
				PreConfig: func() { api.FailNext(http.MethodGet, teamPath, fakeapi.NotFound()) },
				Config:    teamMemberConfig(api, "viewer"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("costfluent_team_member.m", plancheck.ResourceActionCreate),
				}},
				ExpectError: regexp.MustCompile(`Failed to add team member(.|\n)*already a member`),
			},
		},
	})
}

func TestTeamMemberCapabilityRefusalNamesTheCapability(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	members := teamMemberFake(api)
	add := func() string { return "/v1/teams/" + memberTeam + "/members" }
	member := func() string { return add() + "/" + memberUser }

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				PreConfig:   refuseNext(api, http.MethodPost, add, costfluent.CapabilityManageTeam),
				Config:      teamMemberConfig(api, "viewer"),
				ExpectError: lacksCapability("Failed to add team member", costfluent.CapabilityManageTeam),
			},
			{Config: teamMemberConfig(api, "viewer")},
			{
				PreConfig:   refuseNext(api, http.MethodPut, member, costfluent.CapabilityManageTeam),
				Config:      teamMemberConfig(api, "editor"),
				ExpectError: lacksCapability("Failed to change team member role", costfluent.CapabilityManageTeam),
			},
			{
				PreConfig:   refuseNext(api, http.MethodDelete, member, costfluent.CapabilityManageTeam),
				Config:      teamMemberConfig(api, "viewer"),
				Destroy:     true,
				ExpectError: lacksCapability("Failed to remove team member", costfluent.CapabilityManageTeam),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if role, ok := members.role(memberUser); ok {
				return fmt.Errorf("member left after destroy with role %s", role)
			}
			return nil
		},
	})
}
