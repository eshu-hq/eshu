# Terraform State Collector

## Role and Purpose

`collector-terraform-state` is a **claim-driven worker** that reads Terraform
state snapshots, redacts secrets in the parse loop, and commits typed facts
through the shared ingestion boundary. It does not decide what work exists —
the workflow coordinator reconciles collector instances and creates claimable
work items. This runtime only claims `terraform_state` items for one
configured instance, opens the exact local file or exact S3 object the claim
points at, streams the JSON, and emits redacted facts.

**Binary:** `go run ./cmd/collector-terraform-state` (long-running service)
**Kubernetes shape:** `Deployment`
**Source:** `go/cmd/collector-terraform-state/`

## Workflow

```text
1. Load ESHU_COLLECTOR_INSTANCES_JSON; select one enabled
   terraform_state instance whose claims_enabled = true.
2. Open Postgres (shared ESHU_POSTGRES_DSN or split DSNs).
3. Build ClaimedService:
     - WorkflowControlStore claim binding
     - tfstateruntime.ClaimedSource (Resolver, SourceFactory, redaction key)
4. ClaimedService loop:
     - claim next terraform_state work item
     - DiscoveryResolver.Resolve -> exact local or S3 candidate
     - SourceFactory.OpenSource -> StateSource (local or S3 GetObject)
     - terraformstate.ParseStream(ctx, reader, options, sink)
         → (ParseStreamResult, error):
         streaming JSON walk → resource-by-resource decode
         redaction before emission (sensitive outputs, sensitive keys,
         unknown-schema scalars, tags, locator hashes)
     - IngestionStore.Commit(facts) through the shared facts boundary
     - heartbeat claim; release on success or terminal failure
```

Source entry points:

- Claim loop and source factory: `go/internal/collector/tfstateruntime/source.go`
- Streaming parser: `go/internal/collector/terraformstate/parser.go` (`ParseStream`)
- Fact sink and batching: `go/internal/collector/tfstateruntime/fact_spool.go`
- S3 source seam: `go/internal/collector/terraformstate/source_s3.go`
- Local source seam: `go/internal/collector/terraformstate/source_local.go`
- DynamoDB lock metadata: `go/cmd/collector-terraform-state/aws_dynamodb.go`
- Target-scope credential routing: `go/cmd/collector-terraform-state/target_scope_source_factory.go`

## Concurrency Model

- **Single instance per process**: the runtime selects exactly one enabled
  `terraform_state` instance from `ESHU_COLLECTOR_INSTANCES_JSON`. Scale by
  running more replicas, each with its own `instance_id`.
- **Claim-driven**: claims compete via Postgres `FOR UPDATE SKIP LOCKED` in the
  workflow control store, so multiple replicas are safe.
- **No fan-out inside a claim**: each claim reads one state source
  sequentially through the streaming parser. The parser does not parallelize
  resource decoding because correctness (one consistent snapshot per claim)
  takes priority over throughput.
- **Heartbeat**: claims heartbeat on a fixed interval; a missed heartbeat
  surfaces as a stalled claim in `/admin/status`.

## Backing Stores

| Store | Usage |
|-------|-------|
| Postgres | Workflow control store (claims), facts table, content store, status |
| S3 | Read-only `GetObject` with optional `If-None-Match` conditional reads |
| DynamoDB | Optional read-only `GetItem` for Terraform lock-table metadata |
| Graph backend | Read-only discovery of committed Terraform backend facts (Git evidence) |

Raw Terraform state bytes never enter the content store. Only redacted facts
and small warning records cross the persistence boundary.

## Configuration

### Required

| Env var | Purpose |
|---------|---------|
| `ESHU_POSTGRES_DSN` (or `ESHU_FACT_STORE_DSN` + `ESHU_CONTENT_STORE_DSN`) | Shared Postgres runtime loader. |
| `ESHU_COLLECTOR_INSTANCES_JSON` | One enabled `terraform_state` instance with `claims_enabled: true`. |
| `ESHU_TFSTATE_REDACTION_KEY` | Deployment-scoped secret; produces deterministic redaction markers. |

### Optional

