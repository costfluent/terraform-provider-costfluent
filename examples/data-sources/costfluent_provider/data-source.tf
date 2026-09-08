# Lookup provider by name
data "costfluent_provider" "aws_main" {
  name = "Production AWS"
}

# Use provider token in budget filters
resource "costfluent_budget" "aws_only" {
  name   = "AWS Budget"
  amount = 10000
  period = "monthly"

  filters {
    provider_tokens = [data.costfluent_provider.aws_main.id]
  }
}

output "provider_status" {
  value = data.costfluent_provider.aws_main.status
}

output "last_sync" {
  value = data.costfluent_provider.aws_main.last_sync_at
}
