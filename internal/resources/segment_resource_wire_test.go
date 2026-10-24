package resources_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// segmentFake serves /v1/segments with the SegmentResponse shape. Like the API, a shared segment
// keeps no team, product or cost centre, stores its label trimmed and its split with a percent only
// on a percent split, and refuses with 400 an update naming a team, product or cost centre; a direct
// one keeps an empty label when it names a team, has no split and refuses a label or split.
func segmentFake(api *fakeapi.Server) {
	var mu sync.Mutex
	var refusal string
	api.Collection("/v1/segments", fakeapi.Collection{
		IDPrefix:  "seg_",
		Versioned: true,
		Scope:     fakeapi.WorkspaceScoped,
		Create: func(body map[string]any) map[string]any {
			if body["isShared"] == true {
				label, _ := body["label"].(string)
				return map[string]any{"teamLabel": "", "product": "", "costCentre": "", "isShared": true,
					"label": strings.TrimSpace(label), "split": storedSplit(body["split"]),
					"createdAt": "2026-10-03T10:00:00Z"}
			}
			obj := map[string]any{
				"teamLabel":  "",
				"product":    body["product"],
				"costCentre": body["costCentre"],
				"isShared":   false,
				"label":      "",
				"split":      nil,
				"createdAt":  "2026-10-03T10:00:00Z",
			}
			if label, ok := body["teamLabel"].(string); ok {
				obj["teamLabel"] = label
			}
			if team, ok := body["teamId"].(string); ok {
				obj["teamId"] = team
				obj["teamName"] = "Platform"
			}
			return obj
		},
		Apply: func(obj, body map[string]any) {
			mu.Lock()
			defer mu.Unlock()
			if obj["isShared"] == true {
				for _, k := range []string{"teamId", "teamLabel", "product", "costCentre"} {
					if v, ok := body[k]; ok && v != nil {
						refusal = "A shared segment names no team, product or cost centre; change its label or split instead."
						return
					}
				}
				if label, ok := body["label"].(string); ok {
					obj["label"] = strings.TrimSpace(label)
				}
				if split, ok := body["split"]; ok && split != nil {
					obj["split"] = storedSplit(split)
				}
				obj["updatedAt"] = "2026-10-04T11:00:00Z"
				return
			}
			if label, _ := body["label"].(string); strings.TrimSpace(label) != "" || body["split"] != nil {
				refusal = "A direct segment carries no label or split."
				return
			}
			for _, k := range []string{"teamLabel", "product", "costCentre"} {
				if v, ok := body[k]; ok && v != nil {
					obj[k] = v
				}
			}
			obj["updatedAt"] = "2026-10-04T11:00:00Z"
		},
		Updated: func(obj map[string]any) fakeapi.Response {
			mu.Lock()
			defer mu.Unlock()
			if refusal != "" {
				detail := refusal
				refusal = ""
				return fakeapi.Problem(http.StatusBadRequest, "Validation", detail)
			}
			return fakeapi.JSON(obj)
		},
	})
}

// storedSplit is a split as the API answers it: proportional when none was sent, every target with
// its percent, null unless the split is a percent split.
func storedSplit(sent any) map[string]any {
	split, _ := sent.(map[string]any)
	method, _ := split["method"].(string)
	if method == "" {
		method = "proportional"
	}
	targets := []any{}
	sentTargets, _ := split["targets"].([]any)
	for _, t := range sentTargets {
		target, _ := t.(map[string]any)
		var percent any
		if method == "percent" {
			percent = target["percent"]
		}
		targets = append(targets, map[string]any{"segmentId": target["segmentId"], "percent": percent})
	}
	return map[string]any{"method": method, "targets": targets}
}

