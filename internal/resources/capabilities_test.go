package resources_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

// resourceOperations is every Public API operation each action of a resource sends through the
// SDK, read-backs included, and last the calls only some configurations send (a CQL filter's
// translation, a saved filter or settings applied right after a create, a link created disabled),
// in the order its declared needs name their capabilities. Import is left out: it reads with what
// Read needs.
var resourceOperations = map[string]struct {
	new                                       func() fwresource.Resource
	create, read, update, delete, conditional []string
}{
	"costfluent_allocation_rule": {resources.NewAllocationRuleResource,
		[]string{"POST /v1/allocation/rules"},
		[]string{"GET /v1/allocation/rules"},
		[]string{"PUT /v1/allocation/rules/{ruleId}"},
		[]string{"DELETE /v1/allocation/rules/{ruleId}"},
		[]string{"GET /v1/cost-filters/translate"}},
	"costfluent_app": {resources.NewAppResource,
		[]string{"POST /v1/apps"},
		[]string{"GET /v1/apps/{appId}"},
		[]string{"PUT /v1/apps/{appId}"},
		[]string{"DELETE /v1/apps/{appId}"},
		nil},
	"costfluent_budget": {resources.NewBudgetResource,
		[]string{"POST /v1/budgets"},
		[]string{"GET /v1/budgets/{budgetId}"},
		[]string{"PUT /v1/budgets/{budgetId}", "GET /v1/budgets/{budgetId}"},
		[]string{"DELETE /v1/budgets/{budgetId}"},
		nil},
	"costfluent_business_metric": {resources.NewBusinessMetricResource,
		[]string{"POST /v1/business-metrics", "GET /v1/business-metrics/{metricId}"},
		[]string{"GET /v1/business-metrics/{metricId}"},
		[]string{"PUT /v1/business-metrics/{metricId}"},
		[]string{"DELETE /v1/business-metrics/{metricId}"},
		nil},
	"costfluent_cost_alert": {resources.NewCostAlertResource,
		[]string{"POST /v1/cost-alerts", "GET /v1/cost-alerts/{alertId}", "POST /v1/cost-alerts/{alertId}/pause"},
		[]string{"GET /v1/cost-alerts/{alertId}"},
		[]string{"PUT /v1/cost-alerts/{alertId}", "GET /v1/cost-alerts/{alertId}", "POST /v1/cost-alerts/{alertId}/pause", "POST /v1/cost-alerts/{alertId}/resume"},
		[]string{"DELETE /v1/cost-alerts/{alertId}"},
		[]string{"GET /v1/cost-filters/translate"}},
	"costfluent_cost_report": {resources.NewCostReportResource,
		[]string{"POST /v1/cost-reports", "GET /v1/cost-reports/{costReportId}"},
		[]string{"GET /v1/cost-reports/{costReportId}"},
		[]string{"PUT /v1/cost-reports/{costReportId}", "GET /v1/cost-reports/{costReportId}"},
		[]string{"DELETE /v1/cost-reports/{costReportId}"},
		[]string{"GET /v1/cost-filters/translate"}},
	"costfluent_cost_report_subscription": {resources.NewCostReportSubscriptionResource,
		[]string{"POST /v1/cost-report-subscriptions", "GET /v1/cost-report-subscriptions/{subscriptionId}", "POST /v1/cost-report-subscriptions/{subscriptionId}/deactivate"},
		[]string{"GET /v1/cost-report-subscriptions/{subscriptionId}"},
		[]string{"PATCH /v1/cost-report-subscriptions/{subscriptionId}", "GET /v1/cost-report-subscriptions/{subscriptionId}", "POST /v1/cost-report-subscriptions/{subscriptionId}/activate", "POST /v1/cost-report-subscriptions/{subscriptionId}/deactivate"},
		[]string{"DELETE /v1/cost-report-subscriptions/{subscriptionId}"},
		nil},
	"costfluent_dashboard": {resources.NewDashboardResource,
		[]string{"POST /v1/dashboards", "GET /v1/dashboards/{dashboardId}"},
		[]string{"GET /v1/dashboards/{dashboardId}"},
		[]string{"PUT /v1/dashboards/{dashboardId}", "PUT /v1/dashboards/{dashboardId}/saved-filter"},
		[]string{"DELETE /v1/dashboards/{dashboardId}"},
		[]string{"PUT /v1/dashboards/{dashboardId}/saved-filter"}},
	"costfluent_dashboard_widget": {resources.NewDashboardWidgetResource,
		[]string{"POST /v1/dashboards/{dashboardId}/widgets", "GET /v1/dashboards/{dashboardId}/widgets/{widgetId}"},
		[]string{"GET /v1/dashboards/{dashboardId}/widgets/{widgetId}"},
		[]string{"PUT /v1/dashboards/{dashboardId}/widgets/{widgetId}"},
		[]string{"DELETE /v1/dashboards/{dashboardId}/widgets/{widgetId}"},
		nil},
	"costfluent_folder": {resources.NewFolderResource,
		[]string{"POST /v1/folders", "GET /v1/folders/{folderId}"},
		[]string{"GET /v1/folders/{folderId}"},
		[]string{"PUT /v1/folders/{folderId}", "PUT /v1/folders/{folderId}/saved-filter"},
		[]string{"DELETE /v1/folders/{folderId}"},
		[]string{"PUT /v1/folders/{folderId}/saved-filter"}},
	"costfluent_gcp_service_account": {resources.NewGcpServiceAccountResource,
		[]string{"POST /v1/providers/gcp/service-account"},
		nil,
		nil,
		nil,
		nil},
	"costfluent_provider": {resources.NewProviderResource,
		[]string{"POST /v1/providers", "GET /v1/providers/{providerId}"},
		[]string{"GET /v1/providers/{providerId}"},
		[]string{"PUT /v1/providers/{providerId}"},
		[]string{"DELETE /v1/providers/{providerId}"},
		nil},
	"costfluent_saved_filter": {resources.NewSavedFilterResource,
		[]string{"POST /v1/saved-filters", "GET /v1/saved-filters/{filterId}"},
		[]string{"GET /v1/saved-filters/{filterId}"},
		[]string{"PUT /v1/saved-filters/{filterId}"},
		[]string{"DELETE /v1/saved-filters/{filterId}"},
		[]string{"GET /v1/cost-filters/translate"}},
	"costfluent_savings_view": {resources.NewSavingsViewResource,
		[]string{"POST /v1/savings-views"},
		[]string{"GET /v1/savings-views/{savingsViewId}"},
		[]string{"PUT /v1/savings-views/{savingsViewId}"},
		[]string{"DELETE /v1/savings-views/{savingsViewId}"},
		nil},
	"costfluent_segment": {resources.NewSegmentResource,
		[]string{"POST /v1/segments"},
		[]string{"GET /v1/segments"},
		[]string{"PUT /v1/segments/{segmentId}"},
		[]string{"DELETE /v1/segments/{segmentId}"},
		nil},
	"costfluent_tag_policy": {resources.NewTagPolicyResource,
		[]string{"PUT /v1/workspaces/{workspaceId}/tag-policy"},
		[]string{"GET /v1/workspaces/{workspaceId}/tag-policy"},
		[]string{"PUT /v1/workspaces/{workspaceId}/tag-policy"},
		[]string{"PUT /v1/workspaces/{workspaceId}/tag-policy"},
		nil},
	"costfluent_team": {resources.NewTeamResource,
		[]string{"POST /v1/teams"},
		[]string{"GET /v1/teams/{teamId}"},
		[]string{"PUT /v1/teams/{teamId}"},
		[]string{"DELETE /v1/teams/{teamId}"},
		nil},
	"costfluent_team_member": {resources.NewTeamMemberResource,
		[]string{"POST /v1/teams/{teamId}/members"},
		[]string{"GET /v1/teams/{teamId}"},
		[]string{"PUT /v1/teams/{teamId}/members/{userId}"},
		[]string{"DELETE /v1/teams/{teamId}/members/{userId}"},
		nil},
	"costfluent_team_workspace_access": {resources.NewTeamWorkspaceAccessResource,
		[]string{"PUT /v1/teams/{teamId}/workspaces/{workspaceId}"},
		[]string{"GET /v1/teams/{teamId}/workspaces"},
		[]string{"PUT /v1/teams/{teamId}/workspaces/{workspaceId}"},
		[]string{"DELETE /v1/teams/{teamId}/workspaces/{workspaceId}"},
		[]string{"PUT /v1/teams/{teamId}/workspaces/{workspaceId}/default-dashboard"}},
	"costfluent_virtual_tag": {resources.NewVirtualTagResource,
		[]string{"POST /v1/virtual-tags", "GET /v1/virtual-tags/{virtualTagId}"},
		[]string{"GET /v1/virtual-tags/{virtualTagId}"},
		[]string{"PATCH /v1/virtual-tags/{virtualTagId}", "GET /v1/virtual-tags/{virtualTagId}"},
		[]string{"DELETE /v1/virtual-tags/{virtualTagId}"},
		[]string{"GET /v1/cost-filters/translate"}},
	"costfluent_workspace": {resources.NewWorkspaceResource,
		[]string{"POST /v1/workspaces", "GET /v1/workspaces/{workspaceId}"},
		[]string{"GET /v1/workspaces/{workspaceId}"},
		[]string{"PUT /v1/workspaces/{workspaceId}"},
		[]string{"DELETE /v1/workspaces/{workspaceId}"},
		[]string{"PUT /v1/workspaces/{workspaceId}"}},
	"costfluent_workspace_provider": {resources.NewWorkspaceProviderResource,
		[]string{"POST /v1/workspace-providers"},
		[]string{"GET /v1/workspace-providers/{workspaceProviderId}"},
		[]string{"PUT /v1/workspace-providers/{workspaceProviderId}"},
		[]string{"DELETE /v1/workspace-providers/{workspaceProviderId}"},
		[]string{"PUT /v1/workspace-providers/{workspaceProviderId}"}},
}

