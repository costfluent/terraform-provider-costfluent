---
page_title: "Upgrading from 0.2"
subcategory: ""
description: |-
  The configuration changes version 0.3.0 needs, resource by resource, when you upgrade from 0.2.x.
---

# Upgrading from 0.2

Version 0.3.0 renames and reshapes attributes of several resources and data sources. Terraform
upgrades the state 0.2.x wrote when it first reads it with 0.3.0, so nothing needs to be imported
again; the configuration needs the edits below.

Until version 1.0, a minor version may need changes like these, and a patch version never does.
Pin the minor version, so a new minor version is a change you make on purpose:

```terraform
terraform {
  required_providers {
    costfluent = {
      source  = "costfluent/costfluent"
      version = "~> 0.3.0"
    }
  }
}
```

After editing, read `terraform plan` before you apply. A rewritten configuration plans in-place
updates at most; a planned replacement means the configuration does not yet say what the object
holds.

## costfluent_budget

A budget has a start date and a cadence instead of a period, and its amounts are exact decimals.

- `amount` is a decimal string with at most two decimals, such as `"1250.50"`, so it is never
  rounded through a float. Exactly one of `amount` and the new `periods`, an amount per period, is
  set.
- `period` is replaced by `interval_count` and `interval_unit`. `interval_unit` takes `day`,
  `week`, `month` or `year`, in lowercase, and the two default to one month:

  | 0.2.x `period` | 0.3.0 |
  |---|---|
  | `"Monthly"` | leave both out |
  | `"Quarterly"` | `interval_count = 3` |
  | `"Yearly"` | `interval_unit = "year"` |

  The state upgrade maps each budget's period the same way.
- `start_date` is new. Leave it out to keep the start date the budget already has; changing it
  recreates the budget, as changing the cadence does.
- `alerts` defaults to 50, 80 and 100 percent. List a budget's thresholds when they differ, or the
  plan recreates it.
- Changing or removing `segment_id` now moves the same budget in place.
- `cost_report_id` (track a saved report's total) and `app_ids` (notification channels) are new.
- `current_spend` is a decimal string, and `state` is new.

```terraform
# 0.2.x
resource "costfluent_budget" "cloud" {
  name     = "Cloud"
  amount   = 1250.5
  currency = "EUR"
  period   = "Quarterly"
}

# 0.3.0
resource "costfluent_budget" "cloud" {
  name           = "Cloud"
  amount         = "1250.50"
  currency       = "EUR"
  interval_count = 3
}
```

## costfluent_cost_report

`group_by` is a list of up to three slots, outermost first, instead of one dimension string. Each
slot has a `dimension` and, for `Tag` and `VirtualTag`, a `key`. Left out, a report groups by
`ServiceName`; `[]` gives a single total. The state upgrade turns each report's string into one
slot.

```terraform
# 0.2.x
group_by = "Region"

# 0.3.0
group_by = [{ dimension = "Region" }]
```

`settings` gains `measure`, `unit_cost_metric_id` and `unit_cost_label_filter`, and `filter` also
takes CQL text.

## costfluent_allocation_rule

`match_field`, `match_operator`, `match_value` and `match_tag_key` are replaced by one required
`filter`: the cost the rule claims, as a cost filter JSON document built with `jsonencode` or as
CQL text, the way `costfluent_cost_report` takes it.

## costfluent_virtual_tag

`rules`, a JSON document, is replaced by `rule` blocks in evaluation order, each with a `filter`
and the `value` a matching row gets. `collapsed_tag_keys` is new. `computation_mode`, `is_active`
and `priority` are removed, and `status` and `last_computed_at` are no longer exported. Removing
`description` or `default_value` no longer recreates the tag.

## costfluent_cost_alert

`threshold_value` is a decimal string, such as `"250"` or `"12.5"`. Removing `filter` or
`comparison_period` clears it in place instead of recreating the alert. `minimum_amount` is new.

## costfluent_provider

- Changing `name` renames the connection in place: it keeps its ID and the costs it has
  collected. Changing `settings` updates it in place, and removing them clears them.
- A plan that changes `parent_provider_id` is refused rather than recreating the connection.
- `sync_frequency_minutes` takes 60 to 1440 and defaults to 1440, once a day, instead of 360. A
  connection that left it out plans an update to 1440; set `sync_frequency_minutes = 360` to keep
  syncing every six hours.
- `key` is checked at plan against the keys the Public API knows.

## Other resources

- `costfluent_folder`: removing `parent_id` moves the folder back to the top level instead of
  recreating it. `saved_filter_id` is new.
- `costfluent_dashboard`: `is_default` changes in place. `saved_filter_id`, `start_date` and
  `end_date` are new.
- `costfluent_workspace`: `time_zone`, `report_defaults` and the anomaly settings are new.

## Data sources

- `costfluent_cost_data`: `group_by` is a list of up to three slots, each a dimension or
  `Dimension:key`, such as `["ServiceName", "Tag:team"]`. `cost`, `amortized_cost`, `list_cost` and
  `total_cost` are decimal strings, and `measure` is new.
- `costfluent_cost_summary`, `costfluent_anomalies` and `costfluent_entitlement`: amounts are
  decimal strings.
- `costfluent_anomalies`: `total_count` and `unacknowledged_count` are removed.

## New in 0.3.0

The resources `costfluent_app`, `costfluent_business_metric`, `costfluent_cost_report_subscription`,
`costfluent_dashboard_widget`, `costfluent_team`, `costfluent_team_member`,
`costfluent_team_workspace_access` and `costfluent_workspace_provider`, and the data source
`costfluent_user`. A webhook `costfluent_app` created without a secret in its config exports the
secret Costfluent generated as the sensitive `signing_secret`, to check each delivery's
`X-Costfluent-Signature` with.
