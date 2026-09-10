# Lookup workspace by name
data "costfluent_workspace" "production" {
  name = "Production"
}

# Use in other resources
resource "costfluent_budget" "prod_budget" {
  workspace_id = data.costfluent_workspace.production.id
  name         = "Production Budget"
  amount       = 50000
  currency     = "USD"
  period       = "monthly"
  start_date   = "2026-01-01"
}

output "workspace_timezone" {
  value = data.costfluent_workspace.production.timezone
}
