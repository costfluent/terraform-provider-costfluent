resource "costfluent_workspace" "production" {
  name        = "Production"
  description = "Production environment cost tracking"
  currency    = "USD"
  timezone    = "America/New_York"
}

resource "costfluent_workspace" "staging" {
  name     = "Staging"
  currency = "USD"
  timezone = "UTC"
}
