variable "azure_tenant_id" {
  type = string
}

variable "azure_client_id" {
  type = string
}

variable "azure_client_secret" {
  type      = string
  sensitive = true
}

# The role must trust data.costfluent_aws_provider_info's principal_arn with its external_id;
# Costfluent adds the external ID itself, so the credentials carry only the role.
resource "costfluent_provider" "aws_main" {
  key  = "aws"
  name = "AWS Production"
  credentials = {
    role_arn = "arn:aws:iam::123456789012:role/CostfluentBillingRole"
  }
  settings = {
    export_bucket        = "acme-costfluent-export"
    export_bucket_region = "us-east-1"
  }
}

resource "costfluent_provider" "azure" {
  key  = "azure"
  name = "Azure Production"
  credentials = {
    tenant   = var.azure_tenant_id
    appId    = var.azure_client_id
    password = var.azure_client_secret
  }
}
