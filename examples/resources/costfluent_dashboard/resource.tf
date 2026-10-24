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

# Every widget on this dashboard reads its report over August, week by week
resource "costfluent_dashboard" "august" {
  title         = "August Review"
  date_interval = "custom"
  start_date    = "2026-08-01"
  end_date      = "2026-08-31"
  date_bin      = "week"
}

# Every report widget on this dashboard is read through the production filter
resource "costfluent_saved_filter" "production" {
  title  = "Production Only"
  filter = "tags->>'Environment' = 'production'"
}

resource "costfluent_dashboard" "production" {
  title           = "Production Costs"
  saved_filter_id = costfluent_saved_filter.production.id
}
