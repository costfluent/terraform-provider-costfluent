resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_dashboard" "engineering" {
  workspace_id = costfluent_workspace.production.id
  title        = "Engineering Costs"
}

resource "costfluent_cost_report" "by_service" {
  workspace_id = costfluent_workspace.production.id
  title        = "Spend by service"
}

# A saved cost report drawn as a chart (the default), a table or a KPI
resource "costfluent_dashboard_widget" "spend" {
  workspace_id   = costfluent_workspace.production.id
  dashboard_id   = costfluent_dashboard.engineering.id
  cost_report_id = costfluent_cost_report.by_service.id
  title          = "Spend by service"
  display        = "table"
  position       = { x = 0, y = 0, w = 8, h = 4 }
}

# Words beside the numbers, rendered as markdown emphasis without HTML
resource "costfluent_dashboard_widget" "notes" {
  workspace_id = costfluent_workspace.production.id
  dashboard_id = costfluent_dashboard.engineering.id
  type         = "text"
  title        = "Notes"
  content      = "Compute is *up* after the migration; storage should fall next month."
  position     = { x = 8, y = 0, w = 4, h = 4 }
}
