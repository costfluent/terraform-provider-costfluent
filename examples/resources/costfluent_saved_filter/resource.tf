# Production environment filter
resource "costfluent_saved_filter" "production" {
  name        = "Production Only"
  description = "Filter for production environment resources"

  filters = {
    "tag:Environment" = "production"
  }
}

# AWS compute filter
resource "costfluent_saved_filter" "aws_compute" {
  name = "AWS Compute"

  filters = {
    "provider" = "aws"
    "service"  = "Amazon EC2"
  }
}
