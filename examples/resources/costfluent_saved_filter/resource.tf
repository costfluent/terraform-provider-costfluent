# Production environment filter
resource "costfluent_saved_filter" "production" {
  title  = "Production Only"
  filter = "tag:Environment = 'production'"
}

# AWS compute filter, the workspace's default
resource "costfluent_saved_filter" "aws_compute" {
  title      = "AWS Compute"
  filter     = "provider = 'aws' AND service = 'Amazon EC2'"
  is_default = true
}
