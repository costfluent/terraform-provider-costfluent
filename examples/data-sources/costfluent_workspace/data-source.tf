# Lookup workspace by name
data "costfluent_workspace" "production" {
  name = "Production"
}

# Use in other resources
resource "costfluent_budget" "prod_budget" {
  workspace_id = data.costfluent_workspace.production.id
  name         = "Production Budget"
  amount       = 50000
  currency     = data.costfluent_workspace.production.currency
  period       = "Monthly"
}

output "workspace_currency" {
  value = data.costfluent_workspace.production.currency
}
