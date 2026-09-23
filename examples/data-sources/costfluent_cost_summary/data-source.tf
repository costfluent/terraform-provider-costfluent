# September against the 30 days before it.
data "costfluent_cost_summary" "september" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
}

output "total_cost" {
  value = data.costfluent_cost_summary.september.total_cost
}

output "cost_change_percent" {
  value = data.costfluent_cost_summary.september.cost_change_percent
}

# Summary for one workspace rather than the provider's default.
data "costfluent_workspace" "prod" {
  name = "Production"
}

data "costfluent_cost_summary" "production" {
  workspace_id = data.costfluent_workspace.prod.id
  start_date   = "2026-09-01"
  end_date     = "2026-09-30"
}

output "production_amortized" {
  value = data.costfluent_cost_summary.production.total_amortized_cost
}
