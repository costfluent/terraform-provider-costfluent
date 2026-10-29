---
page_title: "Upgrading from 0.3"
subcategory: ""
description: |-
  The configuration changes version 0.4.0 needs when you upgrade from 0.3.x.
---

# Upgrading from 0.3

Version 0.4.0 calls a connection's own name `display_name`, as the Public API does. Terraform
upgrades the state 0.3.x and 0.2.x wrote when it first reads it with 0.4.0, so nothing needs to be
imported again; the configuration needs the edits below. Upgrading from 0.2.x, make the edits in
[Upgrading from 0.2](upgrade-from-0.2) as well.

After editing, read `terraform plan` before you apply. A rewritten configuration plans no change
for a connection; a planned update or replacement means the configuration does not yet say what
the connection holds.

## costfluent_provider

`name` is `display_name`. It is still required, and changing it still renames the connection in
place: it keeps its ID and the costs it has collected.

```terraform
# 0.3.x
resource "costfluent_provider" "aws" {
  key         = "aws"
  name        = "AWS Production"
  credentials = { role_arn = "arn:aws:iam::123456789012:role/CostfluentBillingRole" }
}

# 0.4.0
resource "costfluent_provider" "aws" {
  key          = "aws"
  display_name = "AWS Production"
  credentials  = { role_arn = "arn:aws:iam::123456789012:role/CostfluentBillingRole" }
}
```

## Data sources

- `costfluent_provider`: look a connection up by `display_name` instead of `name`, and read its
  name from `display_name`.
- `costfluent_providers`: each connection's name is `display_name`.

`costfluent_workspace_provider` keeps `provider_name`: it is the connection's display name as the
workspace sees it, not an attribute of its own.