func segmentConfig(api *fakeapi.Server, label, product, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  team_label   = %q
  product      = %q
  cost_centre  = "CC-100"
%s}
`, testWorkspace, label, product, extra)
}

func segmentOnlyID(t *testing.T, api *fakeapi.Server) string {
	t.Helper()
	ids := api.IDs("/v1/segments")
	if len(ids) != 1 {
		t.Fatalf("segments = %v, want one", ids)
	}
	return ids[0]
}

func TestSegmentLifecycleSendsTheContractShape(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: segmentConfig(api, "Platform", "Checkout", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "team_label", "Platform"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Checkout"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "cost_centre", "CC-100"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "is_shared", "false"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "created_at", "2026-10-03T10:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_id"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_name"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "updated_at"),
					resource.TestMatchResourceAttr("costfluent_segment.s", "id", regexp.MustCompile(`^seg_.{22}$`)),
					func(*terraform.State) error {
						create := api.LastRequest(http.MethodPost, "/v1/segments")
						if create.Body["workspaceId"] != testWorkspace || create.Body["teamLabel"] != "Platform" ||
							create.Body["product"] != "Checkout" || create.Body["costCentre"] != "CC-100" {
							return fmt.Errorf("create body = %s", create.Raw)
						}
						for _, unset := range []string{"teamId", "isShared", "label", "split"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("create sent unset %s: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				Config: segmentConfig(api, "Platform", "Payments", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Payments"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "updated_at", "2026-10-04T11:00:00Z"),
					func(*terraform.State) error {
						// The refresh before this update read the segment from its workspace's listing.
						read := api.LastRequest(http.MethodGet, "/v1/segments")
						if read.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("read query = %v", read.Query)
						}
						update := api.LastRequest(http.MethodPut, "/v1/segments/"+segmentOnlyID(t, api))
						if update.Query.Get("workspaceId") != testWorkspace {
							return fmt.Errorf("update query = %v", update.Query)
						}
						// The update replaces the segment's name whole, so unchanged parts are sent too.
						if update.Body["teamLabel"] != "Platform" || update.Body["product"] != "Payments" ||
							update.Body["costCentre"] != "CC-100" {
							return fmt.Errorf("update body = %s", update.Raw)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_segment.s",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + segmentOnlyID(t, api), nil
				},
			},
			{
				// A segment is addressable only inside its workspace, so a bare ID is refused.
				ResourceName:  "costfluent_segment.s",
				ImportState:   true,
				ImportStateId: "seg_0000000000000000000001",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
			{
				// Turning a direct segment into the shared bucket changes what every rule pointing at it
				// means, so it replaces the segment.
				Config:             segmentConfig(api, "Platform", "Payments", "  is_shared = true\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPreRefresh: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.s", plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
			},
		},
		CheckDestroy: func(*terraform.State) error {
			if ids := api.IDs("/v1/segments"); len(ids) != 0 {
				return fmt.Errorf("segments left after destroy: %v", ids)
			}
			return nil
		},
	})
}

// The read finds a segment in the workspace's listing, so one missing from it leaves state.
// The API has no read by ID for a segment, so the provider finds it in the workspace's list, which
// it has to walk past the first page. The fake pages at the API's sizes, and IDs list in order, so
// the managed segment comes after 150 others and the largest page holds 100.
func TestSegmentIsFoundPastTheFirstPage(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)
	for range 150 {
		api.PutIn("/v1/segments", testWorkspace, api.NextID("seg_"), map[string]any{
			"teamLabel": "Other", "product": "Other", "costCentre": "CC-0", "isShared": false,
			"createdAt": "2026-10-01T10:00:00Z",
		})
	}
	config := segmentConfig(api, "Checkout", "Payments", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: func(*terraform.State) error {
					var pages []string
					for _, r := range api.Requests(http.MethodGet, "/v1/segments") {
						pages = append(pages, r.Query.Get("page"))
					}
					if !slices.Contains(pages, "2") {
						return fmt.Errorf("listed pages %v; the read never left page 1", pages)
					}
					return nil
				},
			},
		},
	})
}

func TestSegmentDeletedOutsideTerraformIsRecreated(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: segmentConfig(api, "Platform", "Checkout", "")},
			{
				PreConfig:          func() { api.Remove("/v1/segments", segmentOnlyID(t, api)) },
				Config:             segmentConfig(api, "Platform", "Checkout", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestSegmentReadFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: segmentConfig(api, "Platform", "Checkout", "")},
			{
				PreConfig: func() {
					api.FailNext(http.MethodGet, "/v1/segments",
						fakeapi.Problem(http.StatusForbidden, "Forbidden", "Cost allocation is not on this plan"))
				},
				Config:      segmentConfig(api, "Platform", "Checkout", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Failed to read segment(.|\n)*Cost allocation is not on this plan`),
			},
		},
	})
}

