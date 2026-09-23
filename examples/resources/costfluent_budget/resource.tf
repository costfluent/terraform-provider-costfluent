resource "costfluent_budget" "monthly" {
  name     = "Monthly Cloud Budget"
  amount   = 10000
  currency = "EUR"
  period   = "Monthly"

  # Alerts are an ordered list of thresholds, as percentages of the amount.
  alerts = [
    { threshold_percent = 50 },
    { threshold_percent = 80 },
    { threshold_percent = 100 },
  ]
}

resource "costfluent_segment" "platform" {
  workspace_id = "wsp_abc123"
  team_label   = "Platform"
  product      = "Core API"
  cost_centre  = "CC-1001"
}

# A budget that tracks one allocation segment's cost.
resource "costfluent_budget" "platform" {
  workspace_id = "wsp_abc123"
  name         = "Platform quarterly"
  amount       = 25000
  currency     = "EUR"
  period       = "Quarterly"
  segment_id   = costfluent_segment.platform.id
}
