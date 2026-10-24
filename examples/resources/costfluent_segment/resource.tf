resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

# A segment is what an allocation rule claims cost for: a team, a product and a cost centre.
# The matching lives on ordered allocation rules, not here.
resource "costfluent_segment" "checkout_platform" {
  workspace_id = costfluent_workspace.production.id

  # Name the team row when it exists, or a label when it does not — one or the other, never both.
  team_id     = "team_0f8c2d71b4e6a93c5d27e1"
  product     = "Checkout"
  cost_centre = "CC-1001"
}

resource "costfluent_segment" "data_platform" {
  workspace_id = costfluent_workspace.production.id

  team_label  = "Data Platform"
  product     = "Ingestion"
  cost_centre = "CC-1002"
}

# The workspace's unlabelled shared-cost bucket. It names no team, product or cost centre: its cost
# is spread across direct segments in proportion to their direct cost.
resource "costfluent_segment" "shared" {
  workspace_id = costfluent_workspace.production.id
  is_shared    = true
}

# A labelled shared pool split by fixed percents, which sum to exactly 100. The last target takes
# the rounding residual, so order matters. method = "even" gives each target the same share and
# takes no percents. Allocation is evaluated at query time, so changing a split restates every past
# month.
resource "costfluent_segment" "cluster" {
  workspace_id = costfluent_workspace.production.id
  is_shared    = true
  label        = "Kubernetes cluster"

  split = {
    method = "percent"
    targets = [
      { segment_id = costfluent_segment.checkout_platform.id, percent = 60 },
      { segment_id = costfluent_segment.data_platform.id, percent = 40 },
    ]
  }
}
