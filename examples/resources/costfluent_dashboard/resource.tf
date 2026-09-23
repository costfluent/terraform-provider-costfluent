# Default dashboard for the workspace
resource "costfluent_dashboard" "executive" {
  title         = "Executive Overview"
  is_default    = true
  date_interval = "thisMonth"
  date_bin      = "day"
}

# Quarterly view, binned by week
resource "costfluent_dashboard" "engineering" {
  title         = "Engineering Costs"
  date_interval = "lastQuarter"
  date_bin      = "week"
}
