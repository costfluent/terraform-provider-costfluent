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

## Releasing a New Version

This repository is a published mirror. The source lives in the private Costfluent monorepo at
`toolkit/terraform-provider`, and every change — including a one-word README fix — is made there and
synced here. Nothing is ever committed or pushed to the public repository directly: the sync runs
`rsync --delete` over a hard reset to `origin/main`, so anything that exists only here is destroyed
by the next publish.

All commands below run from the monorepo root.

### One-time prerequisites

- A clone of the public repository at `~/Repositories/costfluent-public/terraform-provider-costfluent`
  (or set `COSTFLUENT_PUBLIC_ROOT`).
- `GPG_PRIVATE_KEY` and `GPG_PASSPHRASE` present as Actions secrets on the public repository. They
  are provisioned from the monorepo's GitHub Terraform root rather than set by hand; without them
  the release job fails at its "Import gpg key" step.
- The provider registered on the Terraform Registry under `costfluent/costfluent` with that key's
  public half. The registry ingests tagged releases only after this is done.

### Release

```bash
# 1. Land the change in the monorepo first: branch, PR, merge to dev.
make check-toolkit

# 2. See exactly what the sync would change in the public repository. Touches nothing.
scripts/publish-public.sh diff terraform-provider

# 3. Mirror the source into the public repository as a pull request.
scripts/publish-public.sh publish terraform-provider --pr

# 4. Merge that pull request. The tag in step 5 must point at the mirrored tree.
gh pr merge --repo costfluent/terraform-provider-costfluent --squash --delete-branch <pr-number>

# 5. Tag the merged main. The tag is the release: it triggers .github/workflows/release.yml in the
#    public repository, which builds, signs and publishes the artifacts the registry ingests.
scripts/publish-public.sh release terraform-provider v0.1.0

# 6. Watch the release build.
gh run watch --repo costfluent/terraform-provider-costfluent
```

`release` refuses to tag a public `main` that differs from the monorepo directory:

```text
error: terraform-provider: origin/main of costfluent/terraform-provider-costfluent differs from
toolkit/terraform-provider. Publish and merge that first.
```

That means steps 3 and 4 have not landed yet — a tag on a tree nobody reviewed would ship source the
monorepo never approved. Run `diff` to see the gap, then publish and merge.

Other useful forms:

```bash
scripts/publish-public.sh list                                 # every mirrored target
scripts/publish-public.sh publish terraform-provider           # commit locally, do not push
scripts/publish-public.sh publish terraform-provider --push    # push the branch, no pull request
scripts/publish-public.sh publish terraform-provider --pr --message "Add segment resource"
```

A publish that finds no difference does nothing: no commit, no branch, no empty pull request. A
release skips a target that already carries the tag.

Version numbers are `vX.Y.Z` — the Terraform Registry requires that form and the script rejects
anything else. Renaming a resource, data source, attribute or output is a major version.

## License

MIT
