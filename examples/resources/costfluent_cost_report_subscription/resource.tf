resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_cost_report" "monthly" {
  workspace_id  = costfluent_workspace.production.id
  title         = "Monthly spend"
  date_interval = "last30Days"
}

resource "costfluent_app" "finance" {
  key    = "email"
  name   = "Finance"
  config = jsonencode({ recipients = "finance@example.com" })
}

# Weekly, at 8 o'clock Berlin time
resource "costfluent_cost_report_subscription" "weekly" {
  workspace_id   = costfluent_workspace.production.id
  cost_report_id = costfluent_cost_report.monthly.id
  name           = "Weekly spend"
  schedule_type  = "weekly"
  delivery_hour  = 8
  time_zone      = "Europe/Berlin"
  app_ids        = [costfluent_app.finance.id]
}
