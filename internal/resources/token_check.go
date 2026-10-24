package resources

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/costfluent/terraform-provider-costfluent/internal/session"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Needs is what a resource's actions need of the API token: an organization token for every action
// when an operation it reads, updates or destroys with is organization-level (Organization), or
// for creating only when only its create is (OrganizationCreate); and the capabilities the
// operations each action always sends declare, read-backs included. Conditional is what only some
// plans send (a CQL filter's translation, a saved filter or settings applied right after a create,
// a link created disabled, a link's accounts changed), each required exactly when the plan sends
// it. capabilities_test.go holds every declaration to the Public API contract's
// x-costfluent-capability and x-costfluent-scope.
type Needs struct {
	Organization                 bool
	OrganizationCreate           bool
	Create, Read, Update, Delete []costfluent.Capability
	Conditional                  []Conditional
}

// Conditional is a capability only some plans send, and which.
type Conditional struct {
	Capability costfluent.Capability
	// Organization is a call a workspace token is refused, of an operation a workspace token may
	// otherwise send: the contract's organization capability for part of a change.
	Organization bool
	// Sent reports whether applying req's plan sends the call that needs Capability; creating is
	// true when the plan creates the object, a replacement included. A value not known until
	// apply counts as sent.
	Sent func(ctx context.Context, req resource.ModifyPlanRequest, creating bool) (bool, diag.Diagnostics)
}

// all is every capability of n, each once, in the order create, read, update, delete, conditional.
func (n Needs) all() []costfluent.Capability {
	var all []costfluent.Capability
	conditional := make([]costfluent.Capability, len(n.Conditional))
	for i, c := range n.Conditional {
		conditional[i] = c.Capability
	}
	for _, need := range slices.Concat(n.Create, n.Read, n.Update, n.Delete, conditional) {
		if !slices.Contains(all, need) {
			all = append(all, need)
		}
	}
	return all
}

// sentence is how a resource's description names its token: the capabilities a workspace token
// may use on the workspace, then those only an organization token may, when there are both.
func (n Needs) sentence() string {
	if n.Organization {
		return fmt.Sprintf("Needs an organization token with %s.", codeList(n.all()))
	}
	workspace, organization := n.split()
	if len(organization) == 0 {
		return fmt.Sprintf("Needs a token with %s on the workspace.", codeList(n.all()))
	}
	return fmt.Sprintf("Needs a token with %s on the workspace, and an organization token with %s for what the "+
		"organization decides.", codeList(workspace), codeList(organization))
}

// split is every capability of a resource that is not wholly organization-level, each once per
// list, in the order create, read, update, delete, conditional: those a workspace token may use,
// and those its organization-level create and conditional calls need.
func (n Needs) split() (workspace, organization []costfluent.Capability) {
	add := func(list *[]costfluent.Capability, capabilities ...costfluent.Capability) {
		for _, c := range capabilities {
			if !slices.Contains(*list, c) {
				*list = append(*list, c)
			}
		}
	}
	if n.OrganizationCreate {
		add(&organization, n.Create...)
	} else {
		add(&workspace, n.Create...)
	}
	add(&workspace, slices.Concat(n.Read, n.Update, n.Delete)...)
	for _, c := range n.Conditional {
		if c.Organization {
			add(&organization, c.Capability)
		} else {
			add(&workspace, c.Capability)
		}
	}
	return workspace, organization
}

// codeList joins capabilities as code spans: `a`, `b` and `c`.
func codeList(capabilities []costfluent.Capability) string {
	quoted := make([]string, len(capabilities))
	for i, c := range capabilities {
		quoted[i] = "`" + string(c) + "`"
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

// managed is what every resource embeds: its type name, what its actions need of the API token,
// and, once the provider is configured, the client and the token that client authenticates with.
// It names the resource, takes the session, and checks the token when Terraform plans, so an apply
// never stops after some resources exist because a later one's token was short.
type managed struct {
	name   string
	needs  Needs
	client *costfluent.Client
	me     *costfluent.Me
}

func (m *managed) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + m.name
}

func (m *managed) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	s, ok := req.ProviderData.(*session.Session)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *session.Session, got %T", req.ProviderData))
		return
	}
	m.client, m.me = s.Client, s.Me
}

// TokenNeeds is what the resource's actions need of the API token.
func (m *managed) TokenNeeds() Needs { return m.needs }

func (m *managed) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	CheckToken(ctx, m.me, "costfluent_"+m.name, m.needs, req, resp)
}

