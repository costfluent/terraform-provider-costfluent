# Daily cost for September, grouped by service and then by the team tag.
data "costfluent_cost_data" "by_service" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  group_by   = ["ServiceName", "Tag:team"]
  limit      = 100
}

output "total_cost" {
  value = data.costfluent_cost_data.by_service.total_cost
}

output "daily_service_cost" {
  value = [for row in data.costfluent_cost_data.by_service.data : {
    date    = row.date
    service = row.dimensions["ServiceName"]
    team    = row.dimensions["Tag:team"]
    cost    = row.cost
  }]
}

# Monthly cost for the first quarter, narrowed to one region.
data "costfluent_cost_data" "eu_west_q1" {
  start_date  = "2026-01-01"
  end_date    = "2026-03-31"
  granularity = "month"
  filter      = "region = 'eu-west-1'"
}

output "eu_west_total" {
  value = data.costfluent_cost_data.eu_west_q1.total_cost
}
