package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	tagPolicyAddress = "costfluent_tag_policy.p"
	// tagPolicies is where the fake keeps each workspace's policy, by workspace ID.
	tagPolicies = "tag-policies"
	// tagPolicyWorkspaces is where the fake keeps the workspaces that exist.
	tagPolicyWorkspaces = "/v1/workspaces"
)

func tagPolicyPath(workspace string) string { return "/v1/workspaces/" + workspace + "/tag-policy" }

// tagPolicyFake serves GET and PUT /v1/workspaces/{workspaceId}/tag-policy for testWorkspace and
// otherWorkspace as the API does: a workspace that never saved a policy reads as one without rules,
// a save replaces the rules whole and answers the policy as stored, and the first rules create the
// report, which clearing the rules keeps. A workspace removed from tagPolicyWorkspaces answers 404.
func tagPolicyFake(api *fakeapi.Server) {
	for _, workspace := range []string{testWorkspace, otherWorkspace} {
		api.Put(tagPolicyWorkspaces, workspace, map[string]any{})
	}
	stored := func(workspace string) map[string]any {
		policy, ok := api.Get(tagPolicies, workspace)
		if !ok {
			return map[string]any{"rules": []any{}, "reportId": nil, "updatedAt": nil}
		}
		delete(policy, "id")
		return policy
	}
	exists := func(r fakeapi.Request) bool {
		_, ok := api.Get(tagPolicyWorkspaces, r.PathValue("workspaceId"))
		return ok
	}
	api.Handle("GET /v1/workspaces/{workspaceId}/tag-policy", func(r fakeapi.Request) fakeapi.Response {
		if !exists(r) {
			return fakeapi.NotFound()
		}
		return fakeapi.JSON(stored(r.PathValue("workspaceId")))
	})
	api.Handle("PUT /v1/workspaces/{workspaceId}/tag-policy", func(r fakeapi.Request) fakeapi.Response {
		if !exists(r) {
			return fakeapi.NotFound()
		}
		rules, ok := r.Body["rules"].([]any)
		if !ok {
			return fakeapi.Problem(http.StatusBadRequest, "Validation", "Name the policy's rules; an empty list clears the policy.")
		}
		workspace := r.PathValue("workspaceId")
		policy := stored(workspace)
		if len(rules) > 0 && policy["reportId"] == nil {
			policy["reportId"] = api.NextID("crp_")
		}
		policy["rules"] = rules
		policy["updatedAt"] = "2026-10-20T07:00:00Z"
		api.Put(tagPolicies, workspace, policy)
		return fakeapi.JSON(stored(workspace))
	})
}

func tagPolicyConfig(api *fakeapi.Server, rules string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_tag_policy" "p" {
  workspace_id = %q
%s}
`, testWorkspace, rules)
}

const (
	// tagPolicyTwoRules names a provider and accounts in one rule and leaves both out of the other.
	tagPolicyTwoRules = `
  rule {
    provider      = "aws"
    account_ids   = ["123456789012"]
    required_keys = ["team", "env"]
  }

  rule {
    required_keys = ["cost-center"]
  }
`
	tagPolicyOneRule = `
  rule {
    provider      = "gcp"
    required_keys = ["owner"]
  }
