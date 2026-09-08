resource "costfluent_budget" "monthly" {
  name       = "Monthly Cloud Budget"
  amount     = 10000
  period     = "monthly"
  start_date = "2024-01-01"

  filters {
    services = ["Amazon EC2", "Amazon S3"]
    regions  = ["us-east-1", "us-west-2"]
  }

  alerts {
    threshold_percent = 50
    channels          = ["slack_alerts"]
  }

  alerts {
    threshold_percent = 80
    channels          = ["slack_alerts", "email_team"]
  }

  alerts {
    threshold_percent = 100
    channels          = ["slack_alerts", "email_team", "pagerduty"]
  }
}

resource "costfluent_budget" "quarterly" {
  name       = "Q1 Budget"
  amount     = 25000
  period     = "quarterly"
  start_date = "2024-01-01"
  end_date   = "2024-03-31"
}
