package resources

import (
	"context"

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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &TagPolicyResource{}
	_ resource.ResourceWithConfigure   = &TagPolicyResource{}
	_ resource.ResourceWithModifyPlan  = &TagPolicyResource{}
	_ resource.ResourceWithImportState = &TagPolicyResource{}
)

// The API's limits on a tag policy (TagPolicyRule and TagPolicy in the domain).
const (
	tagPolicyMaxRules           = 20
	tagPolicyMaxKeys            = 20
	tagPolicyMaxKeyLength       = 128
	tagPolicyMaxAccounts        = 50
	tagPolicyMaxAccountIDLength = 256
)

type TagPolicyResource struct {
	managed
}

type TagPolicyResourceModel struct {
	ID          types.String         `tfsdk:"id"`
	WorkspaceID types.String         `tfsdk:"workspace_id"`
	Rules       []TagPolicyRuleModel `tfsdk:"rule"`
	ReportID    types.String         `tfsdk:"report_id"`
}

type TagPolicyRuleModel struct {
	Provider     types.String `tfsdk:"provider"`
	AccountIDs   types.List   `tfsdk:"account_ids"`
	RequiredKeys types.List   `tfsdk:"required_keys"`
}

func NewTagPolicyResource() resource.Resource {
	return &TagPolicyResource{managed{name: "tag_policy", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageTags},
		Read:   []costfluent.Capability{costfluent.CapabilityViewTags},
		Update: []costfluent.Capability{costfluent.CapabilityManageTags},
		Delete: []costfluent.Capability{costfluent.CapabilityManageTags},
	}}}
}

func (r *TagPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a workspace's tag policy: the tag keys its cost must carry. A workspace has one policy, " +
			"replaced whole on every apply, so manage it from one resource only; creating this resource replaces any " +
			"policy the workspace already has, and destroying it clears the rules. Costfluent measures cost against " +
			"the policy and changes nothing in the cloud. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The workspace ID: a workspace has one tag policy.",
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
			"report_id": schema.StringAttribute{
				Computed: true,
				Description: "The cost report listing cost that misses a required key, by account. Saving the first rules " +
					"creates it; it is kept when the rules are cleared, and reused when rules are saved again. Null " +
					"before the first rules, or once the report is deleted, until the next save creates another.",
				PlanModifiers: []planmodifier.String{
					keepSeededReport{},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"rule": schema.ListNestedBlock{
				Description: "Rules in order. Rules add up: a cost row must carry the required keys of every rule that " +
					"applies to it. At most 20; with none, the workspace has no policy.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(tagPolicyMaxRules),
				},
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"provider": schema.StringAttribute{
							Optional: true,
							Description: "The provider whose cost the rule applies to (e.g., aws, azure, gcp), as the " +
								"Public API spells it. Unset, the rule applies to every provider.",
							Validators: []validator.String{
								stringvalidator.OneOf(providerKeyValues()...),
							},
						},
						"account_ids": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Account IDs as the provider writes them; the rule applies to cost of these accounts. " +
								"Unset or empty, it applies to every account. At most 50, each named once.",
							Validators: []validator.List{
								listvalidator.SizeAtMost(tagPolicyMaxAccounts),
								listvalidator.UniqueValues(),
								listvalidator.ValueStringsAre(
									stringvalidator.LengthBetween(1, tagPolicyMaxAccountIDLength),
									stringvalidator.RegexMatches(notBlank, "must not be blank"),
								),
							},
						},
						"required_keys": schema.ListAttribute{
							Required:    true,
							ElementType: types.StringType,
							Description: "The tag keys a cost row the rule applies to must carry with a value, compared " +
								"case-sensitively. One to 20, each named once.",
							Validators: []validator.List{
								listvalidator.SizeBetween(1, tagPolicyMaxKeys),
								listvalidator.UniqueValues(),
								listvalidator.ValueStringsAre(
									stringvalidator.LengthBetween(1, tagPolicyMaxKeyLength),
									stringvalidator.RegexMatches(notBlank, "must not be blank"),
								),
							},
						},
					},
				},
			},
		},
	}
}

func (r *TagPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TagPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)
	// The SDK names the workspace in the path and falls back to no default, so a missing one is
	// refused here rather than sent as an empty path segment.
	if plan.WorkspaceID.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("workspace_id"), "No workspace for the tag policy",
			"Set workspace_id, or the provider's workspace, to the workspace whose tag policy this resource manages.")
		return
	}

	r.save(ctx, plan, &resp.Diagnostics, &resp.State, "Failed to create tag policy")
}

