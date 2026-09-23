# A virtual tag derives a tag value for cost rows that never carried one. The rule set is a JSON
# document, so it is built with jsonencode.
resource "costfluent_virtual_tag" "cost_center" {
  key              = "cost_center"
  description      = "Virtual cost center assignment"
  computation_mode = "precompute"

  rules = jsonencode([
    { field = "tag:Team", operator = "equals", value = "Platform", result = "CC-1001" },
    { field = "tag:Team", operator = "equals", value = "Frontend", result = "CC-1002" },
    { field = "service_name", operator = "contains", value = "RDS", result = "CC-2001" },
  ])

  # Everything no rule claimed.
  default_value = "CC-9999"
}
