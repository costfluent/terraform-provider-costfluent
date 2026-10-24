# A saved filter written as CQL text. The API stores and returns it as its JSON document; the
# configured text stays in state while it means the stored filter.
resource "costfluent_saved_filter" "production" {
  title  = "Production Only"
  filter = "tags->>'Environment' = 'production'"
}

# AWS compute filter, the workspace's default, written as the JSON document
resource "costfluent_saved_filter" "aws_compute" {
  title = "AWS Compute"
  filter = jsonencode([
    { field = "Provider", operator = "In", values = ["aws"] },
    { field = "ServiceName", operator = "In", values = ["Amazon Elastic Compute Cloud"] },
  ])
  is_default = true
}
