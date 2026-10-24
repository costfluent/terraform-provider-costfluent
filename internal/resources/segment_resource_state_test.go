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

// A state the provider wrote before label and split existed holds neither. It reads at the same
// schema version as a segment without them, and the configuration it was written from plans no
// change, so adding the two needed no state upgrade.
func TestSegmentStateWithoutALabelOrSplitPlansNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(provider.New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	schema := schemas.ResourceSchemas["costfluent_segment"]
	if schema.Version != 0 {
		t.Fatalf("schema version = %d, want 0", schema.Version)
	}
	objectType := schema.ValueType().(tftypes.Object)

	raw, err := json.Marshal(map[string]any{
		"id": "seg_1", "workspace_id": "wsp_1", "team_id": nil, "team_label": "Platform", "team_name": nil,
		"product": "Checkout", "cost_centre": "CC-100", "is_shared": false,
		"created_at": "2026-10-01T00:00:00Z", "updated_at": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "costfluent_segment",
		Version:  0,
		RawState: &tfprotov6.RawState{JSON: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range upgraded.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("reading the state failed: %s: %s", d.Summary, d.Detail)
		}
	}
	prior, err := upgraded.UpgradedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err := prior.As(&attrs); err != nil {
		t.Fatal(err)
	}
	wantNull(t, attrs, "label")
	wantNull(t, attrs, "split")

	// The configuration is the state without its computed attributes.
	config := map[string]tftypes.Value{}
	for name, value := range attrs {
		config[name] = value
	}
	for _, computed := range []string{"id", "team_name", "is_shared", "created_at", "updated_at"} {
		config[computed] = tftypes.NewValue(objectType.AttributeTypes[computed], nil)
	}
	configValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, config))
	if err != nil {
		t.Fatal(err)
	}

	plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "costfluent_segment",
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
	if len(plan.RequiresReplace) != 0 || !planned.Equal(prior) {
		t.Fatalf("planned %v (replacing %v), want the prior state %v unchanged", planned, plan.RequiresReplace, prior)
	}
}
