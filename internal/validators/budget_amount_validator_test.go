package validators

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The API's rule: GreaterThan(0).PrecisionScale(18, 2, ignoreTrailingZeros: true).
func TestBudgetAmountAcceptsWhatTheAPIAccepts(t *testing.T) {
	for value, ok := range map[string]bool{
		"0.01": true, "1250.50": true, "1250.50000000": true, "500": true, "0012.5": true,
		"9999999999999999.99": true, "9999999999999999": true,
		"0": false, "0.00": false, "-5": false, "-0.01": false, "0.001": false, "1250.125": false,
		"1250.12500001": false, "10000000000000000": false, "10000000000000000.00": false,
		"1250.123456789": false, "1e3": false, "": false,
	} {
		resp := &validator.StringResponse{}
		BudgetAmount().ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("amount"), ConfigValue: types.StringValue(value),
		}, resp)
		if resp.Diagnostics.HasError() == ok {
			t.Errorf("%q: valid = %v, want %v", value, !resp.Diagnostics.HasError(), ok)
		}
	}

	for _, value := range []types.String{types.StringUnknown(), types.StringNull()} {
		resp := &validator.StringResponse{}
		BudgetAmount().ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%v: an unknown or absent amount is left to the apply", value)
		}
	}
}

func TestBudgetAmountDescribesTheAcceptedForm(t *testing.T) {
	if got := BudgetAmount().MarkdownDescription(context.Background()); got != BudgetAmount().Description(context.Background()) || got == "" {
		t.Errorf("markdown description %q", got)
	}
}