func TestSegmentCreateFailureSurfacesTheAPIError(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)
	api.FailNext(http.MethodPost, "/v1/segments",
		fakeapi.Problem(http.StatusConflict, "Segment.Duplicate", "A segment with this name already exists"))

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      segmentConfig(api, "Platform", "Checkout", ""),
				ExpectError: regexp.MustCompile(`Failed to create segment(.|\n)*A segment with this name already exists`),
			},
		},
	})
}

// Team public IDs are team_<22 characters>. A segment naming one stores an empty label, which
// reads back as the null the configuration holds.
func TestSegmentAcceptsATeamID(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  team_id      = "tem_0000000000000000000001"
  product      = "Payments"
  cost_centre  = "CC-100"
}
`, testWorkspace),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Expected token starting with "team_"`),
			},
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  team_id      = "team_0000000000000000000001"
  product      = "Checkout"
  cost_centre  = "CC-100"
}
`, testWorkspace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "team_id", "team_0000000000000000000001"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "team_name", "Platform"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_label"),
				),
			},
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  team_id      = "team_0000000000000000000001"
  product      = "Payments"
  cost_centre  = "CC-100"
}
`, testWorkspace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Payments"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_label"),
				),
			},
		},
	})
}

// The API stores a shared segment with an empty team label, product and cost centre whatever was
// sent, so the provider keeps the configured values rather than reporting an inconsistent result.
func TestSegmentSharedBucketApplies(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  product      = "Shared"
  cost_centre  = "CC-000"
  is_shared    = true
}
`, testWorkspace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "is_shared", "true"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Shared"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "cost_centre", "CC-000"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_label"),
				),
			},
		},
	})
}

// The API keeps a segment's owner when an update names none, so a direct segment configured without
// one is refused at plan, as the API refuses it on create, rather than applied as a no-op.
func TestSegmentWithoutAnOwnerIsRefusedAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  product      = "Checkout"
  cost_centre  = "CC-100"
}
`, testWorkspace),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`set team_id or team_label`),
			},
		},
	})
}

func sharedSegmentConfig(api *fakeapi.Server, product, costCentre, extra string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  product      = %q
  cost_centre  = %q
  is_shared    = true
%s}
`, testWorkspace, product, costCentre, extra)
}

func noSegmentUpdate(api *fakeapi.Server) resource.TestCheckFunc {
	return func(*terraform.State) error {
		for _, id := range api.IDs("/v1/segments") {
			if sent := api.Requests(http.MethodPut, "/v1/segments/"+id); len(sent) != 0 {
				return fmt.Errorf("a shared segment was sent %d updates: %s", len(sent), sent[0].Raw)
			}
		}
		return nil
	}
}

// The API stores none of a shared segment's product, cost centre or label and refuses every update
// of one, so editing them changes only state. Each step's apply is followed by a plan that must be
// empty, which holds the edited values in state.
func TestSegmentSharedBucketEditsNeverSendAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: sharedSegmentConfig(api, "Shared", "CC-000", "")},
			{
				Config: sharedSegmentConfig(api, "Platform overhead", "CC-000", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.s", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Platform overhead"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "created_at", "2026-10-03T10:00:00Z"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "updated_at"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "team_name"),
					resource.TestMatchResourceAttr("costfluent_segment.s", "id", regexp.MustCompile(`^seg_.{22}$`)),
					noSegmentUpdate(api),
				),
			},
			{
				Config: sharedSegmentConfig(api, "Platform overhead", "CC-900", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "cost_centre", "CC-900"),
					noSegmentUpdate(api),
				),
			},
			{
				Config: sharedSegmentConfig(api, "Platform overhead", "CC-900", "  team_label   = \"Shared pool\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "team_label", "Shared pool"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "is_shared", "true"),
					noSegmentUpdate(api),
				),
			},
		},
	})
}

// An imported shared segment reads back with no product or cost centre, since the API stores none,
// so the first apply after the import fills them from the configuration without an update.
func TestSegmentImportedSharedBucketAppliesWithoutAnUpdate(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)
	id := api.NextID("seg_")
	api.PutIn("/v1/segments", testWorkspace, id, map[string]any{
		"teamLabel": "", "product": "", "costCentre": "", "isShared": true,
		"createdAt": "2026-10-01T10:00:00Z",
	})
	config := sharedSegmentConfig(api, "Shared", "CC-000", "")

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "costfluent_segment.s",
				ImportState:        true,
				ImportStateId:      testWorkspace + ":" + id,
				ImportStatePersist: true,
			},
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "id", id),
					resource.TestCheckResourceAttr("costfluent_segment.s", "product", "Shared"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "cost_centre", "CC-000"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "created_at", "2026-10-01T10:00:00Z"),
					noSegmentUpdate(api),
				),
			},
		},
	})
}

// sharedSplitConfig is two direct segments and a shared segment configured with body, which can
// name them as costfluent_segment.a and costfluent_segment.b.
func sharedSplitConfig(api *fakeapi.Server, body string) string {
	return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "a" {
  workspace_id = %[1]q
  team_label   = "Checkout"
  product      = "Checkout"
  cost_centre  = "CC-100"
}

resource "costfluent_segment" "b" {
  workspace_id = %[1]q
  team_label   = "Payments"
  product      = "Payments"
  cost_centre  = "CC-200"
}

resource "costfluent_segment" "s" {
  workspace_id = %[1]q
  is_shared    = true
%[2]s}
`, testWorkspace, body)
}

