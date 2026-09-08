# Costfluent Terraform Provider

Terraform provider for managing Costfluent cloud cost management resources.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.23 (for building)

## Installation

### From Terraform Registry (recommended)

```hcl
terraform {
  required_providers {
    costfluent = {
      source  = "costfluent/costfluent"
      version = ">= 1.0.0"
    }
  }
}
```

### Local Development

```bash
make install
```

## Configuration

```hcl
provider "costfluent" {
  api_key   = var.costfluent_api_key  # or COSTFLUENT_API_KEY env var
  workspace = "wsp_xxx"              # optional default workspace
  base_url  = "https://api.costfluent.com"  # optional
  timeout   = 30                     # optional, seconds
}
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `COSTFLUENT_API_KEY` | API key for authentication |
| `COSTFLUENT_WORKSPACE` | Default workspace token |
| `COSTFLUENT_BASE_URL` | API base URL override |

## Resources

| Resource | Description |
|----------|-------------|
| `costfluent_workspace` | Manage workspaces |
| `costfluent_provider` | Cloud provider connections (AWS, Azure, GCP, etc.) |
| `costfluent_folder` | Hierarchical organization folders |
| `costfluent_budget` | Cost budgets with alerts |
| `costfluent_cost_alert` | Threshold-based cost alerts |
| `costfluent_cost_report` | Saved cost reports |
| `costfluent_dashboard` | Custom dashboards |
| `costfluent_segment` | Segments that allocation rules claim cost for |
| `costfluent_saved_filter` | Reusable filter configurations |
| `costfluent_virtual_tag` | Virtual tag mapping rules |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `costfluent_workspace` | Lookup workspace by token or name |
| `costfluent_workspaces` | List all workspaces |
| `costfluent_provider` | Lookup provider by token or name |
| `costfluent_providers` | List all providers |
| `costfluent_anomalies` | List detected cost anomalies |
| `costfluent_cost_summary` | Current cost summary |
| `costfluent_cost_data` | Query cost data (may be slow) |

## Example Usage

### Basic Workspace Setup

```hcl
resource "costfluent_workspace" "production" {
  name        = "Production"
  description = "Production environment costs"
  currency    = "USD"
  timezone    = "America/New_York"
}

resource "costfluent_provider" "aws" {
  key  = "aws"
  name = "Production AWS"
  credentials = {
    role_arn = "arn:aws:iam::123456789:role/CostfluentRole"
  }
}
```

### Budget with Alerts

```hcl
resource "costfluent_budget" "monthly" {
  workspace_id = costfluent_workspace.production.id
  name         = "Monthly Budget"
  amount       = 10000
  currency     = "USD"
  period       = "monthly"

  alerts {
    threshold_percent = 80
    channels          = ["chn_slack"]
  }
  alerts {
    threshold_percent = 100
    channels          = ["chn_slack", "chn_email"]
  }
}
```

### Cost Data Query

```hcl
data "costfluent_cost_data" "last_month" {
  date_range {
    type   = "relative"
    period = "last_30_days"
  }
  group_by = ["service", "region"]
  limit    = 100
}

output "top_services" {
  value = data.costfluent_cost_data.last_month.data
}
```

## Development

```bash
# Build
make build

# Run tests
make test

# Run acceptance tests (requires COSTFLUENT_API_KEY)
make testacc

# Generate documentation
make docs

# Lint
make lint
```

## License

MIT
