resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "USD"
}

# A business metric; its daily values are imported by CSV in the console or through the API
resource "costfluent_business_metric" "customers" {
  workspace_id  = costfluent_workspace.production.id
  name          = "Active customers"
  unit          = "customer"
  label_columns = ["region"]
}

# A report charting cost per customer beside its cost
resource "costfluent_cost_report" "cost_per_customer" {
  workspace_id = costfluent_workspace.production.id
  title        = "Cost per customer"
  settings     = { unit_cost_metric_id = costfluent_business_metric.customers.id }
}