| Env var | Default | Purpose |
|---------|---------|---------|
| `ESHU_TFSTATE_COLLECTOR_INSTANCE_ID` | first enabled | Pick a specific instance when more than one is enabled. |
| `ESHU_TFSTATE_COLLECTOR_OWNER_ID` | host + pid | Operator-readable owner name in claim rows. |
| `ESHU_TFSTATE_COLLECTOR_POLL_INTERVAL` | `1s` | Claim poll cadence. |
| `ESHU_TFSTATE_COLLECTOR_CLAIM_LEASE_TTL` | workflow default | Per-claim lease duration. |
| `ESHU_TFSTATE_COLLECTOR_HEARTBEAT_INTERVAL` | derived | Claim heartbeat cadence (alias: `ESHU_TFSTATE_COLLECTOR_HEARTBEAT`). |
| `ESHU_TFSTATE_SOURCE_MAX_BYTES` | reader default (512 MB) | Per-object size ceiling. Oversize emits `terraform_state_warning` with `warning_kind=state_too_large`. |

### Instance Configuration (JSON)

The instance entry in `ESHU_COLLECTOR_INSTANCES_JSON` carries everything
source-specific. See `go/cmd/collector-terraform-state/config.go` for the
parser. Minimal shape:

```json
{
  "instance_id": "terraform-state-prod",
  "collector_kind": "terraform_state",
  "mode": "continuous",
  "enabled": true,
  "claims_enabled": true,
  "configuration": {
    "target_scopes": [
      {
        "target_scope_id": "aws-prod",
        "provider": "aws",
        "deployment_mode": "central",
        "credential_mode": "central_assume_role",
        "role_arn": "arn:aws:iam::123456789012:role/eshu-tfstate-read",
        "external_id": "external-123",
        "allowed_regions": ["us-east-1"],
        "allowed_backends": ["s3", "local"]
      }
    ],
    "discovery": {
      "graph": true,
      "local_repos": ["platform-infra"],
      "local_state_candidates": {
        "mode": "approved_candidates",
        "approved": [
          {
            "repo_id": "platform-infra",
            "path": "env/prod/terraform.tfstate",
            "target_scope_id": "aws-prod"
          }
        ]
      },
      "seeds": [
        {
          "kind": "s3",
          "target_scope_id": "aws-prod",
          "bucket": "company-terraform-state",
          "key": "prod/app/terraform.tfstate",
          "region": "us-east-1",
          "dynamodb_table": "company-terraform-locks"
        }
      ]
    }
  }
}
```

### Target-Scope Credential Routing

A central scope assumes an account-scoped read role
(`credential_mode: central_assume_role`). An account-local scope uses the
default workload identity in that account
(`credential_mode: workload_identity`). The runtime routes candidates to the
right scope by explicit `target_scope_id` on the seed or by matching the
backend and region against `allowed_backends` / `allowed_regions`. Ambiguous
matches fail **before** the object is opened.

The legacy top-level `aws.role_arn` field still works for a single AWS
identity, but it cannot be mixed with `target_scopes`.

### Local State Approval Policy

The Git collector records repo-local `*.tfstate` files as advisory
`terraform_state_candidate` metadata. **That does not make them readable.**
The runtime opens a local candidate only when the instance config sets
`discovery.local_state_candidates.mode` to `approved_candidates` and lists the
exact `repo_id` plus repo-relative `path`. Symlinks and non-regular files are
rejected at open time. Approved Git-local reads emit a
`terraform_state_warning` with `warning_kind=state_in_vcs` so operators can
see which repos still ship state inside the working tree.

## Telemetry

All instruments are registered in `go/internal/telemetry/instruments.go` and
recorded by `go/internal/collector/tfstateruntime/metrics.go`. The metrics
reference at `docs/docs/reference/telemetry/metrics.md` carries the full
label and bucket schema.

| Metric | Type | Labels | Question it answers |
|--------|------|--------|---------------------|
| `eshu_dp_tfstate_snapshots_observed_total` | counter | `backend_kind`, `result` | Are we observing snapshots, and which fail? |
| `eshu_dp_tfstate_snapshot_bytes` | histogram | `backend_kind` | How big is each state we read? |
| `eshu_dp_tfstate_resources_emitted_total` | counter | `backend_kind` | Are resources reaching the fact boundary? |
| `eshu_dp_tfstate_redactions_applied_total` | counter | `reason` | What kind of secrets are we redacting, and how often? |
| `eshu_dp_tfstate_s3_conditional_get_not_modified_total` | counter | — | Is conditional-GET short-circuiting unchanged reads? |
| `eshu_dp_tfstate_parse_duration_seconds` | histogram | `backend_kind` | Is the parser slowing down? |
| `eshu_dp_tfstate_claim_wait_seconds` | histogram | (workflow-injected) | Is work backing up before a claim starts? |
| `eshu_dp_tfstate_discovery_candidates_total` | counter | `source` | How many candidates are we resolving, and from where? |

