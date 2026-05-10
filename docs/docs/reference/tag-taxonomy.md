# Tag Taxonomy

Eshu uses tag taxonomy to normalize cloud, Terraform state, and configuration
tags into a small set of canonical keys. Collectors do not decide tag meaning.
They emit raw observations. The reducer-owned tag normalizer and correlation
DSL decide normalized meaning from versioned packs and explicit overrides.

## First-Party Alias Pack

The first shipped pack is `first-party/aws-core`.

| Canonical key | Aliases |
| --- | --- |
| `environment` | `environment`, `env`, `stage`, `app_env`, `application_environment`, `deployment_environment` |
| `service` | `service`, `service_name`, `app`, `app_name`, `application`, `component`, `workload`, `project` |
| `owner` | `owner`, `team`, `squad`, `tribe`, `cost_center`, `business_unit` |

Key matching trims whitespace and ignores case. Raw keys and values remain
unchanged in source facts. Normalized tags are derived evidence and must keep
their provenance.

## Override Schema

Use overrides when a company has local tag names or value spelling that should
map to canonical keys.

```yaml
tagTaxonomy:
  aliasPacks:
    - first-party/aws-core
  aliases:
    environment:
      - StageName
    service:
      - Product
  valueMap:
    environment:
      prd: production
      prod: production
      stg: staging
  disabledAliases:
    owner:
      - business_unit
  accountOverrides:
    "123456789012":
      aliases:
        owner:
          - OwningTeam
      valueMap:
        environment:
          live: production
```

Precedence is account override, then collector-instance override, then
first-party pack. Overrides can add aliases, disable aliases, or normalize
values. They cannot rename canonical keys or mutate raw facts.

## Source Precedence

For the same resolved resource identity, current-state tag truth uses this
order:

1. Live cloud observation.
2. Terraform state observation.
3. Source configuration.

If sources disagree, the higher-precedence value is used for current-state
queries and the conflict remains visible as evidence. A missing scan is a
coverage gap, not negative evidence, until the relevant scope is ready.

## Weak Signals

`Name` tags and resource names are weak signals. They can help explain or group
candidates, but they cannot admit canonical workload, owner, service, or
environment truth by themselves.

Name-derived matches need a stronger anchor such as an ARN, image digest,
Terraform provider-resolved ARN, module source path, explicit service tag, or
explicit environment tag.

## Learning Loop

AWS scans emit `aws_tag_distribution` summary facts so operators can see local
tag patterns that are not covered by the current taxonomy.

```json
{
  "fact_kind": "aws_tag_distribution",
  "account_id": "123456789012",
  "region": "us-east-1",
  "resource_type": "ecs_service",
  "tag_key_raw": "StageName",
  "tag_key_normalized": "",
  "observed_count": 37,
  "distinct_value_count": 3,
  "top_value_hashes": [
    {"hash": "sha256:...", "count": 31}
  ],
  "first_seen_generation_id": "aws-17",
  "last_seen_generation_id": "aws-21"
}
```

`/admin/status` must expose tag taxonomy status when the AWS and reducer tag
normalization surfaces are enabled:

- coverage by canonical key
- top unknown tag keys
- aliases and overrides applied
- disabled aliases encountered
- resources missing expected canonical tags after source readiness

JSON responses expose the summary under `tag_taxonomy`:

```json
{
  "tag_taxonomy": {
    "alias_packs": ["first-party/aws-core"],
    "coverage": [
      {"canonical_key": "environment", "resources_observed": 120, "resources_with_key": 104}
    ],
    "unknown_tag_keys": [
      {"tag_key_raw": "StageName", "resource_type": "ecs_service", "observed_count": 37}
    ],
    "applied_aliases": [
      {"canonical_key": "environment", "alias": "env", "source": "first-party/aws-core"}
    ],
    "disabled_aliases": [
      {"canonical_key": "owner", "alias": "business_unit", "source": "collector-instance"}
    ],
    "missing_expected_tags": [
      {"canonical_key": "owner", "resource_type": "lambda_function", "count": 9}
    ]
  }
}
```

Metrics must not use raw tag values as labels. Raw values are evidence data and
must follow the same redaction discipline as collector payloads.