// sharedSegmentRequests are the requests the shared segment received: its create, then its updates.
func sharedSegmentRequests(api *fakeapi.Server, st *terraform.State) (create fakeapi.Request, updates []fakeapi.Request, err error) {
	shared, ok := st.RootModule().Resources["costfluent_segment.s"]
	if !ok {
		return create, nil, fmt.Errorf("costfluent_segment.s not in state")
	}
	for _, r := range api.Requests(http.MethodPost, "/v1/segments") {
		if r.Body["isShared"] == true {
			create = r
		}
	}
	return create, api.Requests(http.MethodPut, "/v1/segments/"+shared.Primary.ID), nil
}

// sentSplit is the split a request body carried, as JSON, so a check compares it whole.
func sentSplit(r fakeapi.Request) string {
	raw, _ := json.Marshal(r.Body["split"])
	return string(raw)
}

func segmentIDs(st *terraform.State) (a, b string) {
	return st.RootModule().Resources["costfluent_segment.a"].Primary.ID,
		st.RootModule().Resources["costfluent_segment.b"].Primary.ID
}

// A labelled shared segment is created with its percent split, the targets in configured order and
// each percent a JSON number; changing the split to even updates it in place with no percent and
// nothing the shared segment does not store; it imports with its label and split; and unsetting
// both sends an empty label and a proportional split. Every apply is followed by a plan that must be
// empty, which holds a percent such as 33.33 read back from the API to the configured value.
func TestSegmentSharedLabelAndSplit(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	percent := `  label        = "Cluster"
  split = {
    method = "percent"
    targets = [
      { segment_id = costfluent_segment.b.id, percent = 33.33 },
      { segment_id = costfluent_segment.a.id, percent = 66.67 },
    ]
  }
`
	even := `  label        = "Cluster"
  split = {
    method  = "even"
    targets = [{ segment_id = costfluent_segment.b.id }, { segment_id = costfluent_segment.a.id }]
  }
`
	var sharedID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: sharedSplitConfig(api, percent),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "label", "Cluster"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.method", "percent"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.targets.#", "2"),
					resource.TestCheckResourceAttrPair("costfluent_segment.s", "split.targets.0.segment_id", "costfluent_segment.b", "id"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.targets.0.percent", "33.33"),
					resource.TestCheckResourceAttrPair("costfluent_segment.s", "split.targets.1.segment_id", "costfluent_segment.a", "id"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.targets.1.percent", "66.67"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "product"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "cost_centre"),
					resource.TestCheckNoResourceAttr("costfluent_segment.a", "label"),
					resource.TestCheckNoResourceAttr("costfluent_segment.a", "split"),
					func(st *terraform.State) error {
						sharedID = st.RootModule().Resources["costfluent_segment.s"].Primary.ID
						create, _, err := sharedSegmentRequests(api, st)
						if err != nil {
							return err
						}
						a, b := segmentIDs(st)
						want := fmt.Sprintf(`{"method":"percent","targets":[{"percent":33.33,"segmentId":%q},{"percent":66.67,"segmentId":%q}]}`, b, a)
						if create.Body["label"] != "Cluster" || sentSplit(create) != want {
							return fmt.Errorf("create body = %s, want label Cluster and split %s", create.Raw, want)
						}
						for _, unset := range []string{"teamId", "teamLabel", "product", "costCentre"} {
							if _, sent := create.Body[unset]; sent {
								return fmt.Errorf("a shared segment's create sent %s: %s", unset, create.Raw)
							}
						}
						return nil
					},
				),
			},
			{
				Config: sharedSplitConfig(api, even),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.s", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.method", "even"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.targets.#", "2"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "split.targets.0.percent"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "updated_at", "2026-10-04T11:00:00Z"),
					func(st *terraform.State) error {
						if id := st.RootModule().Resources["costfluent_segment.s"].Primary.ID; id != sharedID {
							return fmt.Errorf("shared segment %s, want %s updated in place", id, sharedID)
						}
						_, updates, err := sharedSegmentRequests(api, st)
						if err != nil {
							return err
						}
						if len(updates) != 1 {
							return fmt.Errorf("updates = %d, want 1", len(updates))
						}
						a, b := segmentIDs(st)
						want := fmt.Sprintf(`{"method":"even","targets":[{"segmentId":%q},{"segmentId":%q}]}`, b, a)
						if sentSplit(updates[0]) != want || len(updates[0].Body) != 1 {
							return fmt.Errorf("update body = %s, want the split %s alone", updates[0].Raw, want)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "costfluent_segment.s",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return testWorkspace + ":" + sharedID, nil
				},
			},
			{
				Config: sharedSplitConfig(api, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "label"),
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "split"),
					func(st *terraform.State) error {
						_, updates, err := sharedSegmentRequests(api, st)
						if err != nil {
							return err
						}
						if len(updates) != 2 {
							return fmt.Errorf("updates = %d, want 2", len(updates))
						}
						last := updates[1]
						if label, sent := last.Body["label"]; !sent || label != "" || sentSplit(last) != `{"method":"proportional"}` {
							return fmt.Errorf("update body = %s, want an empty label and a proportional split", last.Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// A split changed outside Terraform reads back as it is stored, so the next plan puts the
// configured one back in place.
func TestSegmentSplitChangedOutsideTerraformIsRestored(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)
	config := sharedSplitConfig(api, `  split = {
    method  = "even"
    targets = [{ segment_id = costfluent_segment.a.id }, { segment_id = costfluent_segment.b.id }]
  }
`)
	var sharedID string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(st *terraform.State) error {
					sharedID = st.RootModule().Resources["costfluent_segment.s"].Primary.ID
					return nil
				},
			},
			{
				PreConfig: func() {
					api.Update("/v1/segments", sharedID, func(obj map[string]any) {
						obj["label"] = "Renamed"
						obj["split"] = map[string]any{"method": "proportional", "targets": []any{}}
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("costfluent_segment.s", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("costfluent_segment.s", "label"),
					resource.TestCheckResourceAttr("costfluent_segment.s", "split.method", "even"),
					func(st *terraform.State) error {
						_, updates, err := sharedSegmentRequests(api, st)
						if err != nil {
							return err
						}
						if len(updates) != 1 {
							return fmt.Errorf("updates = %d, want 1", len(updates))
						}
						if updates[0].Body["label"] != "" || !strings.Contains(sentSplit(updates[0]), `"method":"even"`) {
							return fmt.Errorf("update body = %s, want the label cleared and the even split", updates[0].Raw)
						}
						return nil
					},
				),
			},
		},
	})
}

// The API refuses these on apply, so the provider refuses them at plan.
func TestSegmentLabelAndSplitAreValidatedAtPlan(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)

	const first, second = "seg_0000000000000000000001", "seg_0000000000000000000002"
	shared := func(body string) string {
		return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  is_shared    = true
%s}
`, testWorkspace, body)
	}
	split := func(method, targets string) string {
		return shared(fmt.Sprintf("  split = {\n    method  = %q\n    targets = [%s]\n  }\n", method, targets))
	}
	target := func(id, percent string) string {
		if percent == "" {
			return fmt.Sprintf("{ segment_id = %q }, ", id)
		}
		return fmt.Sprintf("{ segment_id = %q, percent = %s }, ", id, percent)
	}

	cases := []struct {
		config string
		err    string
	}{
		{segmentConfig(api, "Platform", "Checkout", "  label = \"Cluster\"\n"), `Direct segment takes no label`},
		{segmentConfig(api, "Platform", "Checkout", "  split = { method = \"proportional\" }\n"), `Direct segment takes no split`},
		{api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_segment" "s" {
  workspace_id = %q
  team_label   = "Platform"
}
`, testWorkspace), `Segment has no product(.|\n)*Segment has no cost centre`},
		{shared(`  label = " Cluster"` + "\n"), `must not be empty or start or end with a space`},
		{shared(`  label = ""` + "\n"), `must not be empty or start or end with a space`},
		{split("weighted", ""), `value must be one of`},
		{split("proportional", target(first, "")), `Proportional split names no targets`},
		{split("even", target(first, "")), `Even split needs two targets`},
		{split("even", target(first, "50")+target(second, "50")), `Even split takes no percent`},
		{split("even", target(first, "")+target(first, "")), `Split names a target twice`},
		{split("percent", ""), `Percent split needs targets`},
		{split("percent", target(first, "")+target(second, "100")), `Percent split target needs a percent`},
		{split("percent", target(first, "33.333")+target(second, "66.667")), `Invalid split percent`},
		{split("percent", target(first, "0")+target(second, "100")), `Invalid split percent`},
		{split("percent", target(first, "-10")+target(second, "110")), `Invalid split percent`},
		{split("percent", target(first, "33.33")+target(second, "66.66")), `sum to exactly 100; these sum to 99.99`},
	}
	steps := make([]resource.TestStep, len(cases))
	for i, c := range cases {
		steps[i] = resource.TestStep{Config: c.config, PlanOnly: true, ExpectError: regexp.MustCompile(c.err)}
	}
	// Three percents with two decimals summing to 100 in hundredths pass, though not in float64.
	steps = append(steps, resource.TestStep{
		Config:   split("percent", target(first, "0.01")+target(second, "64.04")+target("seg_0000000000000000000003", "35.95")),
		PlanOnly: true, ExpectNonEmptyPlan: true,
	})

	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: factories, Steps: steps})
}

