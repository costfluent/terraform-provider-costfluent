resource "costfluent_provider" "aws_main" {
  key  = "aws"
  name = "AWS Production"
  credentials = {
    role_arn = "arn:aws:iam::123456789012:role/CostfluentRole"
  }
  settings = {
    regions = "us-east-1,us-west-2"
  }
}

resource "costfluent_provider" "azure" {
  key  = "azure"
  name = "Azure Production"
  credentials = {
    tenant_id     = var.azure_tenant_id
    client_id     = var.azure_client_id
    client_secret = var.azure_client_secret
  }
}
