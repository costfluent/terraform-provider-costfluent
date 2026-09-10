# A virtual tag derives a tag value from cost rows that never carried one. Rules are an ordered
# list and the highest priority that matches wins, so the catch-all sits at the bottom.
resource "costfluent_virtual_tag" "cost_center" {
  key         = "cost_center"
  name        = "CostCenter"
  description = "Virtual cost center assignment"

  rules = [
    {
      condition = {
        field    = "tag:Team"
        operator = "equals"
        value    = "Platform"
      }
      value    = "CC-1001"
      priority = 100
    },
    {
      condition = {
        field    = "tag:Team"
        operator = "equals"
        value    = "Frontend"
      }
      value    = "CC-1002"
      priority = 90
    },
    {
      condition = {
        field    = "service_name"
        operator = "contains"
        value    = "RDS"
      }
      value    = "CC-2001"
      priority = 50
    },
  ]

  # Everything no rule claimed.
  default_value = "CC-9999"
}
