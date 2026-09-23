# Absolute threshold on one provider's cost
resource "costfluent_cost_alert" "high_spend" {
  name            = "High Daily Spend"
  threshold_type  = "absolute"
  threshold_value = 1000
  provider_ids    = ["prv_abc123"]
}

# Growth against the previous week, narrowed by a filter, evaluated every 6 hours (paused)
resource "costfluent_cost_alert" "ec2_growth" {
  name                         = "EC2 week-over-week growth"
  threshold_type               = "percentageIncrease"
  threshold_value              = 20
  comparison_period            = "previousWeek"
  filter                       = "service = 'Amazon EC2'"
  evaluation_frequency_minutes = 360
  is_paused                    = true
}
