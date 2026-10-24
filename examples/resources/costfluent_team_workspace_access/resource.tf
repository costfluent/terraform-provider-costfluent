resource "costfluent_team" "platform" {
  name = "Platform"
}

resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

# The platform team's members can view and change everything in the production workspace
resource "costfluent_team_workspace_access" "platform_production" {
  team_id      = costfluent_team.platform.id
  workspace_id = costfluent_workspace.production.id
  access_level = "canEdit"
}
