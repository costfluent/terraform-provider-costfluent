package validators

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ascendingValidator struct {
	attribute string
}

// StrictlyAscending requires a list of objects to be ordered by the named int64 attribute, lowest
// first, each value once. An element whose value is unknown leaves the order to the apply.
func StrictlyAscending(attribute string) validator.List {
	return ascendingValidator{attribute: attribute}
}

func (v ascendingValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be listed by %s, lowest first, each value once", v.attribute)
}

func (v ascendingValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ascendingValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	got := make([]int64, 0, len(req.ConfigValue.Elements()))
	for _, element := range req.ConfigValue.Elements() {
		object, ok := element.(types.Object)
		if ok && (object.IsNull() || object.IsUnknown()) {
			return
		}
		var field types.Int64
		if ok {
			field, ok = object.Attributes()[v.attribute].(types.Int64)
		}
		if !ok {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid list element",
				fmt.Sprintf("Expected objects with an int64 %s, got %s", v.attribute, element.Type(ctx)))
			return
		}
		if field.IsNull() || field.IsUnknown() {
			return
		}
		got = append(got, field.ValueInt64())
	}

	want := slices.Compact(slices.Sorted(slices.Values(got)))
	if slices.Equal(got, want) {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid list order",
		fmt.Sprintf("List the elements by %s, lowest first, each value once: the API stores them in that order, "+
			"so any other order would plan a replacement after an import. Expected %v, got %v.", v.attribute, want, got),
	)
}
