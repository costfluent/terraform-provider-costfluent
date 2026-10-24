package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const (
	allocationRuleSegment = "seg_0000000000000000000042"

	allocationRuleEC2Query  = "costs.service = 'AmazonEC2' AND costs.region = 'eu-central-1'"
	allocationRuleEC2Stored = `[{"field":"ServiceName","operator":"In","values":["AmazonEC2"]},` +
		`{"field":"Region","operator":"In","values":["eu-central-1"]}]`
	allocationRuleRDSQuery  = "costs.service = 'AmazonRDS' AND costs.region = 'eu-central-1'"
	allocationRuleRDSStored = `[{"field":"ServiceName","operator":"In","values":["AmazonRDS"]},` +
		`{"field":"Region","operator":"In","values":["eu-central-1"]}]`
)

// allocationRuleStoredFilters is how the API stores a rule filter sent as CQL: as its JSON document.
// A filter sent as JSON is stored re-serialised, which here means in sorted key order.
var allocationRuleStoredFilters = map[string]string{
	allocationRuleEC2Query: allocationRuleEC2Stored,
	allocationRuleRDSQuery: allocationRuleRDSStored,
}

// allocationRuleFields are the rule fields a create or update request carries and a response repeats.
var allocationRuleFields = []string{"name", "priority", "segmentId", "isEnabled"}

func allocationRuleApply(obj, body map[string]any) {
	for _, k := range allocationRuleFields {
		obj[k] = body[k]
	}
	filter, _ := body["filter"].(string)
	var document any
	if stored, ok := allocationRuleStoredFilters[filter]; ok {
		obj["filter"] = stored
	} else if json.Unmarshal([]byte(filter), &document) == nil {
		canonical, _ := json.Marshal(document)
		obj["filter"] = string(canonical)
	} else {
		obj["filter"] = filter
	}
}

// allocationRuleFake serves /v1/allocation/rules with the AllocationRuleResponse shape, which
// create and update both answer with, resolving the segment's name and cost centre and storing a
// CQL filter as its JSON document as the API does, and the translate operation the provider uses to
// tell whether a configured CQL filter means the stored JSON.
func allocationRuleFake(api *fakeapi.Server) {
	api.Collection("/v1/allocation/rules", fakeapi.Collection{
		IDPrefix:  "alr_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			obj := map[string]any{
				"costKind":    "Direct",
				"segmentName": "Platform",
				"costCentre":  "CC-100",
				"createdAt":   "2026-10-03T12:00:00Z",
			}
			allocationRuleApply(obj, body)
			return obj
		},
		Apply: func(obj, body map[string]any) {
			allocationRuleApply(obj, body)
			obj["updatedAt"] = "2026-10-04T12:00:00Z"
		},
	})
	api.Handle("GET /v1/cost-filters/translate", func(r fakeapi.Request) fakeapi.Response {
		query := r.Query.Get("filter")
		stored, ok := allocationRuleStoredFilters[query]
		if !ok {
			return fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "The filter does not parse")
		}
		return fakeapi.JSON(map[string]any{"filter": stored, "query": query})
	})
}

