package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	virtualTagQuery  = "costs.service = 'Amazon EC2'"
	virtualTagStored = `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"]}]`
)

// virtualTagStoredFilter is how the API stores a rule filter: the CQL it was sent as, as its JSON
// document.
func virtualTagStoredFilter(filter any) any {
	if filter == virtualTagQuery {
		return virtualTagStored
	}
	return filter
}

func virtualTagStoredRules(rules any) []any {
	out := []any{}
	list, _ := rules.([]any)
	for _, r := range list {
		rule, _ := r.(map[string]any)
		out = append(out, map[string]any{"value": rule["value"], "filter": virtualTagStoredFilter(rule["filter"])})
	}
	return out
}

// virtualTagFake serves /v1/virtual-tags with the VirtualTagCreatedResponse and VirtualTagResponse
// shapes, an update answering the tag as stored as PATCH does, and the translate operation the
// provider uses to tell whether a configured CQL filter means the stored JSON.
func virtualTagFake(api *fakeapi.Server) {
	api.Collection("/v1/virtual-tags", fakeapi.Collection{
		IDPrefix:              "vtag_",
		Versioned:             true,
		Scope:                 fakeapi.WorkspaceScoped,
		UpdateWorkspaceInBody: true,
		UpdateMethod:          http.MethodPatch,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"key":              body["key"],
				"collapsedTagKeys": body["collapsedTagKeys"],
				"rules":            virtualTagStoredRules(body["rules"]),
				"createdAt":        "2026-10-05T07:00:00Z",
			}
			for _, k := range []string{"description", "defaultValue"} {
				if v, ok := body[k]; ok {
					obj[k] = v
				}
			}
			return obj
		},
		Created: func(obj map[string]any) fakeapi.Response {
			return fakeapi.JSON(map[string]any{"id": obj["id"], "key": obj["key"]})
		},
		Apply: func(obj, body map[string]any) {
			for _, k := range []string{"key", "collapsedTagKeys"} {
				if v, ok := body[k]; ok {
					obj[k] = v
				}
			}
			for k, clear := range map[string]string{"description": "clearDescription", "defaultValue": "clearDefaultValue"} {
				if body[clear] == true {
					delete(obj, k)
				}
				if v, ok := body[k]; ok && v == "" {
					delete(obj, k)
				} else if ok {
					obj[k] = v
				}
			}
			if v, ok := body["rules"]; ok {
				obj["rules"] = virtualTagStoredRules(v)
			}
			obj["updatedAt"] = "2026-10-06T07:00:00Z"
		},
	})
	api.Handle("GET /v1/cost-filters/translate", func(r fakeapi.Request) fakeapi.Response {
		if r.Query.Get("filter") != virtualTagQuery {
			return fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "The filter does not parse")
		}
		return fakeapi.JSON(map[string]any{"filter": virtualTagStored, "query": virtualTagQuery})
	})
}

