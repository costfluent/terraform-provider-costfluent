# Query the last 30 days of cost, grouped by service.
data "costfluent_cost_data" "by_service" {
  date_range = {
    type   = "relative"
    period = "last_30_days"
  }

  group_by = ["service"]
  metrics  = ["billed_cost", "effective_cost"]
  limit    = 20
}

output "total_cost" {
  value = data.costfluent_cost_data.by_service.totals.billed_cost
}

output "top_services" {
  value = [for row in data.costfluent_cost_data.by_service.data : {
    service = row.dimensions["service"]
    cost    = row.billed_cost
  }]
}

# An absolute window instead of a preset, narrowed to one region.
data "costfluent_cost_data" "us_east_q1" {
  date_range = {
    type       = "absolute"
    start_date = "2026-01-01"
    end_date   = "2026-03-31"
  }

  filters = {
    region = "us-east-1"
  }

  group_by = ["service", "account"]
}

output "us_east_total" {
  value = data.costfluent_cost_data.us_east_q1.totals.effective_cost
}
