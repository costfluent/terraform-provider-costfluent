resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_folder" "reports" {
  workspace_id = costfluent_workspace.production.id
  title        = "Reports"
}

# A saved cost report: one window, up to three grouping slots, one filter, and the money settings
# the numbers are computed with.
resource "costfluent_cost_report" "monthly_by_service" {
  workspace_id = costfluent_workspace.production.id
  title        = "Monthly cost by service"

  date_interval = "ThisMonth"
  date_bin      = "Day"
  chart_type    = "StackedBar"

  # Service, then the team tag within each service. `group_by = []` saves a single total.
  group_by = [
    { dimension = "ServiceName" },
    { dimension = "Tag", key = "team" },
  ]

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
  group_by      = [{ dimension = "Region" }]

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

# The same kind of filter written as CQL, which also reaches what the JSON list cannot say: OR.
# The API stores it as JSON; the provider keeps this text while it means what is stored.
resource "costfluent_cost_report" "eu_compute_or_storage" {
  workspace_id = costfluent_workspace.production.id
  title        = "EU compute, or any storage"

  date_interval = "Last30Days"
  group_by      = [{ dimension = "ServiceName" }]

  filter = "(costs.service IN ('Amazon Elastic Compute Cloud', 'Amazon Relational Database Service') AND costs.region LIKE 'eu-%') OR costs.service = 'Amazon Simple Storage Service'"
}

# A segment's cost: what the allocation rules assign to it directly, plus its share of the shared
# cost bucket. Name segments by id, with = or IN, in one condition outside any OR.
resource "costfluent_segment" "checkout" {
  workspace_id = costfluent_workspace.production.id
  team_label   = "Checkout team"
  product      = "Checkout"
  cost_centre  = "CC-1001"
}

resource "costfluent_cost_report" "checkout_team" {
  workspace_id = costfluent_workspace.production.id
  title        = "Checkout team"

  date_interval = "LastMonth"
  group_by      = [{ dimension = "ServiceName" }]

  filter = "costs.segment = '${costfluent_segment.checkout.id}'"
}
