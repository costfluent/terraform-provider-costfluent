package resources_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// v021CostReportState is a cost report as provider v0.2.1 stored it: group_by a single string.
func v021CostReportState(groupBy any) []byte {
	state := map[string]any{
		"id":            "rpt_1",
		"workspace_id":  "wsp_1",
		"title":         "Monthly",
		"folder_id":     nil,
		"chart_type":    "Bar",
		"date_bin":      "Month",
		"date_interval": "Last6Months",
		"start_date":    nil,
		"end_date":      nil,
		"filter":        nil,
		"group_by":      groupBy,
		"settings": map[string]any{
			"amortize": false, "include_credits": true, "include_refunds": true,
			"include_tax": true, "show_forecast": false, "compare_to_last_period": false,
		},
		"created_at": "2026-09-01T00:00:00Z",
		"updated_at": "2026-09-02T00:00:00Z",
	}
	raw, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	return raw
}

type upgradedSlot struct {
	Dimension string
	Key       *string
}

func upgradeCostReport(t *testing.T, raw []byte) []upgradedSlot {
	t.Helper()
	slots, _, _ := upgradeCostReportState(t, raw)
	return slots
}

func upgradeCostReportState(t *testing.T, raw []byte) ([]upgradedSlot, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse) {
	t.Helper()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	schema := schemas.ResourceSchemas["costfluent_cost_report"]
	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "costfluent_cost_report",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("upgrade failed: %s: %s", d.Summary, d.Detail)
		}
	}

	if schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", schema.Version)
	}

	objectType := schema.ValueType()
	value, err := resp.UpgradedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err := value.As(&attrs); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := attrs["title"].As(&title); err != nil || title != "Monthly" {
		t.Fatalf("title = %q (%v), want the prior value carried over", title, err)
	}

	var list []tftypes.Value
	if err := attrs["group_by"].As(&list); err != nil {
		t.Fatal(err)
	}
	slots := make([]upgradedSlot, len(list))
	for i, element := range list {
		var fields map[string]tftypes.Value
		if err := element.As(&fields); err != nil {
			t.Fatal(err)
		}
		if err := fields["dimension"].As(&slots[i].Dimension); err != nil {
			t.Fatal(err)
		}
		if err := fields["key"].As(&slots[i].Key); err != nil {
			t.Fatal(err)
		}
	}
	return slots, server, resp
}

func TestCostReportUpgradeState_v0GroupByStringBecomesOneSlot(t *testing.T) {
	t.Parallel()
	slots := upgradeCostReport(t, v021CostReportState("ServiceName"))
	if len(slots) != 1 || slots[0].Dimension != "ServiceName" || slots[0].Key != nil {
		t.Fatalf("group_by = %+v, want [{ServiceName}]", slots)
	}
}

func TestCostReportUpgradeState_v0TagGroupingKeepsItsKey(t *testing.T) {
	t.Parallel()
	slots := upgradeCostReport(t, v021CostReportState("Tag:team"))
	if len(slots) != 1 || slots[0].Dimension != "Tag" || slots[0].Key == nil || *slots[0].Key != "team" {
		t.Fatalf("group_by = %+v, want [{Tag team}]", slots)
	}
}

func TestCostReportUpgradeState_v0GroupByNullBecomesNoSlots(t *testing.T) {
	t.Parallel()
	slots := upgradeCostReport(t, v021CostReportState(nil))
	if len(slots) != 0 {
		t.Fatalf("group_by = %+v, want an empty list for a report v0.2.1 stored without one", slots)
	}
}

// A v0.2.1 configuration that left group_by out, as most did, plans no change once upgraded,
// which is what Terraform does next with the upgraded state.
func TestCostReportUpgradeState_v0DefaultGroupingPlansClean(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, server, upgraded := upgradeCostReportState(t, v021CostReportState("ServiceName"))

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	objectType := schemas.ResourceSchemas["costfluent_cost_report"].ValueType().(tftypes.Object)
	prior, err := upgraded.UpgradedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}

	config := map[string]tftypes.Value{}
	for name, attrType := range objectType.AttributeTypes {
		config[name] = tftypes.NewValue(attrType, nil)
	}
	config["workspace_id"] = tftypes.NewValue(tftypes.String, "wsp_1")
	config["title"] = tftypes.NewValue(tftypes.String, "Monthly")
	configValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, config))
	if err != nil {
		t.Fatal(err)
	}

	// Terraform proposes the prior value for every omitted computed attribute.
	plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "costfluent_cost_report",
		PriorState:       upgraded.UpgradedState,
		ProposedNewState: upgraded.UpgradedState,
		Config:           &configValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plan.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("plan failed: %s: %s", d.Summary, d.Detail)
		}
	}
	planned, err := plan.PlannedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	if diffs, err := prior.Diff(planned); err != nil || len(diffs) != 0 {
		t.Fatalf("plan changes the upgraded state: %v %v", diffs, err)
	}
	if len(plan.RequiresReplace) != 0 {
		t.Fatalf("plan replaces the report: %v", plan.RequiresReplace)
	}
}
