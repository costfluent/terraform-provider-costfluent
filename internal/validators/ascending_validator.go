package validators

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type ascendingValidator struct {
	group     string
	groups    []string
	attribute string
}

// rankedElement is one element's place in the order: its group's index in groups, then its value.
type rankedElement struct {
	group int
	value int64
}

// StrictlyAscending requires a list of objects to be ordered by the string attribute group, in the
// order groups names its values, then by the int64 attribute, lowest first, each pair once. A null
// group is the first of groups, the value its default gives it. An element with an unknown value
// leaves the order to the apply, and one whose group is not in groups to the group's own validator.
func StrictlyAscending(group string, groups []string, attribute string) validator.List {
	return ascendingValidator{group: group, groups: groups, attribute: attribute}
}

func (v ascendingValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be listed by %s in the order %s, then by %s lowest first, each pair once",
		v.group, strings.Join(v.groups, ", "), v.attribute)
}

func (v ascendingValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v ascendingValidator) ValidateList(ctx context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	got := make([]rankedElement, 0, len(req.ConfigValue.Elements()))
	for _, element := range req.ConfigValue.Elements() {
		object, ok := element.(types.Object)
		if ok && (object.IsNull() || object.IsUnknown()) {
			return
		}
		var group types.String
		var field types.Int64
		if ok {
			group, ok = object.Attributes()[v.group].(types.String)
		}
		if ok {
			field, ok = object.Attributes()[v.attribute].(types.Int64)
		}
		if !ok {
			resp.Diagnostics.AddAttributeError(req.Path, "Invalid list element",
				fmt.Sprintf("Expected objects with a string %s and an int64 %s, got %s", v.group, v.attribute, element.Type(ctx)))
			return
		}
		if group.IsUnknown() || field.IsNull() || field.IsUnknown() {
			return
		}
		rank := 0
		if !group.IsNull() {
			if rank = slices.Index(v.groups, group.ValueString()); rank < 0 {
				return
			}
		}
		got = append(got, rankedElement{group: rank, value: field.ValueInt64()})
	}

	want := slices.Compact(slices.SortedFunc(slices.Values(got), func(a, b rankedElement) int {
		return cmp.Or(cmp.Compare(a.group, b.group), cmp.Compare(a.value, b.value))
	}))
	if slices.Equal(got, want) {
		return
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid list order",
		fmt.Sprintf("List the elements by %s in the order %s, then by %s lowest first, each pair once: the API "+
			"returns them in that order, so no other order can match the state. Expected [%s], got [%s].",
			v.group, strings.Join(v.groups, ", "), v.attribute, v.format(want), v.format(got)),
	)
}

// format writes elements as "group value", such as "actual 80".
func (v ascendingValidator) format(elements []rankedElement) string {
	parts := make([]string, 0, len(elements))
	for _, e := range elements {
		parts = append(parts, fmt.Sprintf("%s %d", v.groups[e.group], e.value))
	}
	return strings.Join(parts, ", ")
}
