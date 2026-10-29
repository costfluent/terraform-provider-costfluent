package resources_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// versionedResource is one resource the API keeps a row version for, as its wire tests fake it:
// config renders it with field set to value, and an edit made elsewhere changes field.
type versionedResource struct {
	kind   string
	path   string
	method string
	field  string
	fake   func(api *fakeapi.Server)
	config func(api *fakeapi.Server, value string) string
}

func versionedResources() []versionedResource {
	return []versionedResource{
		{"Allocation rule", "/v1/allocation/rules", http.MethodPut, "name", allocationRuleFake, func(api *fakeapi.Server, v string) string {
			return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_allocation_rule" "r" {
  workspace_id = %q
  name         = %q
  filter       = %q
  segment_id   = %q
}
`, testWorkspace, v, allocationRuleEC2Query, allocationRuleSegment)
		}},
		{"App", "/v1/apps", http.MethodPut, "name", appFake, func(api *fakeapi.Server, v string) string {
			return appConfigBlock(api, v, fmt.Sprintf(`{ webhookUrl = %q }`, slackHook))
		}},
		{"Budget", "/v1/budgets", http.MethodPut, "name", fakeBudgets, func(api *fakeapi.Server, v string) string {
			return budgetConfig(api, v, "500", "")
		}},
		{"Business metric", "/v1/business-metrics", http.MethodPut, "name", businessMetricFake, func(api *fakeapi.Server, v string) string {
			return businessMetricConfig(api, v, "")
		}},
		{"Cost alert", "/v1/cost-alerts", http.MethodPut, "name", fakeCostAlerts, func(api *fakeapi.Server, v string) string {
			return costAlertConfig(api, v, 10, "")
		}},
		{"Cost report", "/v1/cost-reports", http.MethodPut, "title", costReportFake, func(api *fakeapi.Server, v string) string {
			return costReportConfig(api, fmt.Sprintf("  title = %q\n", v))
		}},
		{"Dashboard", "/v1/dashboards", http.MethodPut, "title", dashboardFake, func(api *fakeapi.Server, v string) string {
			return dashboardConfig(api, v, "")
		}},
		{"Dashboard widget", widgetsOf(testDashboard), http.MethodPut, "title", widgetFake, func(api *fakeapi.Server, v string) string {
			return widgetConfig(api, reportWidget(testReport, v, ""))
		}},
		{"Folder", "/v1/folders", http.MethodPut, "title", fakeFolders, folderConfig},
		{"Provider", "/v1/providers", http.MethodPut, "displayName", fakeProviders, func(api *fakeapi.Server, v string) string {
			return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_provider" "aws" {
  key          = "aws"
  display_name = %q
  credentials  = { role_arn = %q }
}
`, v, providerRoleArn)
		}},
		{"Report notification", "/v1/cost-report-subscriptions", http.MethodPatch, "name", subscriptionFake, func(api *fakeapi.Server, v string) string {
			return api.ProviderBlock() + fmt.Sprintf(`
resource "costfluent_cost_report_subscription" "s" {
  workspace_id   = %q
  cost_report_id = %q
  name           = %q
  schedule_type  = "weekly"
  app_ids        = [%q]
}
`, testWorkspace, subscriptionReport, v, subscriptionApp)
		}},
		{"Saved filter", "/v1/saved-filters", http.MethodPut, "title", savedFilterFake, func(api *fakeapi.Server, v string) string {
			return savedFilterConfig(api, v, "")
		}},
		{"Savings view", savingsViewPath, http.MethodPut, "title", savingsViewFake, func(api *fakeapi.Server, v string) string {
			return savingsViewConfig(api, v, "")
		}},
		{"Segment", "/v1/segments", http.MethodPut, "product", segmentFake, func(api *fakeapi.Server, v string) string {
			return segmentConfig(api, "Payments", v, "")
		}},
		{"Team", "/v1/teams", http.MethodPut, "name", func(api *fakeapi.Server) { teamFake(api) }, func(api *fakeapi.Server, v string) string {
			return teamConfig(api, v, "")
		}},
		{"Virtual tag", "/v1/virtual-tags", http.MethodPatch, "description", virtualTagFake, func(api *fakeapi.Server, v string) string {
			return virtualTagConfig(api, fmt.Sprintf("  description = %q", v))
		}},
		{"Workspace", "/v1/workspaces", http.MethodPut, "name", fakeWorkspaces, func(api *fakeapi.Server, v string) string {
			return workspaceConfig(api, v, "")
		}},
	}
}

// Every write and deletion sends back, as If-Match, the ETag of Terraform's last read or write.
// A change made elsewhere after that read lands before the write and is refused, not overwritten:
// the error says so and the change survives, and the next plan, reading it, applies over it.
func TestVersionedResourcesRefuseToOverwriteAChangeMadeAfterTheirRead(t *testing.T) {
	t.Parallel()
	for _, r := range versionedResources() {
		t.Run(r.kind, func(t *testing.T) {
			t.Parallel()
			api := fakeapi.New(t)
			r.fake(api)
			only := func() string {
				t.Helper()
				ids := api.IDs(r.path)
				if len(ids) != 1 {
					t.Fatalf("%s = %v, want one", r.path, ids)
				}
				return ids[0]
			}
			elsewhere := func(method string) func() {
				return func() {
					id := only()
					api.BeforeNext(method, r.path+"/"+id, func() {
						api.Update(r.path, id, func(obj map[string]any) { obj[r.field] = "Changed elsewhere" })
					})
				}
			}
			var readTag string
			changeKept := func(*terraform.State) error {
				if obj, _ := api.Get(r.path, only()); obj[r.field] != "Changed elsewhere" {
					return fmt.Errorf("%s = %v after the refused write, want the change made elsewhere kept", r.field, obj[r.field])
				}
				return nil
			}
			refused := regexp.MustCompile(r.kind + ` changed outside Terraform`)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: r.config(api, "First"),
						Check: func(*terraform.State) error {
							readTag = api.ETag(r.path, only())
							return nil
						},
					},
					{
						Config: r.config(api, "Second"),
						Check: func(*terraform.State) error {
							write := api.LastRequest(r.method, r.path+"/"+only())
							if got := write.Header.Get("If-Match"); got == "" || got != readTag {
								return fmt.Errorf("%s sent If-Match %q, want the ETag of the last read %q", r.method, got, readTag)
							}
							return nil
						},
					},
					{
						PreConfig:   elsewhere(r.method),
						Config:      r.config(api, "Third"),
						ExpectError: refused,
					},
					{
						Config: r.config(api, "Third"),
						PreConfig: func() {
							if err := changeKept(nil); err != nil {
								t.Error(err)
							}
						},
						Check: func(*terraform.State) error {
							if obj, _ := api.Get(r.path, only()); obj[r.field] != "Third" {
								return fmt.Errorf("%s = %v, want the next apply to set it", r.field, obj[r.field])
							}
							return nil
						},
					},
					{
						PreConfig:   elsewhere(http.MethodDelete),
						Config:      api.ProviderBlock(),
						ExpectError: refused,
					},
					{
						Config: api.ProviderBlock(),
						PreConfig: func() {
							if err := changeKept(nil); err != nil {
								t.Error(err)
							}
						},
						Check: func(*terraform.State) error {
							if ids := api.IDs(r.path); len(ids) != 0 {
								return fmt.Errorf("%s = %v, want the next apply to delete it", r.path, ids)
							}
							return nil
						},
					},
				},
			})
		})
	}
}
