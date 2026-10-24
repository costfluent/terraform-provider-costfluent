package resources_test

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/fakeapi"
	"github.com/costfluent/terraform-provider-costfluent/internal/resources"
	"github.com/costfluent/terraform-provider-costfluent/internal/session"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const otherWorkspace = "wsp_0000000000000000000002"

// workspaceDefaulted is a workspace-scoped resource whose workspace_id may be left to the
// provider's default workspace.
type workspaceDefaulted struct {
	address string
	fake    func(*fakeapi.Server)
	config  func(*fakeapi.Server) string
	// edited is config with one attribute changed that updates in place.
	edited func(*fakeapi.Server) string
	ids    func(*fakeapi.Server) []string
	// importID turns the stored object's ID into an import ID in the provider's workspace.
	importID func(id string) string
	new      func() fwresource.Resource
	// edit is the attribute edited changes, and its value there.
	edit  string
	value any
}

var explicitWorkspaceLine = regexp.MustCompile(`(?m)^\s*workspace_id\s*=.*\n`)

// withoutWorkspace drops workspace_id from a resource block and makes workspace the provider's
// default instead.
func withoutWorkspace(api *fakeapi.Server, config, workspace string) string {
	config = explicitWorkspaceLine.ReplaceAllString(config, "")
	return regexp.MustCompile(`(?s)^provider "costfluent" \{.*?\n\}\n`).
		ReplaceAllLiteralString(config, api.ProviderBlock(fmt.Sprintf("workspace = %q", workspace)))
}

// withDefault keeps the configured workspace_id and makes workspace the provider's default.
func withDefault(api *fakeapi.Server, config, workspace string) string {
	return regexp.MustCompile(`(?s)^provider "costfluent" \{.*?\n\}\n`).
		ReplaceAllLiteralString(config, api.ProviderBlock(fmt.Sprintf("workspace = %q", workspace)))
}

func workspaceDefaultedResources() []workspaceDefaulted {
	bare := func(id string) string { return id }
	collection := func(path string) func(*fakeapi.Server) []string {
		return func(api *fakeapi.Server) []string { return api.IDs(path) }
	}
	return []workspaceDefaulted{
		{
			address: "costfluent_cost_alert.a", fake: fakeCostAlerts,
			config: func(api *fakeapi.Server) string {
				return costAlertConfig(api, "EC2 spike", 25, "  comparison_period = \"previousWeek\"\n")
			},
			ids: collection("/v1/cost-alerts"), importID: bare,
			edited: func(api *fakeapi.Server) string {
				return costAlertConfig(api, "EC2 surge", 25, "  comparison_period = \"previousWeek\"\n")
			},
			new: resources.NewCostAlertResource, edit: "name", value: "EC2 surge",
		},
		{
			address: "costfluent_saved_filter.f", fake: savedFilterFake,
			config: func(api *fakeapi.Server) string { return savedFilterConfig(api, "EC2", "") },
			ids:    collection("/v1/saved-filters"), importID: bare,
			edited: func(api *fakeapi.Server) string { return savedFilterConfig(api, "EC2 renamed", "") },
			new:    resources.NewSavedFilterResource, edit: "title", value: "EC2 renamed",
		},
		{
			address: "costfluent_virtual_tag.t", fake: virtualTagFake,
			config: func(api *fakeapi.Server) string { return virtualTagConfig(api, "") },
			ids:    collection("/v1/virtual-tags"), importID: bare,
			edited: func(api *fakeapi.Server) string {
				return virtualTagConfig(api, "  description = \"Owning team\"")
			},
			new: resources.NewVirtualTagResource, edit: "description", value: "Owning team",
		},
		{
			address: "costfluent_cost_report_subscription.s", fake: subscriptionFake,
			config: func(api *fakeapi.Server) string { return subscriptionConfig(api, "") },
			ids:    collection("/v1/cost-report-subscriptions"), importID: bare,
			edited: func(api *fakeapi.Server) string { return subscriptionConfig(api, "  delivery_hour = 7\n") },
			new:    resources.NewCostReportSubscriptionResource, edit: "delivery_hour", value: int64(7),
		},
		{
			address: "costfluent_folder.f", fake: fakeFolders,
			config: func(api *fakeapi.Server) string { return folderConfig(api, "Finance") },
			ids:    collection("/v1/folders"), importID: bare,
			edited: func(api *fakeapi.Server) string { return folderConfig(api, "Finance renamed") },
			new:    resources.NewFolderResource, edit: "title", value: "Finance renamed",
		},
		{
			address: "costfluent_dashboard.d", fake: dashboardFake,
			config: func(api *fakeapi.Server) string { return dashboardConfig(api, "Overview", "") },
			ids:    collection("/v1/dashboards"), importID: bare,
			edited: func(api *fakeapi.Server) string { return dashboardConfig(api, "Overview renamed", "") },
			new:    resources.NewDashboardResource, edit: "title", value: "Overview renamed",
		},
		{
			address: "costfluent_business_metric.m", fake: businessMetricFake,
			config: func(api *fakeapi.Server) string { return businessMetricConfig(api, "Customers", "") },
			ids:    collection("/v1/business-metrics"), importID: bare,
			edited: func(api *fakeapi.Server) string { return businessMetricConfig(api, "Customers renamed", "") },
			new:    resources.NewBusinessMetricResource, edit: "name", value: "Customers renamed",
		},
		{
			address: widgetAddress, fake: widgetFake,
			config: func(api *fakeapi.Server) string {
				return widgetConfig(api, reportWidget(testReport, "Spend", ""))
			},
			edited: func(api *fakeapi.Server) string {
				return widgetConfig(api, reportWidget(testReport, "Spend renamed", ""))
			},
			ids:      collection(widgetsOf(testDashboard)),
			importID: func(id string) string { return testDashboard + ":" + id },
			new:      resources.NewDashboardWidgetResource, edit: "title", value: "Spend renamed",
		},
	}
}

