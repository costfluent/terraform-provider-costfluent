resource "costfluent_virtual_tag" "cost_center" {
  name        = "CostCenter"
  description = "Virtual cost center assignment"

  rules {
    condition = "tag:Team == 'Platform'"
    value     = "CC-1001"
    priority  = 100
  }

  rules {
    condition = "tag:Team == 'Frontend'"
    value     = "CC-1002"
    priority  = 100
  }

  rules {
    condition = "service contains 'RDS'"
    value     = "CC-2001"
    priority  = 50
  }

  rules {
    condition = "true"
    value     = "CC-9999"
    priority  = 0
  }
}
