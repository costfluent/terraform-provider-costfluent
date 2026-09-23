# What an AWS role must trust before Costfluent can connect it. Pass both values to the
# costfluent/cost-access/aws module's costfluent_principal_arn and external_id inputs.
data "costfluent_aws_provider_info" "this" {}

output "costfluent_principal_arn" {
  value = data.costfluent_aws_provider_info.this.principal_arn
}

output "costfluent_external_id" {
  value     = data.costfluent_aws_provider_info.this.external_id
  sensitive = true
}