func onlyID(api *fakeapi.Server, r workspaceDefaulted) (string, error) {
	ids := r.ids(api)
	if len(ids) != 1 {
		return "", fmt.Errorf("%s objects = %v, want one", r.address, ids)
	}
	return ids[0], nil
}

// A resource created in the provider's default workspace records that workspace, so a later run
// whose default is another workspace still reads, keeps and deletes the original object instead of
// losing it from state and creating a duplicate.
func TestWorkspaceDefaultedResourcesKeepTheirWorkspace(t *testing.T) {
	t.Parallel()
	for _, r := range workspaceDefaultedResources() {
		t.Run(r.address, func(t *testing.T) {
			api := fakeapi.New(t)
			r.fake(api)
			var created string

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: withoutWorkspace(api, r.config(api), testWorkspace),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r.address, "workspace_id", testWorkspace),
							func(*terraform.State) (err error) {
								created, err = onlyID(api, r)
								return err
							},
						),
					},
					{
						Config:   withoutWorkspace(api, r.config(api), otherWorkspace),
						PlanOnly: true,
					},
					{
						Config: withoutWorkspace(api, r.config(api), otherWorkspace),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r.address, "workspace_id", testWorkspace),
							func(*terraform.State) error {
								id, err := onlyID(api, r)
								if err == nil && id != created {
									err = fmt.Errorf("%s = %s, want the original %s", r.address, id, created)
								}
								return err
							},
						),
					},
					{
						Config: withoutWorkspace(api, r.edited(api), otherWorkspace),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction(r.address, plancheck.ResourceActionUpdate),
							},
						},
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(r.address, "workspace_id", testWorkspace),
							resource.TestCheckResourceAttrWith(r.address, "id", func(id string) error {
								if id != created {
									return fmt.Errorf("%s = %s, want the original %s", r.address, id, created)
								}
								return nil
							}),
						),
					},
				},
				CheckDestroy: func(*terraform.State) error {
					if ids := r.ids(api); len(ids) != 0 {
						return fmt.Errorf("%s left %v after destroy under another default workspace", r.address, ids)
					}
					return nil
				},
			})
		})
	}
}

// An object imported by a bare ID, or held in state from a provider that kept workspace_id null,
// has the provider's workspace filled in on read.
func TestWorkspaceDefaultedResourcesBackfillWorkspaceOnRead(t *testing.T) {
	t.Parallel()
	for _, r := range workspaceDefaultedResources() {
		t.Run(r.address, func(t *testing.T) {
			api := fakeapi.New(t)
			r.fake(api)
			config := withoutWorkspace(api, r.config(api), testWorkspace)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: config},
					{
						Config:       config,
						ResourceName: r.address,
						ImportState:  true,
						ImportStateIdFunc: func(*terraform.State) (string, error) {
							id, err := onlyID(api, r)
							return r.importID(id), err
						},
						ImportStateCheck: func(states []*terraform.InstanceState) error {
							if len(states) != 1 {
								return fmt.Errorf("imported %d states, want one", len(states))
							}
							if got := states[0].Attributes["workspace_id"]; got != testWorkspace {
								return fmt.Errorf("imported workspace_id = %q, want %q", got, testWorkspace)
							}
							return nil
						},
					},
				},
			})
		})
	}
}

