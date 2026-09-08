# Executive dashboard
resource "costfluent_dashboard" "executive" {
  name        = "Executive Overview"
  description = "High-level cost metrics for leadership"
  is_default  = true

  # Layout is a JSON array of widgets
  layout = jsonencode([
    {
      id       = "cost-trend"
      type     = "line_chart"
      title    = "Cost Trend"
      position = { x = 0, y = 0, w = 6, h = 4 }
      config = {
        metric      = "billed_cost"
        granularity = "daily"
      }
    },
    {
      id       = "top-services"
      type     = "bar_chart"
      title    = "Top Services"
      position = { x = 6, y = 0, w = 6, h = 4 }
      config = {
        group_by = "service"
        limit    = 10
      }
    }
  ])
}

# Team dashboard in folder
resource "costfluent_dashboard" "engineering" {
  name         = "Engineering Costs"
  folder_token = costfluent_folder.engineering.id
}
