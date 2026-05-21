# Terraform State Collector

`collector-terraform-state` is a claim-driven worker that reads exact
Terraform state snapshots, redacts sensitive values during parsing, and commits
typed facts through the shared ingestion boundary. It does not decide what work
exists and it does not write graph truth.

The workflow coordinator creates claimable `terraform_state` work items. The
collector claims one item, resolves the exact local file or S3 object named by
that claim, streams the JSON, emits redacted facts, and completes or retries the
claim through the shared workflow control store.

| Runtime | Value |
| --- | --- |
| Binary | `go run ./cmd/collector-terraform-state` |
| Kubernetes shape | `Deployment` |
| Command package | `go/cmd/collector-terraform-state/` |
| Parser package | `go/internal/collector/terraformstate/` |
| Claim adapter | `go/internal/collector/tfstateruntime/` |

## Read Next

| Need | Read |
| --- | --- |
| Instance JSON, discovery modes, target scopes, local approval, and redaction config | [Terraform State Collector Config](collector-terraform-state-config.md) |
| Metrics, admin status, and failure triage | [Terraform State Collector Operations](collector-terraform-state-operations.md) |
| Collector environment variables | [Collector Environment](../reference/environment-collectors.md) |
| Collector metrics catalog | [Ingestion And Collector Metrics](../reference/telemetry/metrics-ingestion-collectors.md) |

## Workflow

```text
1. Load ESHU_COLLECTOR_INSTANCES_JSON.
2. Select exactly one enabled terraform_state instance with claims_enabled=true.
3. Open Postgres and workflow control stores.
4. Claim the next terraform_state work item.
5. Resolve discovery config into exact local or S3 candidates.
6. Match the claimed work item to one candidate.
7. Open the source through the local reader or read-only S3 object client.
8. Read snapshot lineage and serial.
9. Stream Terraform state through the parser.
10. Redact sensitive outputs, sensitive keys, unknown-schema scalars, unknown
    composites, tags, and locator values before persistence.
11. Commit terraform_state_* facts through the shared ingestion boundary.
12. Heartbeat and release the claim on success or terminal failure.
```

Source entry points:

- `go/cmd/collector-terraform-state/main.go`
- `go/cmd/collector-terraform-state/config.go`
- `go/cmd/collector-terraform-state/service.go`
- `go/cmd/collector-terraform-state/target_scope_source_factory.go`
- `go/internal/collector/tfstateruntime/source.go`
- `go/internal/collector/terraformstate/parser.go`
- `go/internal/collector/terraformstate/discovery.go`

## Ownership Boundaries

| Layer | Owns | Does not own |
| --- | --- | --- |
| Workflow coordinator | Reconciling collector instances and creating claimable work items. | Reading Terraform state bytes. |
| `collector-terraform-state` command | Runtime config, AWS credential routing, S3 and DynamoDB adapters, service loop wiring. | Terraform JSON parsing rules. |
| `tfstateruntime` | Claim-to-candidate matching, source open, snapshot identity, parser handoff, collected generation return. | Cloud SDK calls and fact commits. |
| `terraformstate` package | Discovery primitives, exact source contracts, streaming parse, redaction, fact envelopes. | Workflow claims, credential selection, graph writes. |

## Backing Stores

| Store | Usage |
| --- | --- |
| Postgres | Workflow claims, facts, content/status surfaces, and prior snapshot freshness metadata. |
| S3 | Read-only exact `GetObject` state reads with optional conditional reads. |
| DynamoDB | Optional read-only `GetItem` for Terraform lock metadata. |
| Graph/Postgres facts | Read-only discovery of indexed Terraform backend and Terragrunt remote-state declarations. |

Raw Terraform state bytes stay inside the source reader and parser window.
Only redacted facts and warning records cross the persistence boundary.

## Concurrency Model

- The process selects one enabled, claim-capable `terraform_state` instance.
- Multiple replicas are safe because claims are coordinated through Postgres
  workflow rows.
- Each claim reads one exact state source sequentially. The parser does not
  parallelize resource decoding because one claim represents one consistent
  snapshot.
- Claim heartbeat and lease settings use the shared workflow claim contract.

## Evidence Emitted

The collector emits reported facts. Reducers and query surfaces decide what
becomes graph truth.

| Fact kind | Meaning |
| --- | --- |
| `terraform_state_snapshot` | State lineage, serial, backend kind, safe locator hash, and read metadata. |
| `terraform_state_resource` | Redacted resource instance evidence. |
| `terraform_state_output` | Redacted output evidence. |
| `terraform_state_module` | Module/resource membership evidence. |
| `terraform_state_provider_binding` | Provider binding evidence for state resources. |
| `terraform_state_tag_observation` | Redacted `tags` and `tags_all` observations for correlation. |
| `terraform_state_warning` | Non-fatal safety or source condition, such as `state_in_vcs`, `state_too_large`, `state_missing`, or redaction drops. |

## Safety Rules

- Do not read unapproved repo-local `.tfstate` files. Git discovery records
  them only as advisory metadata until instance config approves an exact
  repo-relative path.
- Do not scan S3 buckets. S3 reads require exact bucket, key, and region values.
- Do not emit raw bucket names, object keys, local paths, secret values, or full
  Terraform state locators in facts, metric labels, or routine logs.
- Do not treat DynamoDB lock metadata as a consistency decision. It is
  observational context around the opened state body.
- Do not route ambiguous target scopes. Ambiguous matches fail before a source
  is opened.

## Validation

Use focused non-live gates for normal PR validation:

```bash
cd go
go test ./cmd/collector-terraform-state ./internal/collector/terraformstate ./internal/collector/tfstateruntime -count=1
go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence
```

Use live S3 or DynamoDB smokes only with operator-approved read-only target
roles. Keep account IDs, bucket names, object keys, role ARNs, external IDs,
and local absolute paths out of committed docs unless they are clearly
non-secret examples.

## Related Docs

- [Terraform State Collector Config](collector-terraform-state-config.md)
- [Terraform State Collector Operations](collector-terraform-state-operations.md)
- [Collector Service Runtimes](../deployment/service-runtimes-collectors.md)
- [Collector And Reducer Readiness](../reference/collector-reducer-readiness.md)
- [Runtime Admin API](../reference/runtime-admin-api.md)
