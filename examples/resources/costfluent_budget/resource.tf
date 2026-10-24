# The same amount every month from the start date; interval_count and interval_unit default to
# one month.
resource "costfluent_budget" "monthly" {
  name       = "Monthly Cloud Budget"
  amount     = "10000"
  currency   = "EUR"
  start_date = "2026-01-01"

  # Alert thresholds, as percentages of the current period's amount, listed lowest first. When
  # several are crossed at once, only the highest notifies.
  alerts = [
    { threshold_percent = 50 },
    { threshold_percent = 80 },
    { threshold_percent = 100 },
  ]

  # Notification channels every threshold delivers to, by ID.
  app_ids = ["app_AAAAAAAAAAAAAAAAAAAAAA"]
}

resource "costfluent_segment" "platform" {
  workspace_id = "wsp_abc123"
  team_label   = "Platform"
  product      = "Core API"
  cost_centre  = "CC-1001"
}

# A budget that tracks one allocation segment's cost, a quarter at a time, with an amount for each
# quarter. After the last one, the last amount carries on.
resource "costfluent_budget" "platform" {
  workspace_id   = "wsp_abc123"
  name           = "Platform quarterly"
  currency       = "EUR"
  start_date     = "2026-01-01"
  interval_count = 3
  interval_unit  = "month"
  segment_id     = costfluent_segment.platform.id

  periods = [
    { start_date = "2026-01-01", end_date = "2026-03-31", amount = "25000" },
    { start_date = "2026-04-01", end_date = "2026-06-30", amount = "27500" },
  ]
}

resource "costfluent_cost_report" "production_compute" {
  workspace_id = "wsp_abc123"
  title        = "Production compute"
  filter       = jsonencode([{ field = "ServiceName", operator = "In", values = ["Amazon EC2"] }])
}

# A budget that counts what one saved cost report counts: its filter, virtual tags and money
# settings. A report carries at most one cost budget and one usage budget.
resource "costfluent_budget" "production_compute" {
  workspace_id   = "wsp_abc123"
  name           = "Production compute"
  amount         = "8000"
  currency       = "EUR"
  cost_report_id = costfluent_cost_report.production_compute.id
}

resource "costfluent_cost_report" "ai_spend" {
  workspace_id = "wsp_abc123"
  title        = "AI spend"
}

# A usage budget counts one usage unit on a saved cost report's rows instead of money: five million
# tokens a month. Its amounts are quantities in usage_unit; currency is still required, and nothing
# the budget counts is in it.
resource "costfluent_budget" "ai_tokens" {
  workspace_id   = "wsp_abc123"
  name           = "AI tokens"
  measure        = "usage"
  usage_unit     = "Tokens"
  amount         = "5000000"
  currency       = "EUR"
  cost_report_id = costfluent_cost_report.ai_spend.id
}
