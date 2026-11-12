package validators

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The API's rule: MaximumLength, which counts .NET's UTF-16 code units. "é" is two UTF-8 bytes and
// one unit; "😀" is one rune and two units.
func TestUTF16LengthCountsWhatTheAPICounts(t *testing.T) {
	for name, tc := range map[string]struct {
		validator validator.String
		value     string
		ok        bool
	}{
		"ASCII at the limit":                 {UTF16LengthAtMost(200), strings.Repeat("a", 200), true},
		"ASCII past the limit":               {UTF16LengthAtMost(200), strings.Repeat("a", 201), false},
		"two-byte letters at the limit":      {UTF16LengthAtMost(200), strings.Repeat("é", 200), true},
		"two-byte letters past the limit":    {UTF16LengthAtMost(200), strings.Repeat("é", 201), false},
		"three-byte letters at the limit":    {UTF16LengthAtMost(200), strings.Repeat("€", 200), true},
		"astral character counting two":      {UTF16LengthAtMost(200), strings.Repeat("a", 198) + "😀", true},
		"astral character past the limit":    {UTF16LengthAtMost(200), strings.Repeat("a", 199) + "😀", false},
		"empty under at most":                {UTF16LengthAtMost(200), "", true},
		"empty under between 1 and 200":      {UTF16LengthBetween(1, 200), "", false},
		"one letter under between 1 and 200": {UTF16LengthBetween(1, 200), "é", true},
		"past the limit under between":       {UTF16LengthBetween(1, 200), strings.Repeat("é", 201), false},
	} {
		resp := &validator.StringResponse{}
		tc.validator.ValidateString(context.Background(), validator.StringRequest{
			Path: path.Root("name"), ConfigValue: types.StringValue(tc.value),
		}, resp)
		if resp.Diagnostics.HasError() == tc.ok {
			t.Errorf("%s: valid = %v, want %v", name, !resp.Diagnostics.HasError(), tc.ok)
		}
	}

	for _, value := range []types.String{types.StringUnknown(), types.StringNull()} {
		resp := &validator.StringResponse{}
		UTF16LengthBetween(1, 200).ValidateString(context.Background(), validator.StringRequest{ConfigValue: value}, resp)
		if resp.Diagnostics.HasError() {
			t.Errorf("%v: an unknown or absent value is left to the apply", value)
		}
	}
}

func TestUTF16LengthNamesTheLimitAndTheCount(t *testing.T) {
	resp := &validator.StringResponse{}
	UTF16LengthAtMost(200).ValidateString(context.Background(), validator.StringRequest{
		Path: path.Root("product"), ConfigValue: types.StringValue(strings.Repeat("a", 199) + "😀"),
	}, resp)
	errs := resp.Diagnostics.Errors()
	if len(errs) != 1 {
		t.Fatalf("201 units: %d errors, want 1", len(errs))
	}
	if got, want := errs[0].Detail(), "Attribute product must be at most 200 characters (UTF-16 code units), got: 201"; got != want {
		t.Errorf("detail %q, want %q", got, want)
	}
	for _, v := range []validator.String{UTF16LengthAtMost(200), UTF16LengthBetween(1, 200)} {
		if got := v.MarkdownDescription(context.Background()); got != v.Description(context.Background()) || got == "" {
			t.Errorf("markdown description %q", got)
		}
	}
	if got := UTF16LengthBetween(1, 200).Description(context.Background()); got != "must be 1 to 200 characters (UTF-16 code units)" {
		t.Errorf("description %q", got)
	}
}