func (r *TagPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TagPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	policy, err := r.client.GetTagPolicy(ctx, state.WorkspaceID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read tag policy", err)
		return
	}

	resp.Diagnostics.Append(mapTagPolicyToModel(ctx, policy, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *TagPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TagPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.save(ctx, plan, &resp.Diagnostics, &resp.State, "Failed to update tag policy")
}

// Delete clears the rules: the policy is the workspace's and cannot be removed, only emptied. The
// report the first rules created stays, as the API keeps it.
func (r *TagPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TagPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.UpdateTagPolicy(ctx, state.WorkspaceID.ValueString(), &costfluent.UpdateTagPolicyInput{
		Rules: []costfluent.TagPolicyRule{},
	})
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to clear tag policy", err)
	}
}

// ImportState takes the workspace ID, which is the policy's ID.
func (r *TagPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// save replaces the workspace's rules with the planned ones and writes the stored policy to state.
func (r *TagPolicyResource) save(
	ctx context.Context, plan TagPolicyResourceModel, diags *diag.Diagnostics, state *tfsdk.State, summary string,
) {
	rules, ruleDiags := tagPolicyRulesOf(ctx, plan.Rules)
	diags.Append(ruleDiags...)
	if diags.HasError() {
		return
	}

	policy, err := r.client.UpdateTagPolicy(ctx, plan.WorkspaceID.ValueString(), &costfluent.UpdateTagPolicyInput{Rules: rules})
	if err != nil {
		apidiag.AddError(diags, summary, err)
		return
	}

	diags.Append(mapTagPolicyToModel(ctx, policy, &plan)...)
	diags.Append(state.Set(ctx, plan)...)
}

func tagPolicyRulesOf(ctx context.Context, rules []TagPolicyRuleModel) ([]costfluent.TagPolicyRule, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := make([]costfluent.TagPolicyRule, 0, len(rules))
	for _, rule := range rules {
		sent := costfluent.TagPolicyRule{AccountIDs: []string{}, RequiredKeys: []string{}}
		if !rule.Provider.IsNull() && !rule.Provider.IsUnknown() {
			key := costfluent.ProviderKey(rule.Provider.ValueString())
			sent.Provider = &key
		}
		if !rule.AccountIDs.IsNull() && !rule.AccountIDs.IsUnknown() {
			diags.Append(rule.AccountIDs.ElementsAs(ctx, &sent.AccountIDs, false)...)
		}
		if !rule.RequiredKeys.IsNull() && !rule.RequiredKeys.IsUnknown() {
			diags.Append(rule.RequiredKeys.ElementsAs(ctx, &sent.RequiredKeys, false)...)
		}
		out = append(out, sent)
	}
	return out, diags
}

// mapTagPolicyToModel writes the stored policy into the model. The API answers a rule without
// accounts with an empty list, so a rule whose account_ids is unset keeps it unset while the API
// holds none; an import, which has no configuration to keep, reads it unset too.
func mapTagPolicyToModel(ctx context.Context, p *costfluent.TagPolicy, model *TagPolicyResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = model.WorkspaceID
	model.ReportID = types.StringPointerValue(p.ReportID)

	rules := make([]TagPolicyRuleModel, 0, len(p.Rules))
	for i, rule := range p.Rules {
		mapped := TagPolicyRuleModel{Provider: types.StringNull(), AccountIDs: types.ListNull(types.StringType)}
		if rule.Provider != nil {
			mapped.Provider = types.StringValue(string(*rule.Provider))
		}

		configuredAccounts := types.ListNull(types.StringType)
		if i < len(model.Rules) {
			configuredAccounts = model.Rules[i].AccountIDs
		}
		if len(rule.AccountIDs) > 0 || !configuredAccounts.IsNull() {
			var listDiags diag.Diagnostics
			mapped.AccountIDs, listDiags = types.ListValueFrom(ctx, types.StringType, rule.AccountIDs)
			diags.Append(listDiags...)
		}

		var listDiags diag.Diagnostics
		mapped.RequiredKeys, listDiags = types.ListValueFrom(ctx, types.StringType, rule.RequiredKeys)
		diags.Append(listDiags...)

		rules = append(rules, mapped)
	}
	model.Rules = rules
	return diags
}

// keepSeededReport plans the report an update leaves. The API keeps the report the first rules
// created and reuses it on every later save, so a known report_id stays as it is and anything
// referring to it plans no change. With none yet, as before the first rules or after the report was
// deleted, the save may create one, so the value is left unknown.
type keepSeededReport struct{}

func (keepSeededReport) Description(context.Context) string {
	return "Keeps the report the policy already has."
}

func (m keepSeededReport) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepSeededReport) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.StateValue.IsNull() || !req.PlanValue.IsUnknown() {
		// Creating, destroying, no report to keep, or nothing left to decide.
		return
	}
	resp.PlanValue = req.StateValue
}
