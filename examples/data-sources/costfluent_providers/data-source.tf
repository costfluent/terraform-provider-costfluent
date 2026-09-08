# List all configured providers
data "costfluent_providers" "all" {}

output "provider_count" {
  value = length(data.costfluent_providers.all.providers)
}

output "provider_names" {
  value = [for p in data.costfluent_providers.all.providers : p.name]
}

# Filter to find AWS providers
output "aws_providers" {
  value = [for p in data.costfluent_providers.all.providers : p if p.key == "aws"]
}
