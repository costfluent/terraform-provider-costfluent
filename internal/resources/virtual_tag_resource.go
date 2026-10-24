package resources

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &VirtualTagResource{}
	_ resource.ResourceWithConfigure   = &VirtualTagResource{}
	_ resource.ResourceWithModifyPlan  = &VirtualTagResource{}
	_ resource.ResourceWithImportState = &VirtualTagResource{}
)

type VirtualTagResource struct {
	managed
}

type VirtualTagResourceModel struct {
	ID               types.String          `tfsdk:"id"`
	WorkspaceID      types.String          `tfsdk:"workspace_id"`
	Key              types.String          `tfsdk:"key"`
	Description      types.String          `tfsdk:"description"`
	DefaultValue     types.String          `tfsdk:"default_value"`
	CollapsedTagKeys types.List            `tfsdk:"collapsed_tag_keys"`
	Rules            []VirtualTagRuleModel `tfsdk:"rule"`
	CreatedAt        types.String          `tfsdk:"created_at"`
	UpdatedAt        types.String          `tfsdk:"updated_at"`
}

type VirtualTagRuleModel struct {
	Value  types.String `tfsdk:"value"`
	Filter types.String `tfsdk:"filter"`
}

func NewVirtualTagResource() resource.Resource {
	return &VirtualTagResource{managed{name: "virtual_tag", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageTags, costfluent.CapabilityViewTags},
		Read:   []costfluent.Capability{costfluent.CapabilityViewTags},
		Update: []costfluent.Capability{costfluent.CapabilityManageTags, costfluent.CapabilityViewTags},
		Delete: []costfluent.Capability{costfluent.CapabilityManageTags},
		Conditional: []Conditional{{
			Capability: costfluent.CapabilityViewCosts,
			Sent:       sentForCQL(path.MatchRoot("rule").AtAnyListIndex().AtName("filter")),
		}},
	}}}
}

func (r *VirtualTagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent virtual tag: a tag key whose value per cost row is the first collapsed " +
			"provider tag key the row carries, then the first rule whose filter matches, then the default. " +
			"It is evaluated when cost is read, so an edit applies to the next report. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Virtual tag ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Workspace ID. When omitted, the provider's default workspace at creation " +
					"is recorded and kept, so a later change to the provider's default does not move it.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "The key reports group and filter by. Unique in the workspace, case-sensitively.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 128),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Virtual tag description.",
			},
			"default_value": schema.StringAttribute{
				Optional:    true,
				Description: "Value for rows no collapsed key or rule claims. Unset, such rows have an empty value.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 256),
				},
			},
			"collapsed_tag_keys": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Provider tag keys read in order before any rule; the first a row carries gives its " +
					"value, so team and Team can be one tag. At most 10.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(10),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp.",
			},
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				Description: "Rules in evaluation order; the first whose filter matches assigns its value. At most 50, with at most 50 filter clauses across them. " +
					"A virtual tag needs at least one rule or one collapsed tag key.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(50),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"value": schema.StringAttribute{
							Required:    true,
							Description: "The value a matching row gets.",
							Validators: []validator.String{
								stringvalidator.LengthBetween(1, 256),
							},
						},
						"filter": schema.StringAttribute{
							Required: true,
							Description: "The cost filter as its JSON document (build it with jsonencode) or as CQL text, " +
								"as costfluent_cost_report takes it. At least one clause, none on a virtual tag, and no OR: " +
								"write each alternative as its own rule.",
						},
					},
				},
			},
		},
	}
}

func (r *VirtualTagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	collapsed, diags := collapsedTagKeysOf(ctx, plan.CollapsedTagKeys)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := r.client.CreateVirtualTag(ctx, &costfluent.CreateVirtualTagInput{
		WorkspaceID:      plan.WorkspaceID.ValueString(),
		Key:              plan.Key.ValueString(),
		Description:      knownString(plan.Description),
		DefaultValue:     knownString(plan.DefaultValue),
		CollapsedTagKeys: collapsed,
		Rules:            virtualTagRulesOf(plan.Rules),
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create virtual tag", err, plan.WorkspaceID)
		return
	}

	resp.Diagnostics.Append(mapVirtualTagToModel(ctx, r.client, tag, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, tag.ETag)...)
}

func (r *VirtualTagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	tag, err := r.client.GetVirtualTag(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read virtual tag", err)
		return
	}

	resp.Diagnostics.Append(mapVirtualTagToModel(ctx, r.client, tag, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, tag.ETag)...)
}

// Update sends the whole definition: the API keeps an omitted field, so an attribute removed from
// the configuration is sent empty, which clears it.
func (r *VirtualTagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state VirtualTagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	collapsed, diags := collapsedTagKeysOf(ctx, plan.CollapsedTagKeys)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	rules := virtualTagRulesOf(plan.Rules)

	tag, err := r.client.UpdateVirtualTag(ctx, state.ID.ValueString(), &costfluent.UpdateVirtualTagInput{
		WorkspaceID:       state.WorkspaceID.ValueString(),
		Key:               plan.Key.ValueStringPointer(),
		Description:       knownString(plan.Description),
		ClearDescription:  plan.Description.IsNull(),
		DefaultValue:      knownString(plan.DefaultValue),
		ClearDefaultValue: plan.DefaultValue.IsNull(),
		CollapsedTagKeys:  &collapsed,
		Rules:             &rules,
	}, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Virtual tag", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update virtual tag", err)
		return
	}

	resp.Diagnostics.Append(mapVirtualTagToModel(ctx, r.client, tag, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, tag.ETag)...)
}

