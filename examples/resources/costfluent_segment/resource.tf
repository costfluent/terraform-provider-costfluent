resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

# A segment is what an allocation rule claims cost for: a team, a product and a cost centre.
# The matching lives on ordered allocation rules, not here.
resource "costfluent_segment" "checkout_platform" {
  workspace_id = costfluent_workspace.production.id

  # Name the team row when it exists, or a label when it does not — one or the other, never both.
  team_id     = "tem_7f3a9c21"
  product     = "Checkout"
  cost_centre = "CC-1001"
}

resource "costfluent_segment" "data_platform" {
  workspace_id = costfluent_workspace.production.id

  team_label  = "Data Platform"
  product     = "Ingestion"
  cost_centre = "CC-1002"
}

# The shared-cost bucket. Its cost is spread across direct segments in proportion to what they own,
# rather than being charged to whatever it names.
resource "costfluent_segment" "shared" {
  workspace_id = costfluent_workspace.production.id
  is_shared    = true

  team_label  = "Shared"
  product     = "Platform"
  cost_centre = "CC-0000"
}
