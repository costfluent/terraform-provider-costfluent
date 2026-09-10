# Current month against the previous comparable period.
data "costfluent_cost_summary" "current" {
  period = "this_month"
}

output "total_cost" {
  value = data.costfluent_cost_summary.current.total_cost
}

output "cost_change_percent" {
  value = data.costfluent_cost_summary.current.change_percent
}

# Summary for one workspace rather than the provider's default.
data "costfluent_workspace" "prod" {
  name = "Production"
}

data "costfluent_cost_summary" "production" {
  workspace_id = data.costfluent_workspace.prod.id
  period       = "last_30_days"
}

output "production_forecast" {
  value = data.costfluent_cost_summary.production.forecast
}
