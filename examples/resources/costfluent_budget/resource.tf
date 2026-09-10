resource "costfluent_budget" "monthly" {
  name       = "Monthly Cloud Budget"
  amount     = 10000
  currency   = "USD"
  period     = "monthly"
  start_date = "2026-01-01"

  filters = {
    services = ["Amazon EC2", "Amazon S3"]
    regions  = ["us-east-1", "us-west-2"]
  }

  # Alerts are an ordered list, not repeated blocks. Each threshold notifies its own channels, so
  # an early warning can go somewhere quieter than the one that means the budget is spent.
  alerts = [
    {
      threshold_percent = 50
      channels          = ["chn_slack_alerts"]
    },
    {
      threshold_percent = 80
      channels          = ["chn_slack_alerts", "chn_email_team"]
    },
    {
      threshold_percent = 100
      channels          = ["chn_slack_alerts", "chn_email_team", "chn_pagerduty"]
    },
  ]
}

resource "costfluent_budget" "quarterly" {
  name       = "Q1 Budget"
  amount     = 25000
  period     = "quarterly"
  start_date = "2026-01-01"
  end_date   = "2026-03-31"
}