Trace spans (named in `go/internal/telemetry/contract.go`):

- `tfstate.collector.claim.process` — full claim lifecycle.
- `tfstate.discovery.resolve` — candidate resolution.
- `tfstate.source.open` — opening the state reader.
- `tfstate.parser.stream` — streaming parse.
- `tfstate.fact.emit_batch` — handoff to committer.
- `tfstate.coordinator.complete` — claim completion.

High-cardinality values (bucket names, S3 keys, absolute paths, work-item
IDs) are deliberately **excluded** from metric labels. Use the locator hash
emitted in `terraform_state_*` facts and the span attributes when you need to
investigate a specific source.

## Admin Status

The runtime mounts the shared admin surface from `go/internal/runtime/admin.go`:

- `GET /healthz` — process health.
- `GET /readyz` — readiness probe.
- `GET /metrics` — Prometheus scrape.
- `GET /admin/status?format=json` — operator status report.

Terraform-state instances surface in the generic `CollectorInstanceSummary`
inside `CoordinatorSnapshot.CollectorInstances` — see
`go/internal/status/coordinator.go:12-23`. No tfstate-specific status code
exists; the generic queue, claim, and completeness fields already reflect
the runtime. To filter the JSON response:

```bash
curl -s http://localhost:8080/admin/status?format=json \
  | jq '.collector_instances[] | select(.collector_kind=="terraform_state")'
```

## Troubleshooting

### No candidates resolved

Check that exactly one `terraform_state` instance has `enabled: true` **and**
`claims_enabled: true`. If discovery is graph-backed, confirm
`discovery.local_repos` includes the repo whose backend facts you expect, and
that the Git collector has caught up — the resolver returns
`WaitingOnGitGeneration` until the repo generation is graph-ready.

Metric to watch: `eshu_dp_tfstate_discovery_candidates_total{source=...}`.

### S3 access denied

The runtime assumes the role declared by the target scope before issuing
`GetObject`. Common causes:

1. The target-scope role does not trust the central scope's principal.
2. `external_id` mismatch.
3. The bucket policy denies the assumed role.
4. The seed targets a region not in `allowed_regions`.

Check the structured logs for the `failure_class` attribute on the failed
claim and inspect the `tfstate.source.open` span — bucket and key live in
span attributes (not metric labels).

### DynamoDB lock metadata read failures

The runtime calls `GetItem` read-only. A failure here does **not** abort the
claim; it surfaces as a warning fact and continues with the state read.
Confirm the assumed role has `dynamodb:GetItem` on the lock table and that
`dynamodb_table` is set either on the seed or on the backend fact.

### Oversize state warnings

`eshu_dp_tfstate_snapshots_observed_total{result="state_too_large"}` ticks
when an object exceeds `ESHU_TFSTATE_SOURCE_MAX_BYTES`. The runtime emits a
`terraform_state_warning` with `warning_kind=state_too_large` and skips the
claim cleanly — it does not retry-storm. If the size is legitimate, raise
the ceiling; if not, investigate why the backend grew.

### Stale conditional-GET ETag

If `eshu_dp_tfstate_s3_conditional_get_not_modified_total` plateaus at zero
while snapshots are still being observed, the runtime is reading the full
object every claim. The cause is usually a missing or wrong prior-ETag
record. Run a fresh discovery pass; the next read will store a current
ETag and re-arm conditional gets.

### Redaction-reason surge

A sudden spike in
`eshu_dp_tfstate_redactions_applied_total{reason="unknown_provider_schema"}`
usually means a new provider or new attribute shape landed and the parser is
falling through to conservative redaction. Confirm by inspecting recent
`terraform_state_warning` facts. Add the provider/attribute to the schema
package once the shape is understood; until then, the conservative path is
the correct default.

## Related Documentation

- ADR: `docs/docs/adrs/2026-04-20-terraform-state-collector.md`
- Service runtimes overview: `docs/docs/deployment/service-runtimes.md`
- Telemetry reference: `docs/docs/reference/telemetry/metrics.md` (tfstate
  section)
- Package surfaces: `go/internal/collector/terraformstate/README.md`,
  `go/internal/collector/tfstateruntime/README.md`,
  `go/cmd/collector-terraform-state/README.md`
- Dashboard: `docs/dashboards/tfstate.json`
- Alert rules: `deploy/observability/alerts.yaml` (`eshu.tfstate` group)
