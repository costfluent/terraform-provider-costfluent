package resources

import (
	"context"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func storedPolicyWithoutAccounts() *costfluent.TagPolicy {
	aws := costfluent.ProviderKeyAws
	return &costfluent.TagPolicy{Rules: []costfluent.TagPolicyRule{
		{Provider: &aws, AccountIDs: []string{}, RequiredKeys: []string{"team"}},
		{AccountIDs: []string{}, RequiredKeys: []string{"env"}},
	}}
}

func mapped(t *testing.T, policy *costfluent.TagPolicy, model TagPolicyResourceModel) TagPolicyResourceModel {
	t.Helper()
	if diags := mapTagPolicyToModel(context.Background(), policy, &model); diags.HasError() {
		t.Fatal(diags)
	}
	return model
}

// The API answers a rule without accounts with []: account_ids left unset stays unset, and one
// written as [] stays [], so neither plans a change that is not one.
func TestMapTagPolicyKeepsHowAnEmptyAccountListWasConfigured(t *testing.T) {
	t.Parallel()
	model := mapped(t, storedPolicyWithoutAccounts(), TagPolicyResourceModel{
		WorkspaceID: types.StringValue("wsp_1"),
		Rules: []TagPolicyRuleModel{
			{AccountIDs: types.ListNull(types.StringType)},
			{AccountIDs: types.ListValueMust(types.StringType, nil)},
		},
	})

	if !model.Rules[0].AccountIDs.IsNull() {
		t.Errorf("rule 0 account_ids %v, want it left unset", model.Rules[0].AccountIDs)
	}
	if got := model.Rules[1].AccountIDs; got.IsNull() || len(got.Elements()) != 0 {
		t.Errorf("rule 1 account_ids %v, want the configured empty list", got)
	}
	if model.ID.ValueString() != "wsp_1" {
		t.Errorf("id %v, want the workspace", model.ID)
	}
	if model.Rules[0].Provider.ValueString() != "aws" || !model.Rules[1].Provider.IsNull() {
		t.Errorf("providers %v and %v, want aws and unset", model.Rules[0].Provider, model.Rules[1].Provider)
	}
}

// An import has no configuration to keep, so a rule without accounts reads with account_ids unset,
// the way a configuration that leaves it out writes it.
func TestMapTagPolicyReadsNoAccountsAsUnsetOnImport(t *testing.T) {
	t.Parallel()
	model := mapped(t, storedPolicyWithoutAccounts(), TagPolicyResourceModel{WorkspaceID: types.StringValue("wsp_1")})

	for i, rule := range model.Rules {
		if !rule.AccountIDs.IsNull() {
			t.Errorf("rule %d account_ids %v, want it unset", i, rule.AccountIDs)
		}
	}
}

// Accounts added outside Terraform to a rule configured without any are drift, and show.
func TestMapTagPolicyShowsAccountsAddedOutsideTerraform(t *testing.T) {
	t.Parallel()
	policy := storedPolicyWithoutAccounts()
	policy.Rules[0].AccountIDs = []string{"123456789012"}
	model := mapped(t, policy, TagPolicyResourceModel{
		Rules: []TagPolicyRuleModel{{AccountIDs: types.ListNull(types.StringType)}},
	})

	if got := model.Rules[0].AccountIDs.Elements(); len(got) != 1 || !got[0].Equal(types.StringValue("123456789012")) {
		t.Errorf("rule 0 account_ids %v, want the stored account", got)
	}
}
