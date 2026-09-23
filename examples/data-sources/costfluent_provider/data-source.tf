# Lookup provider by name
data "costfluent_provider" "aws_main" {
  name = "Production AWS"
}

# Alert on that one connection's cost.
resource "costfluent_cost_alert" "aws_only" {
  name            = "AWS daily spend"
  threshold_type  = "absolute"
  threshold_value = 500
  provider_ids    = [data.costfluent_provider.aws_main.id]
}

output "provider_status" {
  value = data.costfluent_provider.aws_main.status
}

output "last_sync" {
  value = data.costfluent_provider.aws_main.last_sync_at
}
