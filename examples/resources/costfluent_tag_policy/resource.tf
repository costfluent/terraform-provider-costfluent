resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

# The tag keys the production workspace's cost must carry. Rules add up: a cost row must carry the
# required keys of every rule that applies to it. Costfluent measures cost against the policy and
# changes nothing in the cloud. A workspace has one policy, so manage it from one resource only.
resource "costfluent_tag_policy" "production" {
  workspace_id = costfluent_workspace.production.id

  # Every provider and every account.
  rule {
    required_keys = ["team"]
  }

  # Two AWS accounts also need an environment and a cost center.
  rule {
    provider      = "aws"
    account_ids   = ["123456789012", "210987654321"]
    required_keys = ["env", "cost-center"]
  }
}

# Saving the first rules creates a cost report of the cost missing a required key, by account.
output "untagged_cost_report_id" {
  value = costfluent_tag_policy.production.report_id
}
