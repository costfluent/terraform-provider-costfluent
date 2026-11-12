package resources

import (
	"context"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource               = &GcpServiceAccountResource{}
	_ resource.ResourceWithConfigure  = &GcpServiceAccountResource{}
	_ resource.ResourceWithModifyPlan = &GcpServiceAccountResource{}
)

// GcpServiceAccountResource is the service account Costfluent operates for the organization in
// GCP. It belongs to the organization, not to this configuration: creating or refreshing it
// provisions or reads it, and destroying it only forgets it.
type GcpServiceAccountResource struct {
	managed
}

type GcpServiceAccountResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Email               types.String `tfsdk:"email"`
	OrganizationID      types.String `tfsdk:"organization_id"`
	DirectoryCustomerID types.String `tfsdk:"directory_customer_id"`
}

func NewGcpServiceAccountResource() resource.Resource {
	return &GcpServiceAccountResource{managed{name: "gcp_service_account", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityManageProviders},
		Read:         []costfluent.Capability{costfluent.CapabilityManageProviders},
	}}}
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
		Description: "The service account Costfluent operates for your organization in GCP. Grant it BigQuery Data " +
			"Viewer on your billing export dataset, then connect the billing account with a GCP " +
			"`costfluent_provider`. The account is created on first use and belongs to the organization: " +
			"destroying this resource only removes it from state. Costfluent deletes the account once no " +
			"connection or Cloud Storage destination has used it for a while; every refresh provisions it, " +
			"so the next plan creates it again and grants that depend on it are applied to it again. " +
			r.needs.sentence(),
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

func (r *GcpServiceAccountResource) Create(ctx context.Context, _ resource.CreateRequest, resp *resource.CreateResponse) {
	state, ok := r.provision(ctx, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Read provisions again rather than keeping state: Costfluent deletes an account nothing has used
// for a while, and the refresh that starts every plan must recreate it before the grants on it
// apply, or the grants fail against the deleted account. The email is derived from the
// organization, so a recreated account leaves state as it was.
func (r *GcpServiceAccountResource) Read(ctx context.Context, _ resource.ReadRequest, resp *resource.ReadResponse) {
	state, ok := r.provision(ctx, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *GcpServiceAccountResource) provision(ctx context.Context, diags *diag.Diagnostics) (GcpServiceAccountResourceModel, bool) {
	account, err := r.client.ProvisionGcpServiceAccount(ctx)
	if err != nil {
		apidiag.AddError(diags, "Failed to provision the GCP service account", err)
		return GcpServiceAccountResourceModel{}, false
	}

	return GcpServiceAccountResourceModel{
		ID:                  types.StringValue(account.ServiceAccountEmail),
		Email:               types.StringValue(account.ServiceAccountEmail),
		OrganizationID:      types.StringPointerValue(account.OrganizationID),
		DirectoryCustomerID: types.StringPointerValue(account.DirectoryCustomerID),
	}, true
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
