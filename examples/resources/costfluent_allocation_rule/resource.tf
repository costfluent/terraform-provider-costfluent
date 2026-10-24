resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_segment" "checkout_platform" {
  workspace_id = costfluent_workspace.production.id

  team_label  = "Checkout"
  product     = "Checkout"
  cost_centre = "CC-1001"
}

# Every EC2 cost row in Frankfurt belongs to the checkout segment. The filter is CQL text, or the
# cost filter's JSON document built with jsonencode, as costfluent_cost_report takes it.
resource "costfluent_allocation_rule" "checkout_compute" {
  workspace_id = costfluent_workspace.production.id
  name         = "Checkout compute in Frankfurt"
  priority     = 10

  filter = "costs.service = 'AmazonEC2' AND costs.region = 'eu-central-1'"

  segment_id = costfluent_segment.checkout_platform.id
}

# A lower-priority fallback: the rest of the cost in resource groups named checkout-*.
resource "costfluent_allocation_rule" "checkout_by_resource_group" {
  workspace_id = costfluent_workspace.production.id
  name         = "Checkout resource groups"
  priority     = 20

  filter = jsonencode([{ field = "ResourceGroup", operator = "StartsWith", values = ["checkout-"] }])

  segment_id = costfluent_segment.checkout_platform.id
}
