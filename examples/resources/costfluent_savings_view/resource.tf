# Rightsizing findings waiting for review that save at least 100 a month, in the provider's
# workspace.
resource "costfluent_savings_view" "rightsizing" {
  title                  = "Rightsizing over 100"
  tab                    = "toReview"
  kind                   = "rightsizing"
  minimum_monthly_saving = "100"
}

# Findings one connection's provider generated, while they are being worked on.
resource "costfluent_savings_view" "aws_in_progress" {
  workspace_id     = "wsp_0000000000000000000001"
  title            = "AWS in progress"
  tab              = "inProgress"
  source           = "awsCostOptimizationHub"
  cloud_account_id = "prv_0000000000000000000001"
}
