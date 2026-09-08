# Get current month cost summary
data "costfluent_cost_summary" "current" {
  date_range = "this_month"
}

output "total_cost" {
  value = data.costfluent_cost_summary.current.total_cost
}

output "cost_change_percent" {
  value = data.costfluent_cost_summary.current.change_percent
}

# Workspace-specific summary
data "costfluent_cost_summary" "production" {
  workspace_id = data.costfluent_workspace.prod.id
  date_range   = "last_30_days"
}
