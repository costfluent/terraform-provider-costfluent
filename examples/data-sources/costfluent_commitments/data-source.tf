# Commitments held through the provider's workspace's connections that end soon.
data "costfluent_commitments" "expiring" {
  status = "expiring"
}

output "expiring_commitments" {
  value = [for c in data.costfluent_commitments.expiring.commitments : "${c.type} ${c.id} ends ${c.ends_at}"]
}

# Three-year AWS Savings Plans, with how much of each went unused.
data "costfluent_commitments" "aws_savings_plans" {
  provider_key = "aws"
  type         = "SavingsPlan"
  term_years   = 3
}

output "savings_plan_utilization" {
  value = { for c in data.costfluent_commitments.aws_savings_plans.commitments : c.id => c.utilization_percentage }
}
