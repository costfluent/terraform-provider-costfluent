package validators

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestTokenPrefix(t *testing.T) {
	for name, tc := range map[string]struct {
		value types.String
		valid bool
	}{
		"matching prefix":         {types.StringValue("wsp_0000000000000000000001"), true},
		"prefix alone":            {types.StringValue("wsp_"), true},
		"null is left to Require": {types.StringNull(), true},
		"unknown until apply":     {types.StringUnknown(), true},
		"other resource's token":  {types.StringValue("fld_0000000000000000000001"), false},
		"prefix in another case":  {types.StringValue("WSP_0000000000000000000001"), false},
		"prefix not at the start": {types.StringValue("x_wsp_0000000000000000000001"), false},
		"empty string":            {types.StringValue(""), false},
	} {
		t.Run(name, func(t *testing.T) {
			resp := &validator.StringResponse{}
			TokenPrefix("wsp_").ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("workspace_id"),
				ConfigValue: tc.value,
			}, resp)

			if got := !resp.Diagnostics.HasError(); got != tc.valid {
				t.Fatalf("valid = %v, want %v: %v", got, tc.valid, resp.Diagnostics)
			}
			if tc.valid {
				return
			}
			diag := resp.Diagnostics.Errors()[0]
			if diag.Summary() != "Invalid token format" ||
				!strings.Contains(diag.Detail(), `"wsp_"`) ||
				!strings.Contains(diag.Detail(), tc.value.ValueString()) {
				t.Fatalf("diagnostic = %q: %q", diag.Summary(), diag.Detail())
			}
		})
	}
}

func TestTokenPrefixDescribesTheExpectedPrefix(t *testing.T) {
	v := TokenPrefix("fld_")
	ctx := context.Background()

	if got := v.Description(ctx); got != "must start with fld_" {
		t.Fatalf("Description = %q", got)
	}
	if got := v.MarkdownDescription(ctx); got != v.Description(ctx) {
		t.Fatalf("MarkdownDescription = %q, want the plain description", got)
	}
}