func allocationRuleConfig(api *fakeapi.Server, filter, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_allocation_rule" "r" {
  workspace_id = %q
  name         = "Checkout compute"
  filter       = %q
  segment_id   = %q
%s}
`, testWorkspace, filter, allocationRuleSegment, extra)
}

func allocationRuleOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/allocation/rules")
	if len(ids) != 1 {
		t.Fatalf("allocation rules = %v, want one", ids)
	}
	return ids[0]
}

func TestAllocationRuleLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// The API answers with the filter's JSON document; the configured CQL means the same, so
				// state keeps it and the refresh plan the step runs after apply stays empty.
				Config: allocationRuleConfig(api, allocationRuleEC2Query, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "filter", allocationRuleEC2Query),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "priority", "100"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "is_enabled", "true"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "cost_kind", "Direct"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "segment_name", "Platform"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "cost_centre", "CC-100"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "created_at", "2026-10-03T12:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_allocation_rule.r", "updated_at"),
					resource.TestMatchResourceAttr("costfluent_allocation_rule.r", "id", regexp.MustCompile(`^alr_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/allocation/rules")
						want := map[string]any{
							"workspaceId": testWorkspace, "name": "Checkout compute", "priority": float64(100),
							"filter": allocationRuleEC2Query, "segmentId": allocationRuleSegment, "isEnabled": true,
						}
						for k, v := range want {
							if create.Body[k] != v {
								return fmt.Errorf("create %s = %v, want %v: %s", k, create.Body[k], v, create.Raw)
							}
						}
						if len(create.Body) != len(want) {
							return fmt.Errorf("create sent fields outside the contract: %s", create.Raw)
						}
						return nil
					},
				),
			},
			{
				Config: allocationRuleConfig(api, allocationRuleRDSQuery, "  priority = 10\n  is_enabled = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "filter", allocationRuleRDSQuery),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "priority", "10"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "is_enabled", "false"),
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "updated_at", "2026-10-04T12:00:00Z"),
					func(*terraform.State) error {
						read := api.LastRequest(http.MethodGet, "/v1/allocation/rules")
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						update := api.LastRequest(http.MethodPut, "/v1/allocation/rules/"+allocationRuleOnlyID(t, api))
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						// The update replaces the rule whole, so it carries the unchanged fields too.
						if update.Body["filter"] != allocationRuleRDSQuery ||
							update.Body["priority"] != float64(10) || update.Body["isEnabled"] != false ||
							update.Body["name"] != "Checkout compute" || update.Body["segmentId"] != allocationRuleSegment {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_allocation_rule.r",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + allocationRuleOnlyID(t, api), nil
				},
				// An import has no configured CQL to keep, so it reads the filter as the stored JSON.
				ImportStateVerifyIgnore: []string{"filter"},
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					if got := states[0].Attributes["filter"]; got != allocationRuleRDSStored {
						return fmt.Errorf("imported filter = %q, want the stored JSON", got)
					}
					return nil
				},
			},
			{
				ResourceName:  "costfluent_allocation_rule.r",
				ImportState:   true,
				ImportStateId: "alr_0000000000000000000001",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/allocation/rules"); len(ids) != 0 {
				return fmt.Errorf("allocation rules left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// A filter written as the JSON document in another key order means the stored one, so it plans
// nothing either.
func TestAllocationRuleJSONFilterInAnotherKeyOrderPlansNoChange(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)
	reordered := `[{"values":["AmazonEC2"],"operator":"In","field":"ServiceName"},` +
		`{"field":"Region","values":["eu-central-1"],"operator":"In"}]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: allocationRuleConfig(api, reordered, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_allocation_rule.r", "filter", reordered),
					func(*terraform.State) error {
						rule, _ := api.Get("/v1/allocation/rules", allocationRuleOnlyID(t, api))
						if rule["filter"] != allocationRuleEC2Stored {
							return fmt.Errorf("stored filter = %v, want the API's own spelling", rule["filter"])
						}
						return nil
					},
				),
			},
		},
	})
}

// A filter edited outside Terraform no longer means the configured CQL, so the plan shows it.
func TestAllocationRuleFilterChangedOutsideTerraformPlansAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: allocationRuleConfig(api, allocationRuleEC2Query, "")},
			{
				PreConfig: func() {
					api.Update("/v1/allocation/rules", allocationRuleOnlyID(t, api), func(obj map[string]any) {
						obj["filter"] = allocationRuleRDSStored
					})
				},
				Config:             allocationRuleConfig(api, allocationRuleEC2Query, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// The apply restores the configured filter. Without it the post-test destroy, which does
			// not refresh, would send the version from before the edit and be refused with 412.
			{Config: allocationRuleConfig(api, allocationRuleEC2Query, "")},
		},
	})
}

func TestAllocationRuleFilterTranslationFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: allocationRuleConfig(api, allocationRuleEC2Query, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/cost-filters/translate",
						fakeapi.Problem(http.StatusBadRequest, "CostFilter.Invalid", "Unknown field costs.servce"))
				},
				Config:      allocationRuleConfig(api, allocationRuleEC2Query, ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to compare the allocation rule filter(.|\n)*Unknown field costs.servce`),
			},
		},
	})
}

