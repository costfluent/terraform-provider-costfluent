package datasources

import (
	"github.com/costfluent/terraform-provider-costfluent/internal/costfluent"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// workspaceOf is the workspace a workspace-scoped read names: workspace_id, or the provider's
// workspace when it is unset. With neither it adds the error that says what to set, and is empty.
func workspaceOf(client *costfluent.Client, configured types.String, what string, diags *diag.Diagnostics) string {
	workspace := client.ResolveWorkspace(configured.ValueString())
	if workspace == "" {
		diags.AddAttributeError(path.Root("workspace_id"), "No workspace for the "+what,
			"Set workspace_id, or the provider's workspace, to the workspace whose "+what+" to read.")
	}
	return workspace
}
