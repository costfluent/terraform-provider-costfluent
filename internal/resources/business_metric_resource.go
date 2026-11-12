package resources

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/validators"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &BusinessMetricResource{}
	_ resource.ResourceWithConfigure   = &BusinessMetricResource{}
	_ resource.ResourceWithModifyPlan  = &BusinessMetricResource{}
	_ resource.ResourceWithImportState = &BusinessMetricResource{}
)

type BusinessMetricResource struct {
	managed
}

type BusinessMetricResourceModel struct {
	ID           types.String `tfsdk:"id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Name         types.String `tfsdk:"name"`
	Unit         types.String `tfsdk:"unit"`
	LabelColumns types.List   `tfsdk:"label_columns"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func NewBusinessMetricResource() resource.Resource {
	return &BusinessMetricResource{managed{name: "business_metric", needs: Needs{
		Create: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics, costfluent.CapabilityViewCostReports},
		Read:   []costfluent.Capability{costfluent.CapabilityViewCostReports},
		Update: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics},
		Delete: []costfluent.Capability{costfluent.CapabilityManageBusinessMetrics},
	}}}
}

func (r *BusinessMetricResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent business metric: a business quantity, such as customers or transactions, " +
			"whose daily values a cost report divides its cost by. Its values are imported by CSV in the " +
			"console or through the API, not managed here. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Business metric ID.",
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
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Metric name, not blank.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
					notBlankValidator{},
				},
			},
			"unit": schema.StringAttribute{
				Required:    true,
				Description: "What one unit counts, in the singular, such as customer or transaction. Not blank.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 50),
					notBlankValidator{},
				},
			},
			"label_columns": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: fmt.Sprintf("Up to %d columns each value is labeled by, such as customer or region. Each is "+
					"named, at most %d characters, does not start like a spreadsheet formula (=, +, - or @, unless it "+
					"is a number), is not date or amount in any case, and differs from the others ignoring case. "+
					"Every stored value is keyed by them, so changing them replaces the metric and its values.",
					businessMetricMaxLabelColumns, businessMetricMaxLabelLength),
				Validators: []validator.List{
					listvalidator.SizeAtMost(businessMetricMaxLabelColumns),
					labelColumnsValidator{},
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *BusinessMetricResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan BusinessMetricResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.WorkspaceID = resolvedWorkspace(r.client, plan.WorkspaceID)

	var columns []string
	if !plan.LabelColumns.IsNull() && !plan.LabelColumns.IsUnknown() {
		resp.Diagnostics.Append(plan.LabelColumns.ElementsAs(ctx, &columns, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	metric, err := r.client.CreateBusinessMetric(ctx, &costfluent.CreateBusinessMetricInput{
		WorkspaceID:  plan.WorkspaceID.ValueString(),
		Name:         plan.Name.ValueString(),
		Unit:         plan.Unit.ValueString(),
		LabelColumns: columns,
	})
	if err != nil {
		createFailed(ctx, resp, "Failed to create business metric", err, plan.WorkspaceID)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.WorkspaceID = resolvedWorkspace(r.client, state.WorkspaceID)

	metric, err := r.client.GetBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read business metric", err)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateBusinessMetricInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Unit.Equal(state.Unit) {
		input.Unit = plan.Unit.ValueStringPointer()
	}

	metric, err := r.client.UpdateBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "Business metric", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update business metric", err)
		return
	}

	resp.Diagnostics.Append(mapBusinessMetricToModel(ctx, metric, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, metric.ETag)...)
}

func (r *BusinessMetricResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state BusinessMetricResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteBusinessMetric(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "Business metric", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete business metric", err)
	}
}

// ImportState takes "<workspace ID>:<ID>", or a bare ID in the provider's workspace.
func (r *BusinessMetricResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importOptionallyWorkspaceScoped(ctx, req, resp)
}

// mapBusinessMetricToModel keeps label_columns null when none is configured and the metric has
// none, so an omitted attribute does not plan a change to an empty list.
func mapBusinessMetricToModel(ctx context.Context, m *costfluent.BusinessMetric, model *BusinessMetricResourceModel) diag.Diagnostics {
	model.ID = types.StringValue(m.ID)
	model.Name = types.StringValue(m.Name)
	model.Unit = types.StringValue(m.Unit)
	model.CreatedAt = types.StringValue(m.CreatedAt.Format(time.RFC3339))
	if len(m.LabelColumns) == 0 && model.LabelColumns.IsNull() {
		model.LabelColumns = types.ListNull(types.StringType)
		return nil
	}
	var diags diag.Diagnostics
	model.LabelColumns, diags = types.ListValueFrom(ctx, types.StringType, m.LabelColumns)
	return diags
}

// The API's label column rule, which the contract does not state: a refused column fails only at
// apply, and a changed column plans a replacement, so without this check the apply destroys the old
// metric before the API refuses the new one.
const (
	businessMetricMaxLabelColumns = 5
	businessMetricMaxLabelLength  = 100
)

// businessMetricReservedColumns are the CSV columns every value import carries.
var businessMetricReservedColumns = []string{"date", "amount"}

// formulaStarts are the characters a spreadsheet may start a formula with, as the API reads them:
// the ASCII ones and the full-width forms of the four that have one.
var formulaStarts = []rune{'=', '+', '-', '@', '\t', '\r', '\n', '\uFF1D', '\uFF0B', '\uFF0D', '\uFF20'}

// plainNumber is a signed number as the API's invariant decimal parse reads one: digits with
// thousands separators anywhere after the first digit and before the point, or a point and digits.
var plainNumber = regexp.MustCompile(`^[+-](?:([0-9][0-9,]*)(?:\.([0-9]*))?|\.[0-9]+)$`)

// decimalMax is the largest magnitude a .NET decimal holds. A number past it fails the API's parse,
// so the API reads it as a formula.
const decimalMax = "79228162514264337593543950335"

// labelColumnsValidator holds each column, trimmed as the API trims it, to the API's rule.
type labelColumnsValidator struct{}

func (labelColumnsValidator) Description(_ context.Context) string {
	return fmt.Sprintf("each column named, at most %d characters, not starting like a formula, not %s, "+
		"and different from the others ignoring case",
		businessMetricMaxLabelLength, strings.Join(businessMetricReservedColumns, " or "))
}

func (v labelColumnsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (labelColumnsValidator) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	seen := map[string]string{}
	for i, element := range req.ConfigValue.Elements() {
		at := req.Path.AtListIndex(i)
		column, ok := element.(types.String)
		if !ok || column.IsUnknown() {
			continue
		}
		if column.IsNull() {
			resp.Diagnostics.AddAttributeError(at, "Invalid label column", fmt.Sprintf("%s is null.", at))
			continue
		}
		value := column.ValueString()
		if problem := labelColumnProblem(strings.TrimSpace(value)); problem != "" {
			resp.Diagnostics.AddAttributeError(at, "Invalid label column", fmt.Sprintf("%s %q %s.", at, value, problem))
			continue
		}
		key := ignoringCase(strings.TrimSpace(value))
		if first, repeated := seen[key]; repeated {
			resp.Diagnostics.AddAttributeError(at, "Invalid label column",
				fmt.Sprintf("%s %q repeats label column %q, ignoring case.", at, value, first))
			continue
		}
		seen[key] = value
	}
}

func labelColumnProblem(column string) string {
	switch {
	case column == "":
		return "is blank"
	case len(utf16.Encode([]rune(column))) > businessMetricMaxLabelLength:
		return fmt.Sprintf("is longer than %d characters", businessMetricMaxLabelLength)
	case startsAsFormula(column):
		return "starts like a spreadsheet formula and is not a number"
	}
	for _, reserved := range businessMetricReservedColumns {
		if ignoringCase(column) == ignoringCase(reserved) {
			return "is reserved: every value import carries a date and an amount column"
		}
	}
	return ""
}

// ignoringCase upper-cases each character as the API's ordinal ignore-case comparison does, which
// leaves the dotless i and the long s alone: to the API "ı" and "i" are different columns.
func ignoringCase(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\u0131' || r == '\u017F' {
			return r
		}
		return unicode.ToUpper(r)
	}, value)
}

func startsAsFormula(value string) bool {
	for _, start := range formulaStarts {
		if strings.HasPrefix(value, string(start)) {
			return !parsesAsDecimal(value)
		}
	}
	return false
}

// parsesAsDecimal reports whether the API's decimal parse accepts value. The parse rounds the fraction
// half to even and decimalMax is odd, so decimalMax with any fraction from one half up overflows.
func parsesAsDecimal(value string) bool {
	match := plainNumber.FindStringSubmatch(value)
	if match == nil {
		return false
	}
	integer := strings.TrimLeft(strings.ReplaceAll(match[1], ",", ""), "0")
	switch {
	case len(integer) != len(decimalMax):
		return len(integer) < len(decimalMax)
	case integer != decimalMax:
		return integer < decimalMax
	default:
		return match[2] == "" || match[2][0] < '5'
	}
}

// notBlankValidator refuses a value the API trims to nothing.
type notBlankValidator struct{}

func (notBlankValidator) Description(_ context.Context) string {
	return "must not be blank"
}

func (v notBlankValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (notBlankValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.TrimSpace(req.ConfigValue.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", fmt.Sprintf("%s is blank.", req.Path))
	}
}
