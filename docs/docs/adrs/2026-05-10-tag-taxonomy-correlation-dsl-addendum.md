# ADR: Tag Taxonomy Addendum For Correlation DSL

**Date:** 2026-05-10
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering

**Related:**

- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-multi-source-reducer-and-consumer-contract.md`
- `2026-04-20-aws-cloud-scanner-collector.md`
- `2026-04-20-terraform-state-collector.md`
- Reference: `docs/docs/reference/tag-taxonomy.md`
- Issue: `#28`

---

## Context

The AWS scanner ADR already defines raw tag emission and
`aws_tag_distribution` summary facts. The Terraform state ADR also points tag
normalization, value aliasing, and precedence back to the correlation DSL.

This addendum freezes that missing contract. It does not move write ownership
to collectors. Collectors still emit observed facts. The reducer-owned tag
normalizer and DSL decide normalized tag meaning, precedence, weak signals,
negative evidence, and admin learning-loop output.

## Decision

Eshu ships a first-party tag taxonomy pack for the correlation DSL. The first
pack covers the only tag families allowed to influence cloud, state, and
configuration correlation at launch:

| Canonical key | First-party aliases |
| --- | --- |
| `environment` | `environment`, `env`, `stage`, `app_env`, `application_environment`, `deployment_environment` |
| `service` | `service`, `service_name`, `app`, `app_name`, `application`, `component`, `workload`, `project` |
| `owner` | `owner`, `team`, `squad`, `tribe`, `cost_center`, `business_unit` |

Alias matching is case-insensitive for keys after trimming whitespace. Raw tag
keys and values remain preserved in source facts. Normalized tags are derived
evidence and must record the alias pack, override source, source system, and
generation that produced them.

## Per-Instance Overrides

Runtime instances may load explicit overrides for local naming conventions.
Overrides are configuration, not learned truth. They must be versioned and
reported on the admin surface.

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

Override precedence is:

1. Account override.
2. Runtime-instance override.
3. First-party alias pack.

Overrides may add aliases, disable aliases, or normalize values for a
canonical key. They must not rename canonical keys, mutate raw facts, or make
`Name` tags strong evidence.

## Evidence Precedence

When the same canonical tag key appears for the same resolved resource identity,
the DSL uses this precedence ladder:

1. Live cloud observation.
2. Terraform state observation.
3. Source configuration.

This ladder answers "what is true now?" for a resolved cloud resource. It does
not erase intent. If Terraform state says `environment=staging` and AWS says
`environment=production`, the cloud value wins for current-state queries, and
the DSL must emit conflict evidence explaining the state/config mismatch.

Configuration cannot override an observed cloud value. State cannot override
cloud. Missing cloud coverage is not negative evidence; it is a coverage gap
until the coordinator marks the relevant cloud scope ready.

## Negative Evidence

The DSL must express negative evidence separately from absence:

- Expected canonical tag missing after source readiness is complete.
- A disabled alias appears and is intentionally ignored.
- Two sources provide conflicting normalized values for the same canonical key.
- A candidate depends only on broad or shared tags without a stronger anchor.

Negative evidence may lower confidence, reject candidate admission, or produce
a drift observation. It must not silently delete the positive evidence that led
to the conflict.

## Name Tags And Resource Names

`Name` tags and provider-native resource names are weak signals. They may help
group candidates for inspection, but they cannot admit canonical workload,
ownership, or environment truth by themselves.

Name-derived matches are allowed only when at least one stronger anchor is
present, such as ARN, image digest, Terraform provider-resolved ARN, module
source path, explicit service tag, or explicit environment tag.

## Learning Loop

The AWS scanner emits `aws_tag_distribution` summary facts. The DSL admin
surface consumes those facts to show operators where a local alias override may
be useful.

Required fact shape:

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

The admin surface must show:

- coverage by canonical key for `environment`, `service`, and `owner`
- top unknown tag keys by account, region, and resource type
- aliases applied from first-party packs and overrides
- disabled aliases encountered
- resources missing expected canonical tags after source readiness

`/admin/status?format=json` exposes that summary under `tag_taxonomy`:

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
      {"canonical_key": "owner", "alias": "business_unit", "source": "runtime-instance"}
    ],
    "missing_expected_tags": [
      {"canonical_key": "owner", "resource_type": "lambda_function", "count": 9}
    ]
  }
}
```

Telemetry must stay low-cardinality. Metrics may use canonical key and outcome
labels, but not raw tag values. Raw tag values are evidence data and must pass
through the same redaction discipline as other collector payloads.

## Consequences

This design makes tag-based correlation useful without pretending tags are
stronger than they are. Tags can strengthen cloud/state/config joins, explain
ownership, and expose drift. They cannot replace deterministic anchors.

The learning loop is intentionally advisory. Eshu can suggest that `StageName`
looks like an environment alias, but an operator must accept the override before
it changes normalization behavior.

## Acceptance

This ADR is accepted when:

- the first-party alias pack is documented in the public tag taxonomy reference
- per-instance and per-account override schema is documented
- `aws_tag_distribution` is tied to the admin status surface
- name-tag weak-signal and negative-evidence rules are documented
