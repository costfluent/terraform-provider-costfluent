# Its day and budget periods begin at midnight in Berlin, and a new cost report
# starts on last month by week, unamortised, unless it says otherwise.
resource "costfluent_workspace" "production" {
  name      = "Production"
  currency  = "EUR"
  time_zone = "Europe/Berlin"

  report_defaults = {
    date_interval = "lastMonth"
    date_bin      = "week"
    amortize      = false
  }
}

# Costs billed in other currencies are converted into GBP at each month's average rate.
resource "costfluent_workspace" "uk" {
  name                       = "UK"
  currency                   = "GBP"
  enable_currency_conversion = true
  conversion_currency        = "GBP"
  conversion_method          = "monthlyAverage"
  time_zone                  = "Europe/London"
}
