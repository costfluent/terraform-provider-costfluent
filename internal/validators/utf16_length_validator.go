package validators

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type utf16LengthValidator struct {
	min, max int
}

// UTF16LengthAtMost requires a string of at most maxLength characters as the API counts them:
// UTF-16 code units, the length .NET's MaximumLength checks. stringvalidator's LengthAtMost counts
// UTF-8 bytes, which refuses at plan a value with accented letters the API accepts ("é" is two
// bytes and one unit); UTF8LengthAtMost counts runes, which lets through a value with characters
// outside the Basic Multilingual Plane ("😀" is one rune and two units) that the API refuses at
// apply.
func UTF16LengthAtMost(maxLength int) validator.String {
	return utf16LengthValidator{max: maxLength}
}

// UTF16LengthBetween requires a string of minLength to maxLength characters, counted as
// UTF16LengthAtMost counts them.
func UTF16LengthBetween(minLength, maxLength int) validator.String {
	return utf16LengthValidator{min: minLength, max: maxLength}
}

func (v utf16LengthValidator) Description(_ context.Context) string {
	if v.min == 0 {
		return fmt.Sprintf("must be at most %d characters (UTF-16 code units)", v.max)
	}
	return fmt.Sprintf("must be %d to %d characters (UTF-16 code units)", v.min, v.max)
}

func (v utf16LengthValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v utf16LengthValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if length := utf16Length(req.ConfigValue.ValueString()); length < v.min || length > v.max {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid Attribute Value Length",
			fmt.Sprintf("Attribute %s %s, got: %d", req.Path, v.Description(ctx), length))
	}
}

func utf16Length(s string) int {
	length := 0
	for _, r := range s {
		length += utf16.RuneLen(r)
	}
	return length
}
