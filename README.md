# Costfluent Terraform Provider

Terraform provider for managing Costfluent cloud cost management resources.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.24 (for building)

## Installation

### From Terraform Registry (recommended)

```hcl
terraform {
  required_providers {
    costfluent = {
      source  = "costfluent/costfluent"
      version = "~> 0.3.0"
    }
  }
}
```

Until 1.0, a minor version may need changes to your configuration, so pin the minor version as
above: `~> 0.3.0` takes patch versions only, and a patch version never does. Each minor version
that needs changes has an upgrade guide; for 0.2.x to 0.3.0, see
[Upgrading from 0.2](docs/guides/upgrade-from-0.2.md).

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
| `COSTFLUENT_API_KEY` | API key for authentication: an organization token (`cf_org_`), or a workspace token (`cf_ws_`) whose list data sources return its workspace alone; organization-level resources need an organization token |
| `COSTFLUENT_WORKSPACE` | Default workspace ID for workspace-scoped resources |
| `COSTFLUENT_BASE_URL` | API base URL override |

## Resources

| Resource | Description |
|----------|-------------|
| `costfluent_workspace` | Manage workspaces |
| `costfluent_provider` | Cloud provider connections (AWS, Azure, GCP, etc.) |
| `costfluent_workspace_provider` | A connection enabled in a workspace, for the whole connection or chosen accounts |
| `costfluent_gcp_service_account` | The service account Costfluent operates for your organization in GCP; grant it BigQuery Data Viewer on your billing export |
| `costfluent_folder` | Hierarchical organization folders |
| `costfluent_budget` | Cost budgets with alerts |
| `costfluent_cost_alert` | Threshold-based cost alerts |
| `costfluent_cost_report` | Saved cost reports, optionally charting cost per unit of a business metric |
| `costfluent_business_metric` | Business metrics (customers, transactions) a report divides cost by |
| `costfluent_dashboard` | Custom dashboards, with an optional saved filter every report widget is read through |
| `costfluent_dashboard_widget` | A dashboard widget: a cost report as a chart, table or KPI, or text |
| `costfluent_segment` | Segments that allocation rules claim cost for |
| `costfluent_allocation_rule` | Ordered rules that assign matching cost to a segment |
| `costfluent_saved_filter` | Reusable filter configurations |
| `costfluent_virtual_tag` | Virtual tag mapping rules |
| `costfluent_tag_policy` | A workspace's tag policy: the required keys its cost must carry |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `costfluent_workspace` | Lookup workspace by ID or name |
| `costfluent_workspaces` | List all workspaces |
| `costfluent_provider` | Lookup provider by ID or name |
| `costfluent_providers` | List all providers |
| `costfluent_anomalies` | List detected cost anomalies |
| `costfluent_cost_summary` | Cost summary for a window |
| `costfluent_cost_data` | Query cost data (may be slow) |
| `costfluent_entitlement` | The account's plan and the limits it carries |
| `costfluent_aws_provider_info` | The principal and external ID an AWS role must trust, for the `costfluent/cost-access/aws` module |

## Example Usage

### Basic Workspace Setup

```hcl
resource "costfluent_workspace" "production" {
  name     = "Production"
  currency = "EUR"
}

data "costfluent_aws_provider_info" "this" {}

module "costfluent_aws" {
  source  = "costfluent/cost-access/aws"
  version = "~> 0.2"

  costfluent_principal_arn = data.costfluent_aws_provider_info.this.principal_arn
  external_id              = data.costfluent_aws_provider_info.this.external_id
  cost_export_bucket_name  = "acme-costfluent-export"
}

resource "costfluent_provider" "aws" {
  key         = "aws"
  name        = "Production AWS"
  credentials = module.costfluent_aws.credentials
  settings    = module.costfluent_aws.settings
}
```

### Budget with Alerts

```hcl
resource "costfluent_budget" "monthly" {
  workspace_id = costfluent_workspace.production.id
  name         = "Monthly Budget"
  amount       = "10000"
  currency     = "EUR"
  period       = "Monthly"

  alerts = [
    { threshold_percent = 80 },
    { threshold_percent = 100 },
  ]
}
```

### Cost Data Query

```hcl
data "costfluent_cost_data" "september" {
  start_date = "2026-09-01"
  end_date   = "2026-09-30"
  group_by   = ["ServiceName", "Tag:team"]
  limit      = 5000
}

output "daily_by_service" {
  value = data.costfluent_cost_data.september.data
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

# Regenerate docs/ from the schema and examples/ (requires terraform on PATH)
make docs

# Lint
make lint
```

## Versioning

Versions are `vX.Y.Z`. Renaming a resource, data source, attribute or output is a major version.

## License

MIT
