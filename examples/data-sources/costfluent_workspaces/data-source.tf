data "costfluent_workspaces" "all" {}

output "workspace_names" {
  value = [for ws in data.costfluent_workspaces.all.workspaces : ws.name]
}

# Lookup single workspace by name
data "costfluent_workspace" "prod" {
  name = "Production"
}

output "prod_workspace_id" {
  value = data.costfluent_workspace.prod.id
}