// A configured workspace_id wins over the provider's default and stays as configured.
func TestWorkspaceDefaultedResourcesKeepAnExplicitWorkspace(t *testing.T) {
	t.Parallel()
	for _, r := range workspaceDefaultedResources() {
		t.Run(r.address, func(t *testing.T) {
			api := fakeapi.New(t)
			r.fake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{
						Config: withDefault(api, r.config(api), otherWorkspace),
						Check:  resource.TestCheckResourceAttr(r.address, "workspace_id", testWorkspace),
					},
					{Config: withDefault(api, r.config(api), otherWorkspace), PlanOnly: true},
				},
			})
		})
	}
}

// State written before workspace_id was recorded holds it null. Applied with -refresh=false, Read
// never fills it in and the plan keeps it null, so an update must leave it null: a value the plan
// did not promise fails the apply as an inconsistent result.
func TestWorkspaceDefaultedResourcesUpdateKeepsANullWorkspace(t *testing.T) {
	t.Parallel()
	for _, r := range workspaceDefaultedResources() {
		t.Run(r.address, func(t *testing.T) {
			api := fakeapi.New(t)
			r.fake(api)

			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: factories,
				Steps: []resource.TestStep{
					{Config: withoutWorkspace(api, r.config(api), testWorkspace)},
					{
						// The update makes the edit this config asks for, so refreshing finds nothing to apply.
						PreConfig: func() {
							id, err := onlyID(api, r)
							if err == nil {
								var got types.String
								if got, err = updateWithNullWorkspace(context.Background(), api, r, r.importID(id)); err == nil && !got.IsNull() {
									err = fmt.Errorf("workspace_id after update = %s, want null as planned", got)
								}
							}
							if err != nil {
								t.Error(err)
							}
						},
						Config: withoutWorkspace(api, r.edited(api), testWorkspace),
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
						},
					},
				},
			})
		})
	}
}

// updateWithNullWorkspace reads the object imported by importID into state, nulls its
// workspace_id, and updates it with r.edit set to r.value, as an apply without refresh does with
// state from before workspace_id was recorded. It returns the workspace_id the update stores.
func updateWithNullWorkspace(ctx context.Context, api *fakeapi.Server, r workspaceDefaulted, importID string) (types.String, error) {
	res := r.new()
	client := costfluent.NewClient(costfluent.WithBaseURL(api.URL), costfluent.WithAPIKey("cf_org_test"),
		costfluent.WithWorkspace(testWorkspace))
	var configured fwresource.ConfigureResponse
	res.(fwresource.ResourceWithConfigure).Configure(ctx, fwresource.ConfigureRequest{ProviderData: &session.Session{Client: client}}, &configured)
	var schema fwresource.SchemaResponse
	res.Schema(ctx, fwresource.SchemaRequest{}, &schema)
	diags := append(configured.Diagnostics, schema.Diagnostics...)

	imported := fwresource.ImportStateResponse{State: tfsdk.State{
		Schema: schema.Schema, Raw: tftypes.NewValue(schema.Schema.Type().TerraformType(ctx), nil),
	}}
	res.(fwresource.ResourceWithImportState).ImportState(ctx, fwresource.ImportStateRequest{ID: importID}, &imported)
	read := fwresource.ReadResponse{State: imported.State}
	res.Read(ctx, fwresource.ReadRequest{State: imported.State}, &read)
	diags = append(diags, imported.Diagnostics...)
	diags = append(diags, read.Diagnostics...)
	if diags.HasError() {
		return types.String{}, fmt.Errorf("reading the object: %v", diags)
	}

	state := read.State
	diags = append(diags, state.SetAttribute(ctx, path.Root("workspace_id"), types.StringNull())...)
	plan := tfsdk.Plan{Schema: schema.Schema, Raw: state.Raw.Copy()}
	diags = append(diags, plan.SetAttribute(ctx, path.Root(r.edit), r.value)...)
	updated := fwresource.UpdateResponse{State: tfsdk.State{Schema: schema.Schema, Raw: plan.Raw.Copy()}}
	res.Update(ctx, fwresource.UpdateRequest{
		Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw.Copy()}, Plan: plan, State: state,
	}, &updated)
	diags = append(diags, updated.Diagnostics...)
	if diags.HasError() {
		return types.String{}, fmt.Errorf("updating the object: %v", diags)
	}
	var workspace types.String
	diags = append(diags, updated.State.GetAttribute(ctx, path.Root("workspace_id"), &workspace)...)
	if diags.HasError() {
		return types.String{}, fmt.Errorf("reading workspace_id: %v", diags)
	}
	return workspace, nil
}
