package apidiag

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestAddErrorNamesTheCapabilityA403Lacks(t *testing.T) {
	var diags diag.Diagnostics
	err := fmt.Errorf("listing: %w", &costfluent.APIError{StatusCode: 403, Detail: "API token lacks capability: viewMembers",
		Errors: []costfluent.FieldError{{Name: "capability", Reason: "viewMembers", Code: costfluent.CodeMissingCapability}}})
	AddError(&diags, "Failed to list users", err)

	if len(diags) != 1 || diags[0].Summary() != "Failed to list users" {
		t.Fatalf("diagnostics = %v, want one titled Failed to list users", diags)
	}
	want := "The API token lacks `viewMembers`; create a token that carries it in Settings, API tokens. (" + err.Error() + ")"
	if diags[0].Detail() != want {
		t.Errorf("detail = %q, want %q", diags[0].Detail(), want)
	}
}

func TestAddErrorReportsAnyOtherErrorAsIs(t *testing.T) {
	for name, err := range map[string]error{
		"a scope 403":      &costfluent.APIError{StatusCode: 403, Detail: "Workspace token cannot access other workspaces"},
		"not an API error": errors.New("network down"),
	} {
		var diags diag.Diagnostics
		AddError(&diags, "Failed", err)
		if len(diags) != 1 || diags[0].Detail() != err.Error() || strings.Contains(diags[0].Detail(), "Settings, API tokens") {
			t.Errorf("%s: diagnostics = %v, want the error alone", name, diags)
		}
	}
}
