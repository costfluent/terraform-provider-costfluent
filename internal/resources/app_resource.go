package resources

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/apidiag"
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &AppResource{}
	_ resource.ResourceWithConfigure   = &AppResource{}
	_ resource.ResourceWithModifyPlan  = &AppResource{}
	_ resource.ResourceWithImportState = &AppResource{}
)

// appKeys are the API's AppKey names.
var appKeys = []string{"email", "slack", "teams", "webhook"}

type AppResource struct {
	managed
}

type AppResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Key           types.String `tfsdk:"key"`
	Name          types.String `tfsdk:"name"`
	Config        types.String `tfsdk:"config"`
	IsEnabled     types.Bool   `tfsdk:"is_enabled"`
	CreatedAt     types.String `tfsdk:"created_at"`
	SigningSecret types.String `tfsdk:"signing_secret"`
}

func NewAppResource() resource.Resource {
	return &AppResource{managed{name: "app", needs: Needs{
		Organization: true,
		Create:       []costfluent.Capability{costfluent.CapabilityCreateApp},
		Read:         []costfluent.Capability{costfluent.CapabilityViewMembers},
		Update:       []costfluent.Capability{costfluent.CapabilityUpdateApp},
		Delete:       []costfluent.Capability{costfluent.CapabilityDeleteApp},
	}}}
}

func (r *AppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Costfluent app: a notification channel budgets, cost alerts and report notifications " +
			"deliver to. " +
			r.needs.sentence(),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "App ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "The kind of channel: email, slack, teams or webhook. Changing it recreates the app.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(appKeys...),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "App name.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 200),
				},
			},
			"config": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				Description: "The channel's settings as a JSON object, usually written with jsonencode: recipients for " +
					"email (one address, or several separated by commas), webhookUrl for Slack and Teams, url and an " +
					"optional signing secret for a webhook, which must be public HTTPS, with its optional events: the " +
					"state changes it is sent (savings_item_status, provider_health, spend_band, subscription_status, " +
					"anomaly_status, connection_imported). A null or empty secret counts " +
					"as none. Removing a secret the config named, or setting it to null or empty, removes the " +
					"webhook's signing; a webhook whose config never named one keeps the secret Costfluent " +
					"generated. The API never returns the credentials in it, and returns a webhook's url as its " +
					"scheme and host followed by /***, so the provider keeps what was applied and cannot see a " +
					"change made elsewhere. An imported app starts from that redacted read, and the next apply " +
					"sends the configured values. The Terraform state holds the config as applied, credentials " +
					"included, so keep state in an encrypted backend with restricted access.",
			},
			"is_enabled": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the channel delivers.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"signing_secret": schema.StringAttribute{
				Computed:  true,
				Sensitive: true,
				Description: "The signing secret Costfluent generated for a webhook created without a secret in its " +
					"config; check each delivery's X-Costfluent-Signature with it. The API answers it once, at " +
					"creation, so the provider keeps it from then on. Null for any other channel, for a webhook " +
					"whose config names its own secret or has had one removed, and for an imported app. The " +
					"Terraform state holds it in plaintext, so keep state in an encrypted backend with restricted access.",
				PlanModifiers: []planmodifier.String{
					keepGeneratedSigningSecret{},
				},
			},
		},
	}
}

func (r *AppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AppResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := appConfig(plan.Config)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("config"), "Invalid app config", err.Error())
		return
	}
	app, err := r.client.CreateApp(ctx, &costfluent.CreateAppInput{Key: plan.Key.ValueString(), Name: plan.Name.ValueString(), Config: config})
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to create app", err)
		return
	}

	mapAppToModel(app, &plan)
	plan.SigningSecret = types.StringNull()
	if app.SigningSecret != "" {
		plan.SigningSecret = types.StringValue(app.SigningSecret)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, app.ETag)...)
	if app.Key == "webhook" && !hasWebhookSecret(config) && app.SigningSecret == "" {
		// The app exists and signs with a secret nobody can read. An error rather than a warning
		// leaves it tainted, so the next apply replaces it instead of keeping it unverifiable.
		resp.Diagnostics.AddAttributeError(path.Root("signing_secret"), "Signing secret not returned",
			"Costfluent created webhook app "+app.ID+" and generated its signing secret, but the answer did not "+
				"carry it"+replayReason(app)+". Deliveries are signed with a secret Terraform cannot read, so the "+
				"app is tainted and the next apply replaces it. Set a secret of your own in config, such as "+
				"secret = var.webhook_secret, so the signing secret never depends on the create's answer.")
	}
}

func replayReason(app *costfluent.App) string {
	if app.Replayed {
		return ": the create was retried and the API answered with its replay of the first attempt, which " +
			"never repeats a secret"
	}
	return ""
}

func (r *AppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AppResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	app, err := r.client.GetApp(ctx, state.ID.ValueString())
	if costfluent.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to read app", err)
		return
	}

	mapAppToModel(app, &state)
	if state.Config.IsNull() {
		// An import has no applied config to keep, so it starts from the redacted read; the next
		// apply writes the configuration's own.
		redacted, err := json.Marshal(app.Config)
		if err != nil {
			apidiag.AddError(&resp.Diagnostics, "Failed to read app", err)
			return
		}
		state.Config = types.StringValue(string(redacted))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, app.ETag)...)
}

