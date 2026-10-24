package resources

import (
	"context"
	"testing"
	"time"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const configuredFilter = `[{"values":["Amazon EC2"],"operator":"In","field":"ServiceName"}]`

const storedFilter = `[{"field":"ServiceName","operator":"In","values":["Amazon EC2"]}]`

func storedTag() *costfluent.VirtualTag {
	return &costfluent.VirtualTag{
		ID:               "vtg_1",
		Key:              "team",
		CollapsedTagKeys: []string{},
		Rules:            []costfluent.VirtualTagRule{{Value: "platform", Filter: storedFilter}},
		CreatedAt:        time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// A filter the API stores in its canonical form is the configured one, so it plans no change.
func TestMapVirtualTagKeepsAJSONEqualFilterAsConfigured(t *testing.T) {
	t.Parallel()
	model := VirtualTagResourceModel{
		CollapsedTagKeys: types.ListNull(types.StringType),
		Rules:            []VirtualTagRuleModel{{Value: types.StringValue("platform"), Filter: types.StringValue(configuredFilter)}},
	}

	if diags := mapVirtualTagToModel(context.Background(), nil, storedTag(), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Rules[0].Filter.ValueString(); got != configuredFilter {
		t.Fatalf("filter %s, want the configured %s", got, configuredFilter)
	}
	if !model.CollapsedTagKeys.IsNull() {
		t.Fatalf("collapsed_tag_keys %v, want it left unset", model.CollapsedTagKeys)
	}
}

// A filter changed outside Terraform is drift and shows as the stored value.
func TestMapVirtualTagShowsAChangedFilter(t *testing.T) {
	t.Parallel()
	model := VirtualTagResourceModel{
		CollapsedTagKeys: types.ListNull(types.StringType),
		Rules: []VirtualTagRuleModel{{
			Value:  types.StringValue("platform"),
			Filter: types.StringValue(`[{"field":"Region","operator":"In","values":["eu-west-1"]}]`),
		}},
	}

	if diags := mapVirtualTagToModel(context.Background(), nil, storedTag(), &model); diags.HasError() {
		t.Fatal(diags)
	}

	if got := model.Rules[0].Filter.ValueString(); got != storedFilter {
		t.Fatalf("filter %s, want the stored %s", got, storedFilter)
	}
}
