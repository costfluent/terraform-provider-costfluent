resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_folder" "reports" {
  name = "Reports"
}

# A saved cost report: one window, one grouping dimension, one filter, and the money settings the
# numbers are computed with.
resource "costfluent_cost_report" "monthly_by_service" {
  workspace_id = costfluent_workspace.production.id
  title        = "Monthly cost by service"

  date_interval = "ThisMonth"
  date_bin      = "Day"
  chart_type    = "StackedBar"
  group_by      = "ServiceName"

  settings = {
    amortize               = true
    include_credits        = false
    include_refunds        = true
    include_tax            = true
    show_forecast          = false
    compare_to_last_period = true
  }
}

# The filter travels as the same JSON document the app puts in its URL, so a report saved here and
# one saved in the app are the same definition.
resource "costfluent_cost_report" "q4_eu_compute" {
  workspace_id = costfluent_workspace.production.id
  title        = "Q4 EU compute"
  folder_id    = costfluent_folder.reports.id

  date_interval = "Custom"
  start_date    = "2026-10-01"
  end_date      = "2026-12-31"
  date_bin      = "Month"
  group_by      = "Region"

  filter = jsonencode([
    {
      field    = "ServiceName"
      operator = "In"
      values   = ["AmazonEC2", "AmazonRDS"]
    },
    {
      field    = "Region"
      operator = "StartsWith"
      values   = ["eu-"]
    }
  ])
}