func virtualTagConfig(api *fakeapi.Server, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_virtual_tag" "t" {
  workspace_id       = %q
  key                = "team"
  collapsed_tag_keys = ["team", "Team"]
%s
  rule {
    value  = "compute"
    filter = %q
  }
}
`, testWorkspace, extra, virtualTagQuery)
}

func virtualTagOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/virtual-tags")
	if len(ids) != 1 {
		t.Fatalf("virtual tags = %v, want one", ids)
	}
	return ids[0]
}

func TestVirtualTagLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: virtualTagConfig(api, "  default_value = \"unassigned\"\n  description = \"Owning team\""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "description", "Owning team"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "key", "team"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "default_value", "unassigned"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "collapsed_tag_keys.#", "2"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "collapsed_tag_keys.1", "Team"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "rule.0.value", "compute"),
					// The API stores the JSON; the configured CQL means the same, so it stays.
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "rule.0.filter", virtualTagQuery),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "created_at", "2026-10-05T07:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_virtual_tag.t", "updated_at"),
					resource.TestMatchResourceAttr("costfluent_virtual_tag.t", "id", regexp.MustCompile(`^vtag_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/virtual-tags")
						var body struct {
							WorkspaceID      string              `json:"workspaceId"`
							Key              string              `json:"key"`
							Description      string              `json:"description"`
							DefaultValue     string              `json:"defaultValue"`
							CollapsedTagKeys []string            `json:"collapsedTagKeys"`
							Rules            []map[string]string `json:"rules"`
						}
						if err := json.Unmarshal(create.Raw, &body); err != nil {
							return err
						}
						if body.WorkspaceID != testWorkspace || body.Key != "team" || body.DefaultValue != "unassigned" ||
							body.Description != "Owning team" ||
							fmt.Sprint(body.CollapsedTagKeys) != "[team Team]" || len(body.Rules) != 1 ||
							body.Rules[0]["value"] != "compute" || body.Rules[0]["filter"] != virtualTagQuery {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						read := api.LastRequest(http.MethodGet, "/v1/virtual-tags/"+virtualTagOnlyID(t, api))
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						return nil
					},
				),
			},
			{
				// Removing default_value and description sends their clear flags.
				Config: virtualTagConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_virtual_tag.t", "default_value"),
					resource.TestCheckNoResourceAttr("costfluent_virtual_tag.t", "description"),
					resource.TestCheckResourceAttr("costfluent_virtual_tag.t", "updated_at", "2026-10-06T07:00:00Z"),
					func(*terraform.State) error {
						update := api.LastRequest(http.MethodPatch, "/v1/virtual-tags/"+virtualTagOnlyID(t, api))
						if update.Body["workspaceId"] != testWorkspace || update.Body["key"] != "team" ||
							update.Body["clearDefaultValue"] != true || update.Body["clearDescription"] != true {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						for _, absent := range []string{"defaultValue", "description"} {
							if _, sent := update.Body[absent]; sent {
								return fmt.Errorf("update sent %s beside its clear flag: %s", absent, update.Raw)
							}
						}
						// The update replaces the rules and collapsed keys whole.
						if rules, _ := update.Body["rules"].([]any); len(rules) != 1 {
							return fmt.Errorf("update rules = %s", update.Raw)
						}
						if keys, _ := update.Body["collapsedTagKeys"].([]any); len(keys) != 2 {
							return fmt.Errorf("update collapsedTagKeys = %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_virtual_tag.t",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + virtualTagOnlyID(t, api), nil
				},
				// An import has no configured CQL to keep, so it reads the filter as the stored JSON.
				ImportStateVerifyIgnore: []string{"rule.0.filter"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["rule.0.filter"]; got != virtualTagStored {
						return fmt.Errorf("imported filter = %q, want the stored JSON", got)
					}
					return nil
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/virtual-tags"); len(ids) != 0 {
				return fmt.Errorf("virtual tags left after destroy: %v", ids)
			}
			return nil
		},
	})
}

func TestVirtualTagDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: virtualTagConfig(api, "")},
			{
				PreConfig:          func() { api.Remove("/v1/virtual-tags", virtualTagOnlyID(t, api)) },
				Config:             virtualTagConfig(api, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// A rule filter edited outside Terraform no longer means the configured CQL, so the plan shows it.
func TestVirtualTagRuleFilterChangedOutsideTerraformPlansAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: virtualTagConfig(api, "")},
			{
				PreConfig: func() {
					api.Update("/v1/virtual-tags", virtualTagOnlyID(t, api), func(obj map[string]any) {
						obj["rules"] = []any{map[string]any{"value": "compute",
							"filter": `[{"field":"ServiceName","operator":"In","values":["Amazon RDS"]}]`}}
					})
				},
				Config:             virtualTagConfig(api, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applied, so the destroy after the test, which does not refresh, deletes the version the
			// edit left rather than being refused for it.
			{Config: virtualTagConfig(api, "")},
		},
	})
}

func TestVirtualTagFilterTranslationFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: virtualTagConfig(api, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/cost-filters/translate",
						fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "Unknown field costs.servce"))
				},
				Config:      virtualTagConfig(api, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to compare a virtual tag rule filter(.|\n)*Unknown field costs.servce`),
			},
		},
	})
}

func TestVirtualTagCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)
	api.FailNext(http.MethodPost, "/v1/virtual-tags",
		fakeapi.Problem(http.StatusConflict, "VirtualTag.Duplicate", "A virtual tag with key team already exists"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      virtualTagConfig(api, ""),
				ExpectError: regexp.MustCompile(`Failed to create virtual tag(.|\n)*A virtual tag with key team already exists`),
			},
		},
	})
}

func TestVirtualTagCreateWhoseReadFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)
	createWhoseReadFailsIsTainted(t, api, "/v1/virtual-tags", "vtag_", "costfluent_virtual_tag.t",
		virtualTagConfig(api, ""), "Failed to create virtual tag")
}

func TestVirtualTagCreateWhoseFilterTranslationFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	virtualTagFake(api)
	createWhoseFilterTranslationFailsIsTainted(t, api, "/v1/virtual-tags", "vtag_", "costfluent_virtual_tag.t",
		virtualTagConfig(api, ""), "Failed to compare a virtual tag rule filter")
}