// CheckToken refuses a plan whose token cannot do what the plan is about to do to a resource named
// name: an organization-level resource with a workspace token, a resource in another workspace
// than a workspace token's, or an action whose capabilities the token lacks. Creating and updating
// also need what reading back needs, because Terraform reads the resource on every later plan; a
// replacement deletes and creates, so it needs those and not the update; and each conditional call
// the plan sends is needed too. A plan that changes nothing needs nothing, and an unconfigured
// provider (me nil, as during validation) checks nothing.
func CheckToken(ctx context.Context, me *costfluent.Me, name string, needs Needs, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if me == nil {
		return
	}
	var action string
	var wanted []costfluent.Capability
	switch {
	case req.Plan.Raw.IsNull():
		action, wanted = "destroying", needs.Delete
	case req.State.Raw.IsNull():
		action, wanted = "creating", slices.Concat(needs.Create, needs.Read)
	case req.Plan.Raw.Equal(req.State.Raw):
		return
	default:
		replacing, diags := replaces(ctx, req)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
		if replacing {
			action, wanted = "replacing", slices.Concat(needs.Delete, needs.Create, needs.Read)
		} else {
			action, wanted = "updating", slices.Concat(needs.Update, needs.Read)
		}
	}
	organization := needs.Organization || (needs.OrganizationCreate && action != "updating" && action != "destroying")
	if action != "destroying" {
		creating := action != "updating"
		for _, c := range needs.Conditional {
			sent, diags := c.Sent(ctx, req, creating)
			resp.Diagnostics.Append(diags...)
			if diags.HasError() {
				return
			}
			if sent {
				wanted = append(wanted, c.Capability)
				organization = organization || c.Organization
			}
		}
	}

	if me.TokenType == costfluent.TokenTypeWorkspace {
		workspace := "its workspace"
		if me.WorkspaceID != nil {
			workspace = *me.WorkspaceID
		}
		if needs.Organization {
			resp.Diagnostics.AddError(name+" needs an organization token",
				fmt.Sprintf("%s is managed at the organization level; this is a workspace token for %s. "+
					"Create an organization token in Settings, API tokens.", name, workspace))
			return
		}
		if organization {
			resp.Diagnostics.AddError(name+" needs an organization token",
				fmt.Sprintf("%s %s is decided at the organization level; this is a workspace token for %s. "+
					"Create an organization token in Settings, API tokens. Nothing was changed.", action, name, workspace))
			return
		}
		target, diags := plannedWorkspace(ctx, req)
		resp.Diagnostics.Append(diags...)
		if diags.HasError() {
			return
		}
		if me.WorkspaceID != nil && target != "" && target != *me.WorkspaceID {
			resp.Diagnostics.AddError("The API token cannot reach the workspace of "+name,
				fmt.Sprintf("%s is in workspace %s; this is a workspace token for %s. Use a token for %s, or an "+
					"organization token, from Settings, API tokens. Nothing was changed.", name, target, workspace, target))
			return
		}
	}
	if missing := me.Missing(wanted...); len(missing) > 0 {
		pronoun := "it"
		if len(missing) > 1 {
			pronoun = "them"
		}
		resp.Diagnostics.AddError(fmt.Sprintf("The API token cannot apply %s", name),
			fmt.Sprintf("The API token lacks %s, which %s %s needs; create a token that carries %s in Settings, API tokens. "+
				"Nothing was changed.", codeList(missing), action, name, pronoun))
	}
}

// plannedWorkspace is the workspace_id the plan acts in (the prior one when destroying), or empty
// when the resource has none or it is not known until apply.
func plannedWorkspace(ctx context.Context, req resource.ModifyPlanRequest) (string, diag.Diagnostics) {
	if _, ok := req.State.Schema.GetAttributes()["workspace_id"]; !ok {
		return "", nil
	}
	var workspace types.String
	var diags diag.Diagnostics
	if req.Plan.Raw.IsNull() {
		diags = req.State.GetAttribute(ctx, path.Root("workspace_id"), &workspace)
	} else {
		diags = req.Plan.GetAttribute(ctx, path.Root("workspace_id"), &workspace)
	}
	return workspace.ValueString(), diags
}

