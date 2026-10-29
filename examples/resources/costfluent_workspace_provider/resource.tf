resource "costfluent_workspace" "payments" {
  name     = "Payments"
  currency = "USD"
}

resource "costfluent_provider" "aws_payer" {
  key          = "aws"
  display_name = "AWS Organization"
  credentials = {
    role_arn = "arn:aws:iam::123456789012:role/CostfluentBillingRole"
  }
  settings = {
    export_bucket        = "acme-costfluent-export"
    export_bucket_region = "us-east-1"
  }
}

# The payments workspace reads only the payments team's member accounts of the organization.
# Other workspaces can enable the same connection for their own accounts.
resource "costfluent_workspace_provider" "payments_aws" {
  workspace_id = costfluent_workspace.payments.id
  provider_id  = costfluent_provider.aws_payer.id
  account_ids  = ["210987654321", "345678901234"]
}

# Without account_ids the workspace reads the whole connection, including accounts discovered later.
resource "costfluent_workspace_provider" "finance_aws" {
  workspace_id = "wsp_0000000000000000000002"
  provider_id  = costfluent_provider.aws_payer.id
}
