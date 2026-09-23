resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "EUR"
}

# Costs billed in other currencies are converted into GBP at each month's average rate.
resource "costfluent_workspace" "uk" {
  name                       = "UK"
  currency                   = "GBP"
  enable_currency_conversion = true
  conversion_currency        = "GBP"
  conversion_method          = "monthlyAverage"
}