// The API refuses a rule filter that constrains nothing or reads a virtual tag; the provider
// surfaces its answer rather than second-guessing the grammar it does not own.
func TestAllocationRuleCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)
	api.FailNext(http.MethodPost, "/v1/allocation/rules",
		fakeapi.Problem(http.StatusBadRequest, "Validation", "An allocation rule cannot match on a virtual tag."))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      allocationRuleConfig(api, "virtual_tags->>'team' = 'checkout'", ""),
				ExpectError: regexp.MustCompile(`Failed to create allocation rule(.|\n)*cannot match on a virtual tag`),
			},
		},
	})
}

// createWhoseFilterTranslationFailsIsTainted applies config, which declares one resource at address
// with a CQL filter, while the translation that compares it with the stored filter after the create
// is refused. The object exists once the create returns, so its ID is kept in state, tainted: the
// next apply replaces it rather than creating a second object beside one nothing manages.
func createWhoseFilterTranslationFailsIsTainted(t *testing.T, api *fakeapi.Server, collection, idPrefix, address, config, summary string) {
	t.Helper()
	// The fake numbers IDs from 1, so the object this test creates is the first.
	first := idPrefix + fmt.Sprintf("%022d", 1)
	api.FailNext(http.MethodGet, "/v1/cost-filters/translate",
		fakeapi.Problem(http.StatusForbidden, "Forbidden", "The token cannot translate filters"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs(collection); len(ids) != 0 {
				return fmt.Errorf("%s left after destroy: %v", collection, ids)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(summary + `(.|\n)*The token cannot translate filters`),
			},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(address, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				Check: func(*terraform.State) error {
					if ids := api.IDs(collection); len(ids) != 1 || ids[0] == first {
						return fmt.Errorf("%s = %v, want only the replacement of %s", collection, ids, first)
					}
					return nil
				},
			},
		},
	})
}

func TestAllocationRuleCreateWhoseFilterTranslationFailsIsTainted(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)
	createWhoseFilterTranslationFailsIsTainted(t, api, "/v1/allocation/rules", "alr_", "costfluent_allocation_rule.r",
		allocationRuleConfig(api, allocationRuleEC2Query, ""), "Failed to compare the allocation rule filter")
}

func TestAllocationRuleDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: allocationRuleConfig(api, allocationRuleEC2Query, "")},
			{
				PreConfig:          func() { api.Remove("/v1/allocation/rules", allocationRuleOnlyID(t, api)) },
				Config:             allocationRuleConfig(api, allocationRuleEC2Query, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestAllocationRuleUpdateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	allocationRuleFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: allocationRuleConfig(api, allocationRuleEC2Query, "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodPut, "/v1/allocation/rules/"+allocationRuleOnlyID(t, api),
						fakeapi.Problem(http.StatusBadRequest, "Allocation.CostKindMismatch",
							"A rule cannot move cost onto a shared segment"))
				},
				Config:      allocationRuleConfig(api, allocationRuleRDSQuery, ""),
				ExpectError: regexp.MustCompile(`Failed to update allocation rule(.|\n)*A rule cannot move cost onto a shared segment`),
			},
		},
	})
}

// The segment must be a segment token.
func TestAllocationRuleRefusesAForeignSegmentTokenAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	config := api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_allocation_rule" "r" {
  workspace_id = %q
  name         = "Checkout compute"
  filter       = %q
  segment_id   = "wsp_0000000000000000000001"
}
`, testWorkspace, allocationRuleEC2Query)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Expected token starting with "seg_"`),
			},
		},
	})
}
