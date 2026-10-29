package resources_test

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// v03ProviderState is a connection as provider v0.3.x and v0.2.1 stored it: the display name in
// name.
func v03ProviderState() []byte {
	raw, err := json.Marshal(map[string]any{
		"id":                     "prv_1",
		"key":                    "aws",
		"name":                   "AWS prod",
		"description":            "Management account",
		"credentials":            map[string]any{"role_arn": "arn:aws:iam::123456789012:role/CostfluentBillingRole"},
		"settings":               map[string]any{"export_bucket": "cur-exports"},
		"status":                 "active",
		"parent_provider_id":     nil,
		"external_id":            "123456789012",
		"sync_frequency_minutes": 360,
		"last_sync_at":           "2026-10-01T06:00:00Z",
		"last_sync_status":       "succeeded",
		"next_sync_at":           "2026-10-02T06:00:00Z",
		"created_at":             "2026-09-01T00:00:00Z",
		"updated_at":             nil,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func upgradeProvider(t *testing.T, raw []byte) (map[string]tftypes.Value, tfprotov6.ProviderServer, *tfprotov6.UpgradeResourceStateResponse) {
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
	schema := schemas.ResourceSchemas["costfluent_provider"]
	if schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", schema.Version)
	}

	resp, err := server.UpgradeResourceState(ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: "costfluent_provider",
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
	value, err := resp.UpgradedState.Unmarshal(schema.ValueType())
	if err != nil {
		t.Fatal(err)
	}
	var attrs map[string]tftypes.Value
	if err := value.As(&attrs); err != nil {
		t.Fatal(err)
	}
	return attrs, server, resp
}

func TestProviderUpgradeState_v03NameBecomesDisplayName(t *testing.T) {
	t.Parallel()
	attrs, _, _ := upgradeProvider(t, v03ProviderState())

	wantString(t, attrs, "display_name", "AWS prod")
	if _, ok := attrs["name"]; ok {
		t.Fatal("name is still in the upgraded state")
	}
}

// Everything but the display name is carried as it was, credentials and settings included, which
// the API never returns and so no refresh could restore.
func TestProviderUpgradeState_v03CarriesTheConnectionOver(t *testing.T) {
	t.Parallel()
	attrs, _, _ := upgradeProvider(t, v03ProviderState())

	for name, want := range map[string]string{
		"id": "prv_1", "key": "aws", "description": "Management account", "status": "active",
		"external_id": "123456789012", "last_sync_at": "2026-10-01T06:00:00Z", "last_sync_status": "succeeded",
		"next_sync_at": "2026-10-02T06:00:00Z", "created_at": "2026-09-01T00:00:00Z",
	} {
		wantString(t, attrs, name, want)
	}
	wantNull(t, attrs, "parent_provider_id")
	wantNull(t, attrs, "updated_at")

	var minutes big.Float
	if err := attrs["sync_frequency_minutes"].As(&minutes); err != nil || minutes.Cmp(big.NewFloat(360)) != 0 {
		t.Fatalf("sync_frequency_minutes = %v (%v), want 360", &minutes, err)
	}
	for name, want := range map[string]map[string]string{
		"credentials": {"role_arn": "arn:aws:iam::123456789012:role/CostfluentBillingRole"},
		"settings":    {"export_bucket": "cur-exports"},
	} {
		var elements map[string]tftypes.Value
		if err := attrs[name].As(&elements); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(elements) != len(want) {
			t.Fatalf("%s = %v, want %v", name, elements, want)
		}
		for k, v := range want {
			if got := stringAttr(t, elements, k); got == nil || *got != v {
				t.Fatalf("%s.%s = %v, want %q", name, k, got, v)
			}
		}
	}
}

// A configuration that renames name to display_name, as the upgrade guide says, plans no change
// against the upgraded state: the same connection, not a rename or a replacement.
func TestProviderUpgradeState_v03RewrittenConfigurationPlansClean(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, server, upgraded := upgradeProvider(t, v03ProviderState())

	schemas, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	objectType := schemas.ResourceSchemas["costfluent_provider"].ValueType().(tftypes.Object)
	prior, err := upgraded.UpgradedState.Unmarshal(objectType)
	if err != nil {
		t.Fatal(err)
	}
	var priorAttrs map[string]tftypes.Value
	if err := prior.As(&priorAttrs); err != nil {
		t.Fatal(err)
	}

	config := map[string]tftypes.Value{}
	for name, attrType := range objectType.AttributeTypes {
		config[name] = tftypes.NewValue(attrType, nil)
	}
	for name, value := range map[string]any{"key": "aws", "display_name": "AWS prod", "description": "Management account", "sync_frequency_minutes": 360} {
		config[name] = tftypes.NewValue(objectType.AttributeTypes[name], value)
	}
	config["credentials"] = priorAttrs["credentials"]
	config["settings"] = priorAttrs["settings"]
	configValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, config))
	if err != nil {
		t.Fatal(err)
	}

	// Terraform proposes the configured values, and the prior value for every omitted computed one.
	proposed := map[string]tftypes.Value{}
	for name := range objectType.AttributeTypes {
		proposed[name] = priorAttrs[name]
		if !config[name].IsNull() {
			proposed[name] = config[name]
		}
	}
	proposedValue, err := tfprotov6.NewDynamicValue(objectType, tftypes.NewValue(objectType, proposed))
	if err != nil {
		t.Fatal(err)
	}

	plan, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "costfluent_provider",
		PriorState:       upgraded.UpgradedState,
		ProposedNewState: &proposedValue,
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
		t.Fatalf("plan replaces the connection: %v", plan.RequiresReplace)
	}
}
