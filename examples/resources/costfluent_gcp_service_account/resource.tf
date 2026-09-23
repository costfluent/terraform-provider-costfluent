# The service account Costfluent operates for your organization in GCP. Grant it BigQuery Data
# Viewer on your billing export dataset, then connect the billing account with a GCP
# costfluent_provider. The full chain, with the grant, is in the GCP cost-access module's README.
resource "costfluent_gcp_service_account" "this" {}

output "costfluent_service_account_email" {
  value = costfluent_gcp_service_account.this.email
}

# Allow these in a domain-restricted sharing policy, when your organization has one.
output "costfluent_google_organization_id" {
  value = costfluent_gcp_service_account.this.organization_id
}
