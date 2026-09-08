# Query last 30 days of cost data grouped by service
data "costfluent_cost_data" "by_service" {
  date_range {
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

# Filtered query for specific region
data "costfluent_cost_data" "us_east" {
  date_range {
    type   = "relative"
    period = "this_month"
  }

  filters = {
    region = "us-east-1"
  }

  group_by = ["service", "account"]
}

output "us_east_total" {
  value = data.costfluent_cost_data.us_east.totals.effective_cost
}
