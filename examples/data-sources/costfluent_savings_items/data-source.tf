# Findings to review in the provider's workspace estimated to save at least 100 a month, largest
# first.
data "costfluent_savings_items" "to_review" {
  tab                    = "toReview"
  minimum_monthly_saving = "100"
  sort                   = "largestSaving"
}

output "largest_finding" {
  value = try(data.costfluent_savings_items.to_review.savings_items[0].title, null)
}

# Rightsizing findings one connection's provider generated, in another workspace.
data "costfluent_savings_items" "rightsizing" {
  workspace_id     = "wsp_0000000000000000000001"
  kind             = "rightsizing"
  cloud_account_id = "prv_0000000000000000000001"
}

output "rightsizing_monthly_savings" {
  value = [for item in data.costfluent_savings_items.rightsizing.savings_items : "${item.estimated_monthly_saving} ${item.currency}"]
}
