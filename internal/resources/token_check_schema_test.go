package resources_test

import (
	"reflect"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// hasPlanModifiers reports whether a schema attribute or block carries any plan modifier.
func hasPlanModifiers(v any) bool {
	field := reflect.ValueOf(v).FieldByName("PlanModifiers")
	return field.IsValid() && field.Len() > 0
}

// nestedAttributes is what a nested attribute or block holds.
func nestedAttributes(v any) map[string]schema.Attribute {
	switch n := v.(type) {
	case schema.SingleNestedAttribute:
		return n.Attributes
	case schema.ListNestedAttribute:
		return n.NestedObject.Attributes
	case schema.SetNestedAttribute:
		return n.NestedObject.Attributes
	case schema.MapNestedAttribute:
		return n.NestedObject.Attributes
	case schema.ListNestedBlock:
		return n.NestedObject.Attributes
	case schema.SetNestedBlock:
		return n.NestedObject.Attributes
	case schema.SingleNestedBlock:
		return n.Attributes
	}
	return nil
}

// The token check tells a replacement from an update by running the plan modifiers of every
// top-level attribute of the kinds it handles. A modifier on another kind, on a nested attribute
// or on a block would go unseen and check a replacement as an update.
func TestReplacesSeesEveryPlanModifier(t *testing.T) {
	t.Parallel()
	seen := 0
	for _, newResource := range provider.New("test")().Resources(t.Context()) {
		r := newResource()
		meta := &resource.MetadataResponse{}
		r.Metadata(t.Context(), resource.MetadataRequest{ProviderTypeName: "costfluent"}, meta)
		resp := &resource.SchemaResponse{}
		r.Schema(t.Context(), resource.SchemaRequest{}, resp)

		for name, attribute := range resp.Schema.Attributes {
			if hasPlanModifiers(attribute) {
				seen++
				switch attribute.(type) {
				case interface{ StringPlanModifiers() []planmodifier.String },
					interface{ Int64PlanModifiers() []planmodifier.Int64 },
					interface{ BoolPlanModifiers() []planmodifier.Bool },
					interface{ ListPlanModifiers() []planmodifier.List },
					interface{ MapPlanModifiers() []planmodifier.Map },
					interface{ ObjectPlanModifiers() []planmodifier.Object }:
				default:
					t.Errorf("%s.%s: plan modifiers on a %T, a kind the token check does not run", meta.TypeName, name, attribute)
				}
			}
			for nested, a := range nestedAttributes(attribute) {
				if hasPlanModifiers(a) {
					t.Errorf("%s.%s.%s: plan modifiers on a nested attribute, which the token check does not run", meta.TypeName, name, nested)
				}
			}
		}
		for name, block := range resp.Schema.Blocks {
			if hasPlanModifiers(block) {
				t.Errorf("%s.%s: plan modifiers on a block, which the token check does not run", meta.TypeName, name)
			}
			for nested, a := range nestedAttributes(block) {
				if hasPlanModifiers(a) {
					t.Errorf("%s.%s.%s: plan modifiers on a nested attribute, which the token check does not run", meta.TypeName, name, nested)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("found no attribute with plan modifiers, so the walk reads none")
	}
}
