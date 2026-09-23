package resources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &GcpServiceAccountResource{}
	_ resource.ResourceWithConfigure = &GcpServiceAccountResource{}
)

// GcpServiceAccountResource is the service account Costfluent operates for the organization in
// GCP. It belongs to the organization, not to this configuration: creating it provisions or reads
// it, and destroying it only forgets it.
type GcpServiceAccountResource struct {
	client *costfluent.Client
}

type GcpServiceAccountResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Email               types.String `tfsdk:"email"`
	OrganizationID      types.String `tfsdk:"organization_id"`
	DirectoryCustomerID types.String `tfsdk:"directory_customer_id"`
}

func NewGcpServiceAccountResource() resource.Resource {
	return &GcpServiceAccountResource{}
}

func (r *GcpServiceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gcp_service_account"
}

func (r *GcpServiceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:      true,
			Description:   description,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}

	resp.Schema = schema.Schema{
		Description: "The service account Costfluent operates for your organization in GCP. Grant it BigQuery " +
			"Data Viewer on your billing export dataset, then connect the billing account with a GCP " +
			"`costfluent_provider`. The account is created on first use and belongs to the organization: " +
			"destroying this resource only removes it from state.",
		Attributes: map[string]schema.Attribute{
			"id":    computed("The service account's email."),
			"email": computed("The service account's email: the principal to grant BigQuery Data Viewer on the dataset."),
			"organization_id": computed("Costfluent's Google organization ID, for a domain-restricted sharing " +
				"policy to allow. Empty when not configured."),
			"directory_customer_id": computed("Costfluent's Google Workspace customer ID, for a domain-restricted " +
				"sharing policy to allow. Empty when not configured."),
		},
	}
}

func (r *GcpServiceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *GcpServiceAccountResource) Create(ctx context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	account, err := r.client.ProvisionGcpServiceAccount(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Failed to provision the GCP service account", err.Error())
		return
	}

	state := GcpServiceAccountResourceModel{
		ID:                  types.StringValue(account.ServiceAccountEmail),
		Email:               types.StringValue(account.ServiceAccountEmail),
		OrganizationID:      types.StringPointerValue(account.OrganizationID),
		DirectoryCustomerID: types.StringPointerValue(account.DirectoryCustomerID),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Read keeps state: the account is derived from the organization and never changes.
func (r *GcpServiceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state GcpServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update has nothing to change: the resource has no arguments.
func (r *GcpServiceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state GcpServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *GcpServiceAccountResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"GCP service account kept",
		"The service account belongs to your Costfluent organization and is not deleted. Remove its BigQuery "+
			"Data Viewer grant on the dataset to revoke Costfluent's access.",
	)
}
