resource "costfluent_team" "platform" {
  name = "Platform"
}

resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

resource "costfluent_dashboard" "platform_costs" {
  workspace_id = costfluent_workspace.production.id
  title        = "Platform costs"
}

# The platform team's members can view and change everything in the production workspace, and open
# on the platform costs dashboard after signing in unless they chose their own
resource "costfluent_team_workspace_access" "platform_production" {
  team_id              = costfluent_team.platform.id
  workspace_id         = costfluent_workspace.production.id
  access_level         = "canEdit"
  default_dashboard_id = costfluent_dashboard.platform_costs.id
}
