package resources

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                   = &SegmentResource{}
	_ resource.ResourceWithConfigure      = &SegmentResource{}
	_ resource.ResourceWithModifyPlan     = &SegmentResource{}
	_ resource.ResourceWithValidateConfig = &SegmentResource{}
	_ resource.ResourceWithImportState    = &SegmentResource{}
)

// SegmentResource manages one segment cost can be assigned to.
//
// This is the resource the segment map is built from, and the reason it is worth having in
// Terraform at all: a re-org becomes a diff rather than an afternoon of re-entering segments in a UI.
type SegmentResource struct {
	managed
}

type SegmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	TeamID      types.String `tfsdk:"team_id"`
	TeamLabel   types.String `tfsdk:"team_label"`
	TeamName    types.String `tfsdk:"team_name"`
	Product     types.String `tfsdk:"product"`
	CostCentre  types.String `tfsdk:"cost_centre"`
	IsShared    types.Bool   `tfsdk:"is_shared"`
	Label       types.String `tfsdk:"label"`
	Split       types.Object `tfsdk:"split"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

// SegmentSplitModel is how a shared segment's pool is spread.
type SegmentSplitModel struct {
	Method  types.String `tfsdk:"method"`
	Targets types.List   `tfsdk:"targets"`
}

// SegmentSplitTargetModel is one direct segment a split gives a share to.
type SegmentSplitTargetModel struct {
	SegmentID types.String  `tfsdk:"segment_id"`
	Percent   types.Float64 `tfsdk:"percent"`
}

var segmentSplitTargetAttrTypes = map[string]attr.Type{
	"segment_id": types.StringType,
	"percent":    types.Float64Type,
}

var segmentSplitAttrTypes = map[string]attr.Type{
	"method":  types.StringType,
	"targets": types.ListType{ElemType: types.ObjectType{AttrTypes: segmentSplitTargetAttrTypes}},
}

// segmentLabel matches a label the API stores as written: it trims surrounding spaces, so a label
// carrying them would read back different from the configuration.
var segmentLabel = regexp.MustCompile(`^\S(?s:.*\S)?$`)

// segmentSplitMaxTargets is the API's limit on the targets one split names.
const segmentSplitMaxTargets = 100

func NewSegmentResource() resource.Resource {
	return &SegmentResource{managed{name: "segment", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageCostAllocation},
		Read:   []costfluent.Capability{costfluent.CapabilityViewCostAllocation},
		Update: []costfluent.Capability{costfluent.CapabilityManageCostAllocation},
		Delete: []costfluent.Capability{costfluent.CapabilityManageCostAllocation},
	}}}
}

func (r *SegmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent segment: where an allocation rule assigns cost. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Segment token.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Workspace token the segment belongs to.",
				PlanModifiers: []planmodifier.String{
					// Segments do not move between workspaces. Rules point at them by id inside one
					// workspace, so a move would silently break every rule that names this segment.
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					validators.TokenPrefix("wsp_"),
				},
			},
			"team_id": schema.StringAttribute{
				Optional:    true,
				Description: "Team token when the segment is a real team. Set this or team_label, not both.",
				Validators: []validator.String{
					validators.TokenPrefix("team_"),
				},
			},
			"team_label": schema.StringAttribute{
				Optional:    true,
				Description: "Segment name when it is a label rather than a team row.",
			},
			"team_name": schema.StringAttribute{
				Computed:    true,
				Description: "The referenced team's current name, resolved on read.",
			},
			"product": schema.StringAttribute{
				Optional: true,
				Description: "Product the segment is responsible for. Required on a direct segment; the API " +
					"stores none for a shared one.",
			},
			"cost_centre": schema.StringAttribute{
				Optional: true,
				Description: "Cost centre the segment charges to. Required on a direct segment; the API " +
					"stores none for a shared one.",
			},
			"is_shared": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Create a shared-cost bucket instead of a named segment. A shared segment " +
					"names no team, product or cost centre; its cost is spread across direct segments as " +
					"split says.",
				PlanModifiers: []planmodifier.Bool{
					// A shared bucket and a real segment are different things, not two states of one
					// thing: flipping this would change what every rule pointing here means.
					boolRequiresReplace{},
				},
			},
			"label": schema.StringAttribute{
				Optional: true,
				Description: "A shared segment's label. A workspace holds one shared segment per label, " +
					"compared ignoring case, and one without. Only a shared segment takes one.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(200),
					stringvalidator.RegexMatches(segmentLabel,
						"must not be empty or start or end with a space; leave it unset for the unlabelled shared segment"),
				},
			},
			"split": schema.SingleNestedAttribute{
				Optional: true,
				Description: "How a shared segment's pool is spread across direct segments of its workspace. " +
					"Unset is proportional. Only a shared segment takes one. Allocation is evaluated at query " +
					"time, so changing the split restates every past month.",
				Attributes: map[string]schema.Attribute{
					"method": schema.StringAttribute{
						Required: true,
						Description: "proportional spreads the pool by each direct segment's positive direct cost " +
							"and names no targets; even gives two or more targets the same share; percent gives " +
							"each target its percent, the percents summing to exactly 100.",
						Validators: []validator.String{
							stringvalidator.OneOf(
								costfluent.SegmentSplitProportional, costfluent.SegmentSplitEven, costfluent.SegmentSplitPercent),
						},
					},
					"targets": schema.ListNestedAttribute{
						Optional: true,
						Description: "The distinct direct segments of the workspace an even or percent split names, " +
							"at most 100, in order. Per currency each share but the last is rounded to eight " +
							"decimal places and the last target takes the rest.",
						Validators: []validator.List{
							listvalidator.SizeAtMost(segmentSplitMaxTargets),
						},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"segment_id": schema.StringAttribute{
									Required:    true,
									Description: "Token of a direct segment in the same workspace.",
									Validators: []validator.String{
										validators.TokenPrefix("seg_"),
									},
								},
								"percent": schema.Float64Attribute{
									Optional: true,
									Description: "A percent split's share for this target: above 0 and at most 100, " +
										"with at most two decimals. Unset for an even split.",
								},
							},
						},
					},
				},
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "Last update timestamp.",
			},
		},
	}
}

// ValidateConfig refuses a direct segment without an owner, product or cost centre, or with a label
// or split, and a split the API would refuse. The API refuses a direct segment without an owner on
// create and keeps the owner an update names none of, so it would never match the configuration. A
// shared segment may leave product and cost centre unset, which is what lets an imported one, read
// back without them, plan nothing.
func (r *SegmentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config SegmentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() || config.IsShared.IsUnknown() {
		return
	}
	if config.IsShared.ValueBool() {
		validateSegmentSplit(ctx, config.Split, &resp.Diagnostics)
		return
	}
	if !config.Label.IsNull() && !config.Label.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("label"), "Direct segment takes no label",
			"Only a shared segment (is_shared = true) carries a label.")
	}
	if !config.Split.IsNull() && !config.Split.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("split"), "Direct segment takes no split",
			"Only a shared segment (is_shared = true) has a pool to split.")
	}
	if config.Product.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("product"), "Segment has no product",
			"A direct segment names the product it is responsible for: set product.")
	}
	if config.CostCentre.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("cost_centre"), "Segment has no cost centre",
			"A direct segment names the cost centre it charges to: set cost_centre.")
	}
	if config.TeamID.IsUnknown() || config.TeamLabel.IsUnknown() {
		return
	}
	if config.TeamID.IsNull() && config.TeamLabel.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("team_label"), "Segment has no owner",
			"A direct segment names who owns it: set team_id or team_label.")
	}
}

// validateSegmentSplit refuses at plan the splits the API refuses on apply: proportional names no
// targets, even names at least two and no percents, and percent gives every target a percent above
// 0 and at most 100 with at most two decimals, summing to exactly 100. Targets are distinct.
func validateSegmentSplit(ctx context.Context, value types.Object, diags *diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return
	}
	var split SegmentSplitModel
	diags.Append(value.As(ctx, &split, basetypes.ObjectAsOptions{})...)
	if diags.HasError() || split.Method.IsUnknown() || split.Targets.IsUnknown() {
		return
	}
	var targets []SegmentSplitTargetModel
	if !split.Targets.IsNull() {
		diags.Append(split.Targets.ElementsAs(ctx, &targets, false)...)
		if diags.HasError() {
			return
		}
	}

	at := path.Root("split").AtName("targets")
	method := split.Method.ValueString()
	switch method {
	case costfluent.SegmentSplitProportional:
		if len(targets) > 0 {
			diags.AddAttributeError(at, "Proportional split names no targets",
				"A proportional split spreads across every direct segment by its direct cost; remove targets.")
		}
		return
	case costfluent.SegmentSplitEven:
		if len(targets) < 2 {
			diags.AddAttributeError(at, "Even split needs two targets", "An even split names at least two targets.")
		}
	case costfluent.SegmentSplitPercent:
		if len(targets) == 0 {
			diags.AddAttributeError(at, "Percent split needs targets",
				"A percent split names its targets, each with a percent, summing to exactly 100.")
		}
	default:
		return
	}

	seen := map[string]bool{}
	cents, percentsKnown := int64(0), true
	for i, target := range targets {
		if id := target.SegmentID; !id.IsNull() && !id.IsUnknown() {
			if seen[id.ValueString()] {
				diags.AddAttributeError(at.AtListIndex(i).AtName("segment_id"), "Split names a target twice",
					fmt.Sprintf("%s is already a target of this split; a split names each target once.", id.ValueString()))
			}
			seen[id.ValueString()] = true
		}
		percent := target.Percent
		if percent.IsUnknown() {
			percentsKnown = false
			continue
		}
		where := at.AtListIndex(i).AtName("percent")
		if method == costfluent.SegmentSplitEven {
			if !percent.IsNull() {
				diags.AddAttributeError(where, "Even split takes no percent", "An even split gives every target the same share.")
			}
			continue
		}
		if percent.IsNull() {
			diags.AddAttributeError(where, "Percent split target needs a percent",
				"Each target of a percent split takes a percent above 0 and at most 100.")
			percentsKnown = false
			continue
		}
		value := percent.ValueFloat64()
		hundredths := math.Round(value * 100)
		if value <= 0 || value > 100 || hundredths/100 != value {
			diags.AddAttributeError(where, "Invalid split percent",
				fmt.Sprintf("%v is not a percent above 0 and at most 100 with at most two decimals.", value))
			percentsKnown = false
			continue
		}
		cents += int64(hundredths)
	}
	// Summed in hundredths: summed as float64, 0.01 + 64.04 + 35.95 is 100.00000000000001.
	if method == costfluent.SegmentSplitPercent && percentsKnown && len(targets) > 0 && cents != 100_00 {
		diags.AddAttributeError(at, "Split percents do not sum to 100",
			fmt.Sprintf("A percent split's percents sum to exactly 100; these sum to %.2f.", float64(cents)/100))
	}
}

func (r *SegmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SegmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.CreateSegmentInput{
		WorkspaceID: plan.WorkspaceID.ValueString(),
		Product:     plan.Product.ValueString(),
		CostCentre:  plan.CostCentre.ValueString(),
		IsShared:    plan.IsShared.ValueBool(),
	}

	if !plan.TeamID.IsNull() {
		team := plan.TeamID.ValueString()
		input.TeamID = &team
	}
	if !plan.TeamLabel.IsNull() {
		label := plan.TeamLabel.ValueString()
		input.TeamLabel = &label
	}
	input.Label = plan.Label.ValueString()
	input.Split = segmentSplitInput(ctx, plan.Split, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	segment, err := r.client.CreateSegment(ctx, input)
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to create segment", err)
		return
	}

	mapSegmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, segment.ETag)...)
}

func (r *SegmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SegmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// There is no single-segment read on the contract, and adding one just for Terraform would be a
	// route with one caller. The listing is already scoped to the workspace, so finding the row in
	// it costs one request either way.
	segments, err := r.client.ListSegments(ctx, state.WorkspaceID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read segment", err)
		return
	}

	for i := range segments {
		if segments[i].ID == state.ID.ValueString() {
			mapSegmentToModel(&segments[i], &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			resp.Diagnostics.Append(keepETagWhileUnchanged(ctx, resp.Private, resp.State.Raw.Equal(req.State.Raw))...)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *SegmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SegmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.IsShared.ValueBool() {
		r.updateShared(ctx, plan, state, req, resp)
		return
	}

	input := &costfluent.UpdateSegmentInput{
		Product:    plan.Product.ValueString(),
		CostCentre: plan.CostCentre.ValueString(),
	}

	if !plan.TeamID.IsNull() {
		team := plan.TeamID.ValueString()
		input.TeamID = &team
	}
	if !plan.TeamLabel.IsNull() {
		label := plan.TeamLabel.ValueString()
		input.TeamLabel = &label
	}

	segment, err := r.client.UpdateSegment(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Segment", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update segment", err)
		return
	}

	mapSegmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, segment.ETag)...)
}

// updateShared sends a shared segment's label and split when either changed. The API stores no team
// label, product or cost centre for a shared segment and refuses an update naming one, so a change
// to them lives in state alone. Sending it would fail every apply, and after an import, which reads
// those attributes back as null, no configuration could avoid it. When nothing the API stores
// changed, the private state keeps its ETag because the stored row did not change.
func (r *SegmentResource) updateShared(
	ctx context.Context, plan, state SegmentResourceModel, req resource.UpdateRequest, resp *resource.UpdateResponse,
) {
	input := &costfluent.UpdateSegmentInput{}
	if !plan.Label.Equal(state.Label) {
		// An unset label is sent empty, which makes this the workspace's unlabelled shared segment.
		label := plan.Label.ValueString()
		input.Label = &label
	}
	if !plan.Split.Equal(state.Split) {
		input.Split = segmentSplitInput(ctx, plan.Split, &resp.Diagnostics)
		if input.Split == nil {
			// An unset split is proportional, and only a split the update names replaces the stored one.
			input.Split = &costfluent.SegmentSplit{Method: costfluent.SegmentSplitProportional}
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if input.Label == nil && input.Split == nil {
		plan.ID = state.ID
		plan.TeamName = state.TeamName
		plan.CreatedAt = state.CreatedAt
		plan.UpdatedAt = state.UpdatedAt
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		return
	}

	segment, err := r.client.UpdateSegment(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Segment", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update segment", err)
		return
	}

	mapSegmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, segment.ETag)...)
}

func (r *SegmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SegmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSegment(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Segment", true) {
		return
	}
	if err != nil {
		// The server refuses while a rule, a budget, a shared segment's split, or a cost report, cost
		// alert or saved filter still names this segment.
		// Surfacing that as-is is the useful failure: allocation is query-time, so a silent delete would restate every past
		// month rather than only affecting the future.
		apidiag.AddError(&resp.Diagnostics, "Failed to delete segment", err)
	}
}

// ImportState takes "<workspace token>:<segment token>".
//
// The workspace has to travel with the id because every read is workspace-scoped: a bare segment
// token is not addressable, which is the same tenancy rule the API enforces.
func (r *SegmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspace, id, ok := splitWorkspaceScopedID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import identifier",
			"Expected \"<workspace token>:<segment token>\", got: "+req.ID)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), workspace)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

func mapSegmentToModel(t *costfluent.Segment, model *SegmentResourceModel) {
	model.ID = types.StringValue(t.ID)
	model.TeamLabel = storedOrConfigured(t.TeamLabel, model.TeamLabel)
	model.Product = storedOrConfigured(t.Product, model.Product)
	model.CostCentre = storedOrConfigured(t.CostCentre, model.CostCentre)
	model.IsShared = types.BoolValue(t.IsShared)
	model.CreatedAt = types.StringValue(t.CreatedAt.Format(time.RFC3339))

	// An empty label is the unlabelled shared segment, which the configuration writes as no label.
	if t.Label != "" {
		model.Label = types.StringValue(t.Label)
	} else {
		model.Label = types.StringNull()
	}
	model.Split = segmentSplitObject(t.Split, model.Split)

	if t.TeamID != nil {
		model.TeamID = types.StringValue(*t.TeamID)
	} else {
		model.TeamID = types.StringNull()
	}
	if t.TeamName != nil {
		model.TeamName = types.StringValue(*t.TeamName)
	} else {
		model.TeamName = types.StringNull()
	}
	if t.UpdatedAt != nil {
		model.UpdatedAt = types.StringValue(t.UpdatedAt.Format(time.RFC3339))
	} else {
		model.UpdatedAt = types.StringNull()
	}
}

// storedOrConfigured reads a segment string the API answers with "" when it stores none: the label
// of a segment that names a team, and the label, product and cost centre of the shared bucket.
// Those keep what the configuration holds, null when it holds nothing.
func storedOrConfigured(stored string, configured types.String) types.String {
	if stored != "" {
		return types.StringValue(stored)
	}
	if configured.IsUnknown() {
		return types.StringNull()
	}
	return configured
}

// segmentSplitInput is the configured split as the API takes it; nil while it is unset or unknown,
// which a create reads as proportional.
func segmentSplitInput(ctx context.Context, value types.Object, diags *diag.Diagnostics) *costfluent.SegmentSplit {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var split SegmentSplitModel
	diags.Append(value.As(ctx, &split, basetypes.ObjectAsOptions{})...)
	var targets []SegmentSplitTargetModel
	if !split.Targets.IsNull() && !split.Targets.IsUnknown() {
		diags.Append(split.Targets.ElementsAs(ctx, &targets, false)...)
	}
	input := &costfluent.SegmentSplit{Method: split.Method.ValueString()}
	for _, target := range targets {
		input.Targets = append(input.Targets, costfluent.SegmentSplitTarget{
			SegmentID: target.SegmentID.ValueString(),
			Percent:   target.Percent.ValueFloat64Pointer(),
		})
	}
	return input
}

// segmentSplitObject is the API's split as the split attribute. An unset split is proportional, so a
// proportional split without targets reads as null while the prior value is null, as after an
// import, and an empty target list reads as the null or empty list the prior value holds.
func segmentSplitObject(split *costfluent.SegmentSplit, prior types.Object) types.Object {
	if split == nil {
		return types.ObjectNull(segmentSplitAttrTypes)
	}
	if split.Method == costfluent.SegmentSplitProportional && len(split.Targets) == 0 &&
		(prior.IsNull() || prior.IsUnknown()) {
		return types.ObjectNull(segmentSplitAttrTypes)
	}

	var priorTargets []attr.Value
	priorHasTargets := false
	if !prior.IsNull() && !prior.IsUnknown() {
		if list, ok := prior.Attributes()["targets"].(types.List); ok && !list.IsNull() && !list.IsUnknown() {
			priorTargets, priorHasTargets = list.Elements(), true
		}
	}

	elementType := types.ObjectType{AttrTypes: segmentSplitTargetAttrTypes}
	targets := types.ListNull(elementType)
	if len(split.Targets) > 0 || priorHasTargets {
		elements := make([]attr.Value, len(split.Targets))
		for i, target := range split.Targets {
			percent := types.Float64PointerValue(target.Percent)
			if i < len(priorTargets) {
				percent = samePercent(priorTargets[i], target, percent)
			}
			elements[i] = types.ObjectValueMust(segmentSplitTargetAttrTypes, map[string]attr.Value{
				"segment_id": types.StringValue(target.SegmentID),
				"percent":    percent,
			})
		}
		targets = types.ListValueMust(elementType, elements)
	}
	return types.ObjectValueMust(segmentSplitAttrTypes, map[string]attr.Value{
		"method":  types.StringValue(split.Method),
		"targets": targets,
	})
}

// samePercent keeps the prior value of a target's percent when the API answers the same number.
// Terraform hands a configured 33.33 back from state at a higher precision than the float64 the
// API's answer decodes to, and the two compare unequal, so without it every refresh would read a
// percent split as changed elsewhere and drop the ETag that guards the next write.
func samePercent(prior attr.Value, target costfluent.SegmentSplitTarget, read types.Float64) types.Float64 {
	object, ok := prior.(types.Object)
	if !ok || target.Percent == nil {
		return read
	}
	id, _ := object.Attributes()["segment_id"].(types.String)
	percent, _ := object.Attributes()["percent"].(types.Float64)
	if id.ValueString() != target.SegmentID || percent.IsNull() || percent.IsUnknown() ||
		percent.ValueFloat64() != *target.Percent {
		return read
	}
	return percent
}