`
)

// sentRules is the rules of the latest PUT to workspace's tag policy, as JSON with sorted keys.
func sentRules(api *fakeapi.Server, workspace string) (string, error) {
	rs := api.Requests(http.MethodPut, tagPolicyPath(workspace))
	if len(rs) == 0 {
		return "", fmt.Errorf("no PUT %s received", tagPolicyPath(workspace))
	}
	rules, ok := rs[len(rs)-1].Body["rules"]
	if !ok {
		return "", fmt.Errorf("PUT body %s carries no rules", rs[len(rs)-1].Raw)
	}
	raw, err := json.Marshal(rules)
	return string(raw), err
}

func checkSentRules(api *fakeapi.Server, workspace, want string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		got, err := sentRules(api, workspace)
		if err == nil && got != want {
			err = fmt.Errorf("sent rules %s, want %s", got, want)
		}
		return err
	}
}

func TestTagPolicyLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)
	var reportID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: tagPolicyConfig(api, tagPolicyTwoRules),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagPolicyAddress, "id", testWorkspace),
					resource.TestCheckResourceAttr(tagPolicyAddress, "workspace_id", testWorkspace),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.#", "2"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.provider", "aws"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.account_ids.0", "123456789012"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.required_keys.#", "2"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.required_keys.1", "env"),
					resource.TestCheckNoResourceAttr(tagPolicyAddress, "rule.1.provider"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.1.required_keys.0", "cost-center"),
					resource.TestMatchResourceAttr(tagPolicyAddress, "report_id", regexp.MustCompile(`^crp_.{22}$`)),
					resource.TestCheckResourceAttrWith(tagPolicyAddress, "report_id", func(id string) error {
						reportID = id
						return nil
					}),
					// A rule without accounts goes out as an empty list, the API's "every account",
					// and one without a provider leaves the field out.
					checkSentRules(api, testWorkspace,
						`[{"accountIds":["123456789012"],"provider":"aws","requiredKeys":["team","env"]},`+
							`{"accountIds":[],"requiredKeys":["cost-center"]}]`),
				),
				// The API answers the omitted account_ids as [], which must read back unset rather
				// than plan a change on every run.
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(tagPolicyAddress,
						tfjsonpath.New("rule").AtSliceIndex(1).AtMapKey("account_ids"), knownvalue.Null()),
				},
			},
			{
				Config:   tagPolicyConfig(api, tagPolicyTwoRules),
				PlanOnly: true,
			},
			{
				// The update replaces the rules whole and keeps the report the first rules created.
				Config: tagPolicyConfig(api, tagPolicyOneRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(tagPolicyAddress, plancheck.ResourceActionUpdate),
						plancheck.ExpectKnownValue(tagPolicyAddress, tfjsonpath.New("report_id"),
							knownvalue.StringRegexp(regexp.MustCompile(`^crp_`))),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.#", "1"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.provider", "gcp"),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.0.required_keys.0", "owner"),
					resource.TestCheckResourceAttrWith(tagPolicyAddress, "report_id", func(id string) error {
						if id != reportID {
							return fmt.Errorf("report_id = %s, want the first rules' %s", id, reportID)
						}
						return nil
					}),
					checkSentRules(api, testWorkspace, `[{"accountIds":[],"provider":"gcp","requiredKeys":["owner"]}]`),
				),
			},
			{
				// The import ID is the workspace ID.
				ResourceName:      tagPolicyAddress,
				ImportState:       true,
				ImportStateId:     testWorkspace,
				ImportStateVerify: true,
			},
		},
		// Destroying clears the rules: the policy belongs to the workspace and is never deleted.
		CheckDestroy: func(*terraform.State) error {
			got, err := sentRules(api, testWorkspace)
			if err != nil {
				return fmt.Errorf("destroy sent no rules: %w", err)
			}
			if got != "[]" {
				return fmt.Errorf("destroy sent rules %s, want []", got)
			}
			if policy, _ := api.Get(tagPolicies, testWorkspace); len(policy["rules"].([]any)) != 0 {
				return fmt.Errorf("rules left after destroy: %v", policy["rules"])
			}
			return nil
		},
	})
}

// A configuration without rules is a valid, empty policy. Its first rules create the report, so the
// plan that adds them cannot know report_id yet.
func TestTagPolicyWithoutRulesPlansTheReportItsFirstRulesCreate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: tagPolicyConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.#", "0"),
					resource.TestCheckNoResourceAttr(tagPolicyAddress, "report_id"),
					checkSentRules(api, testWorkspace, "[]"),
				),
			},
			{
				Config: tagPolicyConfig(api, tagPolicyOneRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectUnknownValue(tagPolicyAddress, tfjsonpath.New("report_id")),
					},
				},
				Check: resource.TestMatchResourceAttr(tagPolicyAddress, "report_id", regexp.MustCompile(`^crp_.{22}$`)),
			},
		},
	})
}

func TestTagPolicyOfAVanishedWorkspaceIsRemovedFromState(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: tagPolicyConfig(api, tagPolicyOneRule)},
			{
				PreConfig: func() { api.Remove(tagPolicyWorkspaces, testWorkspace) },
				Config:    tagPolicyConfig(api, tagPolicyOneRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(tagPolicyAddress, plancheck.ResourceActionCreate),
					},
				},
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Rules removed outside Terraform are drift: the policy still exists, empty, and the plan puts the
// configured rules back.
func TestTagPolicyClearedOutsideTerraformPlansTheRulesBack(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: tagPolicyConfig(api, tagPolicyOneRule)},
			{
				PreConfig: func() {
					api.Update(tagPolicies, testWorkspace, func(policy map[string]any) { policy["rules"] = []any{} })
				},
				Config: tagPolicyConfig(api, tagPolicyOneRule),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(tagPolicyAddress, plancheck.ResourceActionUpdate),
					},
				},
				Check: checkSentRules(api, testWorkspace, `[{"accountIds":[],"provider":"gcp","requiredKeys":["owner"]}]`),
			},
		},
	})
}

// A policy created in the provider's default workspace records that workspace, so a later run whose
// default is another workspace still manages the original policy instead of moving it.
func TestTagPolicyKeepsTheWorkspaceItWasCreatedIn(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: withoutWorkspace(api, tagPolicyConfig(api, tagPolicyOneRule), testWorkspace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagPolicyAddress, "id", testWorkspace),
					resource.TestCheckResourceAttr(tagPolicyAddress, "workspace_id", testWorkspace),
				),
			},
			{
				Config:   withoutWorkspace(api, tagPolicyConfig(api, tagPolicyOneRule), otherWorkspace),
				PlanOnly: true,
			},
			{
				Config: withoutWorkspace(api, tagPolicyConfig(api, tagPolicyTwoRules), otherWorkspace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(tagPolicyAddress, "workspace_id", testWorkspace),
					resource.TestCheckResourceAttr(tagPolicyAddress, "rule.#", "2"),
				),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			got, err := sentRules(api, testWorkspace)
			if err != nil {
				return fmt.Errorf("destroy sent no rules to the original workspace: %w", err)
			}
			if got != "[]" {
				return fmt.Errorf("destroy sent rules %s to the original workspace, want []", got)
			}
			if sent := api.Requests(http.MethodPut, tagPolicyPath(otherWorkspace)); len(sent) != 0 {
				return fmt.Errorf("the provider's later default workspace received %d saves, want none", len(sent))
			}
			return nil
		},
	})
}

func TestTagPolicyWithoutAWorkspaceIsRefused(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      explicitWorkspaceLine.ReplaceAllString(tagPolicyConfig(api, tagPolicyOneRule), ""),
				ExpectError: regexp.MustCompile(`No workspace for the tag policy`),
			},
		},
	})
}

func TestTagPolicySaveFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)
	api.FailNext(http.MethodPut, tagPolicyPath(testWorkspace),
		fakeapi.Problem(http.StatusBadRequest, "Validation", "A tag policy holds at most 20 rules."))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      tagPolicyConfig(api, tagPolicyOneRule),
				ExpectError: regexp.MustCompile(`Failed to create tag policy(.|\n)*A tag policy holds at most 20 rules`),
			},
			{Config: tagPolicyConfig(api, tagPolicyOneRule)},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, tagPolicyPath(testWorkspace),
						fakeapi.Problem(http.StatusBadRequest, "Validation", "A required tag key must not be blank."))
				},
				Config:      tagPolicyConfig(api, tagPolicyTwoRules),
				ExpectError: regexp.MustCompile(`Failed to update tag policy(.|\n)*A required tag key must not be blank`),
			},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, tagPolicyPath(testWorkspace), fakeapi.MissingCapability("viewTags"))
				},
				Config:      tagPolicyConfig(api, tagPolicyOneRule),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to read tag policy(.|\n)*viewTags`),
			},
		},
	})
}

func TestTagPolicyDestroyFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	tagPolicyFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: tagPolicyConfig(api, tagPolicyOneRule)},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, tagPolicyPath(testWorkspace), fakeapi.MissingCapability("manageTags"))
				},
				Config:      api.ProviderBlock(),
				ExpectError: regexp.MustCompile(`Failed to clear tag policy(.|\n)*manageTags`),
			},
		},
	})
}

// The limits the schema holds a rule to are the API's, so a plan breaking one is refused before
// anything is sent.
func TestTagPolicyRuleOutsideTheLimitsIsRefusedAtPlan(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ rule, want string }{
		"provider not a key": {
			rule: `provider = "AWS"` + "\n    required_keys = [\"team\"]",
			want: `rule\[0\]\.provider value must be one of`,
		},
		"no required keys": {
			rule: `required_keys = []`,
			want: `rule\[0\]\.required_keys list must contain at least 1 elements`,
		},
		"a required key twice": {
			rule: `required_keys = ["team", "team"]`,
			want: `required_keys = \["team", "team"\](.|\n)*duplicate values of: "team"`,
		},
		"a blank account": {
			rule: `account_ids = [" "]` + "\n    required_keys = [\"team\"]",
			want: `rule\[0\]\.account_ids\[0\] must not be blank`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			tagPolicyFake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config:      tagPolicyConfig(api, "  rule {\n    "+tc.rule+"\n  }\n"),
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tc.want),
					},
				},
			})
		})
	}
}
