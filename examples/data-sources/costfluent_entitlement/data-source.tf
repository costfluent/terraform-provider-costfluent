# The plan the account is on and the limits it carries.
data "costfluent_entitlement" "current" {}

output "plan" {
  value = data.costfluent_entitlement.current.plan_name
}

# Unset means unlimited, so compare with care.
output "connections_allowed" {
  value = data.costfluent_entitlement.current.max_connections
}