func (r *AppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state AppResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := &costfluent.UpdateAppInput{}
	if !plan.Name.Equal(state.Name) {
		input.Name = plan.Name.ValueStringPointer()
	}
	if !plan.Config.Equal(state.Config) {
		config, err := appConfig(plan.Config)
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("config"), "Invalid app config", err.Error())
			return
		}
		applied, err := appConfig(state.Config)
		if err != nil {
			resp.Diagnostics.AddError("Failed to update app", "the applied config in state is not a JSON object: "+err.Error())
			return
		}
		// The API keeps a webhook's stored secret when an update leaves it out, in any casing of its key,
		// and removes it on a null or an empty one. A null or empty secret in the config means none, so
		// it is sent only as the null that removes a secret the applied config named, and otherwise left
		// out, keeping a generated one.
		withoutEmptyWebhookSecret(config)
		if hasWebhookSecret(applied) && !hasWebhookSecret(config) {
			config[webhookSecret] = nil
		}
		input.Config = config
		if plan.SigningSecret.IsUnknown() {
			plan.SigningSecret = signingSecretAfterUpdate(state.SigningSecret, applied, config)
		}
	}
	if plan.SigningSecret.IsUnknown() {
		plan.SigningSecret = state.SigningSecret
	}

	app, err := r.client.UpdateApp(ctx, state.ID.ValueString(), input, ifMatch(ctx, req.Private, &resp.Diagnostics))
	if changedOutsideTerraform(err, &resp.Diagnostics, "App", false) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to update app", err)
		return
	}

	mapAppToModel(app, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(setETag(ctx, resp.Private, app.ETag)...)
}

func (r *AppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AppResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApp(ctx, state.ID.ValueString(), ifMatch(ctx, req.Private, &resp.Diagnostics))
	if costfluent.IsNotFound(err) || changedOutsideTerraform(err, &resp.Diagnostics, "App", true) {
		return
	}
	if err != nil {
		apidiag.AddError(&resp.Diagnostics, "Failed to delete app", err)
	}
}

func (r *AppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

const webhookSecret = "secret"

// isWebhookSecret is a config entry the API reads as a webhook secret: its key in any casing with a
// value that is not null or an empty string, as CreateApp's WithSigningSecret decides.
func isWebhookSecret(key string, value any) bool {
	return strings.EqualFold(key, webhookSecret) && value != nil && value != ""
}

func hasWebhookSecret(config map[string]any) bool {
	for key, value := range config {
		if isWebhookSecret(key, value) {
			return true
		}
	}
	return false
}

func withoutEmptyWebhookSecret(config map[string]any) {
	for key, value := range config {
		if strings.EqualFold(key, webhookSecret) && !isWebhookSecret(key, value) {
			delete(config, key)
		}
	}
}

func appConfig(raw types.String) (map[string]any, error) {
	var config map[string]any
	if err := json.Unmarshal([]byte(raw.ValueString()), &config); err != nil {
		return nil, err
	}
	return config, nil
}

// mapAppToModel sets everything the API reports except config, which it reports redacted.
func mapAppToModel(app *costfluent.App, model *AppResourceModel) {
	model.ID = types.StringValue(app.ID)
	model.Key = types.StringValue(app.Key)
	model.Name = types.StringValue(app.Name)
	model.IsEnabled = types.BoolValue(app.IsEnabled)
	model.CreatedAt = types.StringValue(app.CreatedAt.Format(time.RFC3339))
}

// signingSecretAfterUpdate is what an update leaves of the signing secret the API generated at
// create: kept while the configuration names no secret, gone once it names one, which signs in its
// place, or removes one, which an update sends as null and so removes signing.
func signingSecretAfterUpdate(stored types.String, applied, planned map[string]any) types.String {
	if hasWebhookSecret(applied) || hasWebhookSecret(planned) {
		return types.StringNull()
	}
	return stored
}

// keepGeneratedSigningSecret plans the signing secret an update leaves, so a plan that changes
// nothing about the secret shows no change to it. A create, or a configuration not yet known,
// leaves it unknown.
type keepGeneratedSigningSecret struct{}

func (keepGeneratedSigningSecret) Description(context.Context) string {
	return "Keeps the generated signing secret while the configuration names no secret of its own."
}

func (m keepGeneratedSigningSecret) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepGeneratedSigningSecret) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plannedRaw, appliedRaw types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("config"), &plannedRaw)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("config"), &appliedRaw)...)
	if resp.Diagnostics.HasError() || plannedRaw.IsUnknown() {
		return
	}
	planned, err := appConfig(plannedRaw)
	if err != nil {
		return
	}
	applied, err := appConfig(appliedRaw)
	if err != nil {
		return
	}
	resp.PlanValue = signingSecretAfterUpdate(req.StateValue, applied, planned)
}
