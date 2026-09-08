package resources

import (
	"context"
	"time"

	"github.com/costfluent/costfluent-go/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SegmentResource{}
	_ resource.ResourceWithConfigure   = &SegmentResource{}
	_ resource.ResourceWithImportState = &SegmentResource{}
)

// SegmentResource manages one segment cost can be assigned to.
//
// This is the resource the segment map is built from, and the reason it is worth having in
// Terraform at all: a re-org becomes a diff rather than an afternoon of re-entering segments in a UI.
type SegmentResource struct {
	client *costfluent.Client
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
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func NewSegmentResource() resource.Resource {
	return &SegmentResource{}
}

func (r *SegmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (r *SegmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent segment: where an allocation rule assigns cost.",
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
					validators.TokenPrefix("tem_"),
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
				Required:    true,
				Description: "Product the segment is responsible for.",
			},
			"cost_centre": schema.StringAttribute{
				Required:    true,
				Description: "Cost centre the segment charges to.",
			},
			"is_shared": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Create the workspace's shared-cost bucket instead of a named segment. " +
					"A shared segment names nothing; its cost is spread across direct segments by weight.",
				PlanModifiers: []planmodifier.Bool{
					// A shared bucket and a real segment are different things, not two states of one
					// thing: flipping this would change what every rule pointing here means.
					boolRequiresReplace{},
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

func (r *SegmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*costfluent.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *costfluent.Client")
		return
	}
	r.client = client
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

	segment, err := r.client.CreateSegment(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create segment", err.Error())
		return
	}

	mapSegmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
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
	list, err := r.client.ListSegments(ctx, state.WorkspaceID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read segment", err.Error())
		return
	}

	for i := range list.Segments {
		if list.Segments[i].ID == state.ID.ValueString() {
			mapSegmentToModel(&list.Segments[i], &state)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
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
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input)
	if err != nil {
		resp.Diagnostics.AddError("Failed to update segment", err.Error())
		return
	}

	mapSegmentToModel(segment, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *SegmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SegmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSegment(
		ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		return
	}
	if err != nil {
		// The server refuses while a rule still names this segment. Surfacing that as-is is the
		// useful failure: allocation is query-time, so a silent delete would restate every past
		// month rather than only affecting the future.
		resp.Diagnostics.AddError("Failed to delete segment", err.Error())
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
	model.TeamLabel = types.StringValue(t.TeamLabel)
	model.Product = types.StringValue(t.Product)
	model.CostCentre = types.StringValue(t.CostCentre)
	model.IsShared = types.BoolValue(t.IsShared)
	model.CreatedAt = types.StringValue(t.CreatedAt.Format(time.RFC3339))

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
