# Threshold alert
resource "costfluent_cost_alert" "high_spend" {
  name            = "High Daily Spend"
  description     = "Alert when daily spend exceeds $1000"
  type            = "threshold"
  metric          = "billed_cost"
  operator        = "gt"
  threshold_value = 1000
  period          = "daily"
  channels        = ["chn_slack_alerts"]
}

# Anomaly alert (paused)
resource "costfluent_cost_alert" "anomaly" {
  name            = "Cost Anomaly Detection"
  type            = "anomaly"
  metric          = "effective_cost"
  operator        = "gt"
  threshold_value = 20 # 20% deviation
  is_paused       = true
}