// replaces reports whether Terraform will replace the resource rather than update it in place:
// whether a top-level attribute's plan modifiers require replacement for this change. The
// framework runs them before ModifyPlan but hands it none of their verdicts, so they run again
// here; TestReplacesSeesEveryPlanModifier holds every resource's schema to the kinds handled.
func replaces(ctx context.Context, req resource.ModifyPlanRequest) (bool, diag.Diagnostics) {
	for name, attribute := range req.Plan.Schema.GetAttributes() {
		p := path.Root(name)
		var replace bool
		var diags diag.Diagnostics
		switch a := attribute.(type) {
		case interface{ StringPlanModifiers() []planmodifier.String }:
			replace, diags = replacedBy(ctx, req, p, a.StringPlanModifiers(), func(m planmodifier.String, config, plan, state types.String) bool {
				r := &planmodifier.StringResponse{PlanValue: plan}
				m.PlanModifyString(ctx, planmodifier.StringRequest{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		case interface{ Int64PlanModifiers() []planmodifier.Int64 }:
			replace, diags = replacedBy(ctx, req, p, a.Int64PlanModifiers(), func(m planmodifier.Int64, config, plan, state types.Int64) bool {
				r := &planmodifier.Int64Response{PlanValue: plan}
				m.PlanModifyInt64(ctx, planmodifier.Int64Request{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		case interface{ BoolPlanModifiers() []planmodifier.Bool }:
			replace, diags = replacedBy(ctx, req, p, a.BoolPlanModifiers(), func(m planmodifier.Bool, config, plan, state types.Bool) bool {
				r := &planmodifier.BoolResponse{PlanValue: plan}
				m.PlanModifyBool(ctx, planmodifier.BoolRequest{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		case interface{ ListPlanModifiers() []planmodifier.List }:
			replace, diags = replacedBy(ctx, req, p, a.ListPlanModifiers(), func(m planmodifier.List, config, plan, state types.List) bool {
				r := &planmodifier.ListResponse{PlanValue: plan}
				m.PlanModifyList(ctx, planmodifier.ListRequest{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		case interface{ MapPlanModifiers() []planmodifier.Map }:
			replace, diags = replacedBy(ctx, req, p, a.MapPlanModifiers(), func(m planmodifier.Map, config, plan, state types.Map) bool {
				r := &planmodifier.MapResponse{PlanValue: plan}
				m.PlanModifyMap(ctx, planmodifier.MapRequest{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		case interface{ ObjectPlanModifiers() []planmodifier.Object }:
			replace, diags = replacedBy(ctx, req, p, a.ObjectPlanModifiers(), func(m planmodifier.Object, config, plan, state types.Object) bool {
				r := &planmodifier.ObjectResponse{PlanValue: plan}
				m.PlanModifyObject(ctx, planmodifier.ObjectRequest{Path: p, PathExpression: p.Expression(), Config: req.Config,
					ConfigValue: config, Plan: req.Plan, PlanValue: plan, State: req.State, StateValue: state, Private: req.Private}, r)
				return r.RequiresReplace
			})
		}
		if replace || diags.HasError() {
			return replace, diags
		}
	}
	return false, nil
}

// replacedBy reads the attribute at p from the configuration, the plan and the state, and reports
// whether any of its modifiers requires replacement, as run reports for one.
func replacedBy[V attr.Value, M any](
	ctx context.Context, req resource.ModifyPlanRequest, p path.Path, modifiers []M, run func(m M, config, plan, state V) bool,
) (bool, diag.Diagnostics) {
	if len(modifiers) == 0 {
		return false, nil
	}
	var config, plan, state V
	var diags diag.Diagnostics
	diags.Append(req.Config.GetAttribute(ctx, p, &config)...)
	diags.Append(req.Plan.GetAttribute(ctx, p, &plan)...)
	diags.Append(req.State.GetAttribute(ctx, p, &state)...)
	if diags.HasError() {
		return false, diags
	}
	for _, m := range modifiers {
		if run(m, config, plan, state) {
			return true, diags
		}
	}
	return false, diags
}

// sentWhenSet is a conditional call sent after a create when the string attribute at p is set, and
// on an update when it changes.
func sentWhenSet(p path.Path) func(context.Context, resource.ModifyPlanRequest, bool) (bool, diag.Diagnostics) {
	return func(ctx context.Context, req resource.ModifyPlanRequest, creating bool) (bool, diag.Diagnostics) {
		var plan, state types.String
		diags := req.Plan.GetAttribute(ctx, p, &plan)
		if creating || diags.HasError() {
			return !plan.IsNull(), diags
		}
		diags.Append(req.State.GetAttribute(ctx, p, &state)...)
		return !plan.Equal(state), diags
	}
}

// sentUnless is a conditional call sent after a create unless the boolean attribute at p is
// planned as value, which is what the create leaves it at, and on every update.
func sentUnless(p path.Path, value bool) func(context.Context, resource.ModifyPlanRequest, bool) (bool, diag.Diagnostics) {
	return func(ctx context.Context, req resource.ModifyPlanRequest, creating bool) (bool, diag.Diagnostics) {
		if !creating {
			return true, nil
		}
		var plan types.Bool
		diags := req.Plan.GetAttribute(ctx, p, &plan)
		return plan.IsUnknown() || plan.IsNull() || plan.ValueBool() != value, diags
	}
}

// sentForCQL is the translation of a CQL filter, which reading the object back sends for each
// filter at paths written as CQL rather than JSON.
func sentForCQL(paths ...path.Expression) func(context.Context, resource.ModifyPlanRequest, bool) (bool, diag.Diagnostics) {
	return func(ctx context.Context, req resource.ModifyPlanRequest, _ bool) (bool, diag.Diagnostics) {
		var diags diag.Diagnostics
		for _, expression := range paths {
			matched, matchDiags := req.Plan.PathMatches(ctx, expression)
			diags.Append(matchDiags...)
			for _, p := range matched {
				var filter types.String
				diags.Append(req.Plan.GetAttribute(ctx, p, &filter)...)
				if filter.IsUnknown() || (!filter.IsNull() && isCQL(filter.ValueString())) {
					return true, diags
				}
			}
		}
		return false, diags
	}
}