type declaredOperation struct {
	Capabilities []string `json:"x-costfluent-capability"`
	Scope        string   `json:"x-costfluent-scope"`
	Description  string   `json:"description"`
}

// organizationCondition matches the sentence a workspace operation's description ends with when
// its last capability is needed on the organization for part of a change.
var organizationCondition = regexp.MustCompile("It also needs an organization token with `([A-Za-z]+)` [^\n]+\\.$")

// conditionalOrganization is the capability a workspace operation needs on the organization only
// for part of a change, the last of its x-costfluent-capability, or empty when it has none.
func (o declaredOperation) conditionalOrganization() string {
	match := organizationCondition.FindStringSubmatch(o.Description)
	if o.Scope != "workspace" || match == nil || len(o.Capabilities) == 0 || o.Capabilities[len(o.Capabilities)-1] != match[1] {
		return ""
	}
	return match[1]
}

// contract reads each operation's declared capabilities and scope, and every capability a token can
// carry, from the copy of the Public API contract the embedded SDK carries.
func contract(t *testing.T) (map[string]declaredOperation, map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile("../costfluent/testdata/public.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths      map[string]map[string]declaredOperation `json:"paths"`
		Components struct {
			Schemas struct {
				ApiTokenCapability struct {
					Values []string `json:"x-extensible-enum"`
				} `json:"ApiTokenCapability"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	operations := map[string]declaredOperation{}
	for path, methods := range doc.Paths {
		for method, operation := range methods {
			operations[strings.ToUpper(method)+" "+path] = operation
		}
	}
	grantable := map[string]bool{}
	for _, id := range doc.Components.Schemas.ApiTokenCapability.Values {
		grantable[id] = true
	}
	return operations, grantable
}

// capabilitySentence is the sentence a resource's description names its token with: an
// organization token when an operation it reads, updates or destroys with is organization-level;
// otherwise a token on the workspace, and an organization token for the organization-level create
// or conditional calls when it has any.
func capabilitySentence(ids, workspace, organizationOnly []string, organization bool) string {
	named := func(ids []string) string {
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = "`" + id + "`"
		}
		if len(quoted) == 1 {
			return quoted[0]
		}
		return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
	}
	if organization {
		return fmt.Sprintf("Needs an organization token with %s.", named(ids))
	}
	if len(organizationOnly) == 0 {
		return fmt.Sprintf("Needs a token with %s on the workspace.", named(ids))
	}
	return fmt.Sprintf("Needs a token with %s on the workspace, and an organization token with %s for what the "+
		"organization decides.", named(workspace), named(organizationOnly))
}

var codeSpan = regexp.MustCompile("`([A-Za-z]+)`")

// tokenNeeds is what every resource declares its actions need of the API token.
type tokenNeeds interface {
	TokenNeeds() resources.Needs
}

func TestEveryResourceDeclaresWhatItsOperationsNeed(t *testing.T) {
	t.Parallel()
	operations, grantable := contract(t)

	for name, r := range resourceOperations {
		// organization is whether an operation the resource reads, updates or destroys with, or a
		// conditional call, is organization-level; organizationCreate whether its create is, and
		// workspaceLevel whether one of those others is on the workspace.
		organization, organizationCreate, workspaceLevel := false, false, false
		// conditionalOrganization is the capabilities the conditional calls need on the organization
		// only for part of a change.
		var conditionalOrganization []string
		// declared is the capabilities the contract declares for one action's operations, each once;
		// an organization capability needed only for part of a change counts as conditional only.
		declared := func(action string, keys []string) []string {
			var ids []string
			for _, key := range keys {
				operation, ok := operations[key]
				if !ok {
					t.Errorf("%s: %s sends %s, which the contract does not document", name, action, key)
					continue
				}
				switch {
				case operation.Scope == "organization" && action == "create":
					organizationCreate = true
				case operation.Scope == "organization":
					organization = true
				case operation.Scope == "workspace" && action != "create":
					workspaceLevel = true
				}
				partial := operation.conditionalOrganization()
				if partial != "" && action != "conditional" && !slices.Contains(r.conditional, key) {
					t.Errorf("%s: %s sends %s, whose %s is needed on the organization for part of a change; list it under conditional",
						name, action, key, partial)
				}
				if partial != "" && action == "conditional" && !slices.Contains(conditionalOrganization, partial) {
					conditionalOrganization = append(conditionalOrganization, partial)
				}
				for _, id := range operation.Capabilities {
					if (id != partial || action == "conditional") && !slices.Contains(ids, id) {
						ids = append(ids, id)
					}
				}
			}
			return ids
		}
		want := map[string][]string{
			"create": declared("create", r.create), "read": declared("read", r.read),
			"update": declared("update", r.update), "delete": declared("delete", r.delete),
			"conditional": declared("conditional", r.conditional),
		}
		// A resource only its create makes organization-level is wholly so unless a workspace token
		// may take one of its other actions.
		organizationCreate = organizationCreate && !organization
		organization = organization || (organizationCreate && !workspaceLevel)
		organizationCreate = organizationCreate && workspaceLevel

		resource, ok := r.new().(tokenNeeds)
		if !ok {
			t.Errorf("%s declares no TokenNeeds", name)
			continue
		}
		needs := resource.TokenNeeds()
		got := map[string][]string{
			"create": ids(needs.Create), "read": ids(needs.Read), "update": ids(needs.Update), "delete": ids(needs.Delete),
			"conditional": ids(conditionalCapabilities(needs.Conditional)),
		}
		for action := range want {
			if !slices.Equal(got[action], want[action]) {
				t.Errorf("%s: %s needs %v, its operations declare %v", name, action, got[action], want[action])
			}
		}
		if needs.Organization != organization {
			t.Errorf("%s: Organization is %v, the contract's scopes say %v", name, needs.Organization, organization)
		}
		if needs.OrganizationCreate != organizationCreate {
			t.Errorf("%s: OrganizationCreate is %v, the contract's scopes say %v", name, needs.OrganizationCreate, organizationCreate)
		}
		var gotConditionalOrganization []string
		for _, c := range needs.Conditional {
			if c.Organization && !slices.Contains(gotConditionalOrganization, string(c.Capability)) {
				gotConditionalOrganization = append(gotConditionalOrganization, string(c.Capability))
			}
		}
		if !slices.Equal(gotConditionalOrganization, conditionalOrganization) {
			t.Errorf("%s: conditional calls need %v on the organization, the contract says %v",
				name, gotConditionalOrganization, conditionalOrganization)
		}

		var all, workspace, organizationOnly []string
		add := func(list *[]string, ids ...string) {
			for _, id := range ids {
				if !slices.Contains(*list, id) {
					*list = append(*list, id)
				}
			}
		}
		add(&all, slices.Concat(want["create"], want["read"], want["update"], want["delete"], want["conditional"])...)
		if organizationCreate {
			add(&organizationOnly, want["create"]...)
		} else {
			add(&workspace, want["create"]...)
		}
		add(&workspace, slices.Concat(want["read"], want["update"], want["delete"])...)
		for _, id := range want["conditional"] {
			if slices.Contains(conditionalOrganization, id) {
				add(&organizationOnly, id)
			} else {
				add(&workspace, id)
			}
		}
		if len(all) == 0 {
			t.Errorf("%s: its operations declare no capability", name)
			continue
		}

		resp := &fwresource.SchemaResponse{}
		r.new().Schema(context.Background(), fwresource.SchemaRequest{}, resp)
		description := resp.Schema.Description

		if want := capabilitySentence(all, workspace, organizationOnly, organization); !strings.Contains(description, want) {
			t.Errorf("%s: description %q does not say %q", name, description, want)
		}
		for _, match := range codeSpan.FindAllStringSubmatch(description, -1) {
			if grantable[match[1]] && !slices.Contains(all, match[1]) {
				t.Errorf("%s: description names %s, which none of its operations needs", name, match[1])
			}
		}
	}
}

func conditionalCapabilities(conditional []resources.Conditional) []costfluent.Capability {
	capabilities := make([]costfluent.Capability, len(conditional))
	for i, c := range conditional {
		capabilities[i] = c.Capability
	}
	return capabilities
}

func ids(capabilities []costfluent.Capability) []string {
	var out []string
	for _, c := range capabilities {
		out = append(out, string(c))
	}
	return out
}

func TestEveryRegisteredResourceHasItsOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := provider.New("test")()
	for _, newResource := range p.Resources(ctx) {
		resp := &fwresource.MetadataResponse{}
		newResource().Metadata(ctx, fwresource.MetadataRequest{ProviderTypeName: "costfluent"}, resp)
		if _, ok := resourceOperations[resp.TypeName]; !ok {
			t.Errorf("%s lists no operations in resourceOperations, so nothing holds its description to the contract", resp.TypeName)
		}
	}
}

// The update of a workspace provider asserts removeWorkspaceProvider to turn the link off and
// addWorkspaceProvider for any other change, so the description says which, not only that the
// resource needs both.
func TestWorkspaceProviderDescriptionSaysWhichChangeNeedsWhichCapability(t *testing.T) {
	t.Parallel()
	resp := &fwresource.SchemaResponse{}
	resources.NewWorkspaceProviderResource().Schema(context.Background(), fwresource.SchemaRequest{}, resp)

	want := "Creating the link and any change but turning it off need `addWorkspaceProvider`; turning it off, " +
		"creating it turned off included, and destroying it need `removeWorkspaceProvider`."
	if !strings.Contains(resp.Schema.Description, want) {
		t.Errorf("description %q does not say %q", resp.Schema.Description, want)
	}
}
