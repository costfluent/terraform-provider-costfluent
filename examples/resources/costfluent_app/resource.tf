# An email channel; several recipients are separated by commas
resource "costfluent_app" "finance" {
  key  = "email"
  name = "Finance"
  config = jsonencode({
    recipients = "finance@example.com, cfo@example.com"
  })
}

# A Slack channel. The API never returns the webhook URL, so keep it in a variable
variable "slack_webhook_url" {
  type      = string
  sensitive = true
}

resource "costfluent_app" "ops" {
  key  = "slack"
  name = "Ops alerts"
  config = jsonencode({
    webhookUrl = var.slack_webhook_url
  })
}
