# Terraform State Collector

Use this page for the Terraform-state collector boundary and workflow. Instance
JSON and discovery policy live in
[Terraform State Collector Config](collector-terraform-state-config.md).
Metrics and triage live in
[Terraform State Collector Operations](collector-terraform-state-operations.md).

`collector-terraform-state` is a claim-driven worker that reads exact
Terraform state snapshots, redacts sensitive values during parsing, and commits
typed facts through the ingestion boundary. It does not decide what work exists
and it does not write graph truth.

| Runtime | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-collector-terraform-state` |
| Kubernetes shape | optional `Deployment` |
| Command package | `go/cmd/collector-terraform-state/` |
| Parser package | `go/internal/collector/terraformstate/` |
| Claim adapter | `go/internal/collector/tfstateruntime/` |

## Workflow

```text
ESHU_COLLECTOR_INSTANCES_JSON
  -> select terraform_state instance
  -> claim workflow item
  -> resolve exact local or S3 state candidate
  -> open approved source
  -> stream and redact Terraform state
  -> commit terraform_state_* facts
  -> heartbeat/release claim
```

Raw Terraform state bytes stay inside the source reader and parser window.
Only redacted facts and warning records cross the persistence boundary.

## Ownership Boundaries

| Layer | Owns |
| --- | --- |
| Workflow coordinator | Collector instance reconciliation and claimable work creation. |
| `collector-terraform-state` command | Runtime config, AWS credential routing, S3/DynamoDB adapters, service loop wiring. |
| `tfstateruntime` | Claim-to-candidate matching, source open, snapshot identity, parser handoff. |
| `terraformstate` package | Discovery primitives, exact source contracts, streaming parse, redaction, fact envelopes. |

Reducers and query surfaces decide what becomes graph truth.

## Safety Rules

- Multiple replicas are safe because claims are coordinated through Postgres.
- Each claim reads one exact state source sequentially.
- Do not read unapproved repo-local `.tfstate` files.
- Do not scan S3 buckets; S3 reads require exact bucket, key, and region.
- Do not emit raw bucket names, object keys, local paths, secret values, or full
  Terraform state locators in facts, metric labels, or routine logs.
- Treat DynamoDB lock metadata as observation only, not a consistency decision.
- Ambiguous target scopes fail before a source is opened.

## Evidence Emitted

The collector emits reported `terraform_state_snapshot`,
`terraform_state_resource`, `terraform_state_output`,
`terraform_state_module`, `terraform_state_provider_binding`,
`terraform_state_tag_observation`, and `terraform_state_warning` facts.
Repository ingestion may also emit Git-scoped `terraform_state_warning` facts
for unresolved Terraform backend expressions before any state source is opened.
Those warnings are discovery evidence only: they keep the unresolved source
visible in status while preserving the exact-candidate requirement for
Terraform-state reads.

## Related Docs

- [Terraform State Collector Config](collector-terraform-state-config.md)
- [Terraform State Collector Operations](collector-terraform-state-operations.md)
- [Collector Runtime Services](../deployment/service-runtimes-collectors.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
