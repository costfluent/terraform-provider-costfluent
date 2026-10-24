# A virtual tag gives every cost row a value for one key: the first provider tag key the row
# carries, then the first rule whose filter matches, then the default. It is evaluated when cost is
# read, so reports use an edit straight away. Each filter is the cost filter JSON document, built
# with jsonencode, as costfluent_cost_report takes it.
resource "costfluent_virtual_tag" "team" {
  key         = "team"
  description = "Owning team for every cost line"

  # Provider tags spelled two ways are one tag.
  collapsed_tag_keys = ["team", "Team"]

  rule {
    value = "platform"
    filter = jsonencode([
      { field = "ServiceName", operator = "In", values = ["Amazon EKS", "Amazon EC2"] },
    ])
  }

  rule {
    value = "data"
    filter = jsonencode([
      { field = "Tag", tagKey = "app", operator = "StartsWith", values = ["warehouse-"] },
    ])
  }

  # Everything no provider tag or rule claimed.
  default_value = "unowned"
}