func (r *VirtualTagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VirtualTagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteVirtualTag(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Virtual tag", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete virtual tag", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *VirtualTagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

func collapsedTagKeysOf(ctx context.Context, list types.List) ([]string, diag.Diagnostics) {
	keys := []string{}
	if list.IsNull() || list.IsUnknown() {
		return keys, nil
	}
	diags := list.ElementsAs(ctx, &keys, false)
	return keys, diags
}

func virtualTagRulesOf(rules []VirtualTagRuleModel) []costfluent.VirtualTagRule {
	out := make([]costfluent.VirtualTagRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, costfluent.VirtualTagRule{Value: rule.Value.ValueString(), Filter: rule.Filter.ValueString()})
	}
	return out
}

// mapVirtualTagToModel writes the stored tag into the model. A filter the API returns in a
// different form that means the same keeps the configured string, and an unset collapsed_tag_keys
// stays unset while the API holds none, so neither plans a change that is not one.
func mapVirtualTagToModel(
	ctx context.Context, client *costfluent.Client, t *costfluent.VirtualTag, model *VirtualTagResourceModel,
) diag.Diagnostics {
	model.ID = types.StringValue(t.ID)
	model.Key = types.StringValue(t.Key)
	model.Description = types.StringPointerValue(t.Description)
	model.DefaultValue = types.StringPointerValue(t.DefaultValue)
	model.CreatedAt = types.StringValue(t.CreatedAt.Format(time.RFC3339))
	model.UpdatedAt = optionalTime(t.UpdatedAt)

	var diags diag.Diagnostics
	if len(t.CollapsedTagKeys) == 0 && model.CollapsedTagKeys.IsNull() {
		model.CollapsedTagKeys = types.ListNull(types.StringType)
	} else {
		var listDiags diag.Diagnostics
		model.CollapsedTagKeys, listDiags = types.ListValueFrom(ctx, types.StringType, t.CollapsedTagKeys)
		diags.Append(listDiags...)
	}

	rules := make([]VirtualTagRuleModel, 0, len(t.Rules))
	for i, rule := range t.Rules {
		configured := types.StringNull()
		if i < len(model.Rules) {
			configured = model.Rules[i].Filter
		}
		filter, err := keepConfiguredFilter(ctx, client, configured, &rule.Filter)
		if err != nil {
			apidiag.AddError(&diags, "Failed to compare a virtual tag rule filter", err)
			return diags
		}
		rules = append(rules, VirtualTagRuleModel{Value: types.StringValue(rule.Value), Filter: filter})
	}
	model.Rules = rules

	return diags
}

// keepConfiguredFilter returns the configured filter while it means the stored one, and the stored
// one otherwise. The API stores and returns every filter as its JSON document, so without this a
// filter written as CQL, or as JSON in another key order, would plan a change on every refresh and
// fail apply as an inconsistent result. JSON is compared as JSON; CQL is translated by the API,
// which owns the grammar, and its JSON compared. With nothing configured, as after an import, the
// stored value is taken.
func keepConfiguredFilter(
	ctx context.Context, client *costfluent.Client, configured types.String, stored *string,
) (types.String, error) {
	if stored == nil {
		return types.StringNull(), nil
	}
	if configured.IsNull() || configured.IsUnknown() {
		return types.StringValue(*stored), nil
	}

	text := configured.ValueString()
	if !isCQL(text) {
		if sameJSON(text, *stored) {
			return configured, nil
		}
		return types.StringValue(*stored), nil
	}

	translated, err := client.TranslateCostFilter(ctx, text)
	if err != nil {
		return types.StringNull(), err
	}
	if sameJSON(translated.Filter, *stored) {
		return configured, nil
	}
	return types.StringValue(*stored), nil
}

// isCQL reports whether a filter is written as CQL, which the API translates, rather than as its
// JSON document.
func isCQL(filter string) bool {
	trimmed := strings.TrimSpace(filter)
	return trimmed != "" && trimmed[0] != '[' && trimmed[0] != '{'
}

// sameJSON reports whether two documents are equal as JSON, so a stored filter that differs from
// the configuration only in whitespace or key order does not plan a change.
func sameJSON(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	return reflect.DeepEqual(x, y)
}