// Terraform reads a configured 33.33 back from state at a higher precision than the float64 the
// API's answer decodes to. A refresh that took the two for different numbers would treat the
// segment as changed elsewhere and drop the ETag, so the next update could overwrite a change made
// after the read.
func TestSegmentPercentSplitKeepsItsETagAcrossARefresh(t *testing.T) {
	t.Parallel()
	api := fakeapi.New(t)
	segmentFake(api)
	config := func(label string) string {
		return sharedSplitConfig(api, fmt.Sprintf(`  label = %q
  split = {
    method = "percent"
    targets = [
      { segment_id = costfluent_segment.a.id, percent = 33.33 },
      { segment_id = costfluent_segment.b.id, percent = 66.67 },
    ]
  }
`, label))
	}
	var sharedID, readTag string

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("Cluster"),
				Check: func(st *terraform.State) error {
					sharedID = st.RootModule().Resources["costfluent_segment.s"].Primary.ID
					readTag = api.ETag("/v1/segments", sharedID)
					return nil
				},
			},
			{
				Config: config("Cluster EU"),
				Check: func(*terraform.State) error {
					write := api.LastRequest(http.MethodPut, "/v1/segments/"+sharedID)
					if got := write.Header.Get("If-Match"); got == "" || got != readTag {
						return fmt.Errorf("update sent If-Match %q, want the ETag of the last read %q", got, readTag)
					}
					if write.Body["label"] != "Cluster EU" || len(write.Body) != 1 {
						return fmt.Errorf("update body = %s, want the label alone", write.Raw)
					}
					return nil
				},
			},
		},
	})
}
