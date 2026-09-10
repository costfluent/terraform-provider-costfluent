terraform {
  required_providers {
    costfluent = {
      source  = "costfluent/costfluent"
      version = ">= 0.1.0"
    }
  }
}

provider "costfluent" {
  # api_key = var.costfluent_api_key  # Or set COSTFLUENT_API_KEY
  # workspace = "wsp_default"        # Optional default workspace
}
