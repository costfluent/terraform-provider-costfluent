package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDecimalAcceptsWhatTheAPIAccepts(t *testing.T) {
	for value, ok := range map[string]bool{
		"500": true, "1250.50": true, "-3.12345678": true,
		"1e3": false, "1,000": false, "1.123456789": false, "": false,
	} {
		resp := &validator.StringResponse{}
		Decimal().ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("amount"), ConfigValue: types.StringValue(value),
		}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: valid = %v, want %v", value, !resp.Diagnostics.HasError(), ok)
		}
	}

	resp := &validator.StringResponse{}
	Decimal().ValidateString(context.Background(), validator.StringRequest{ConfigValue: types.StringUnknown()}, resp)
	if resp.Diagnostics.HasError() {
		t.Error("an unknown amount is left to the apply")
	}
}

func TestDecimalDescribesTheAcceptedForm(t *testing.T) {
	if got := Decimal().MarkdownDescription(context.Background()); got != Decimal().Description(context.Background()) || got == "" {
		t.Errorf("markdown description %q", got)
	}
}
