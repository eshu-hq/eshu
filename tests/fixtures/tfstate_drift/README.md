# tfstate Drift Compose Proof Corpus

Compose-level proof corpus for the Terraform config-vs-state drift handler
(`DomainConfigStateDrift`). This fixture is the runtime evidence input for
issue #166 — the follow-up to PR #165 that proves the drift adapters wired
in production actually fire end-to-end against a real Postgres + reducer.

## Layout

| File | Purpose |
| --- | --- |
| `seed.sql` | Idempotent SQL that writes the four scenarios into `ingestion_scopes`, `scope_generations`, and `fact_records`. |
| `expected/added_in_state.json` | Human-readable documentation of the expected counter delta and structured log line for scenario A. The verifier asserts these inline today; the JSON exists for review and future scenarios. |
| `expected/added_in_config.json` | Same shape for scenario B. |
| `expected/removed_from_state.json` | Same shape for scenario C. |
| `expected/ambiguous_owner.json` | Documents the expected zero counter delta and the rejection log for the ambiguous-owner path. |

## How the corpus drives the drift handler

The seed writes the exact JSON shapes the production queries read:

- `go/internal/storage/postgres/tfstate_backend_canonical.go`
  reads `fact_records.payload->'parsed_file_data'->'terraform_backends'`.
- `go/internal/storage/postgres/tfstate_drift_evidence.go`
  reads `fact_records.payload->'parsed_file_data'->'terraform_resources'`
  for the config side and `fact_records.fact_kind = 'terraform_state_resource'`
  for the state side, joined through `fact_records.fact_kind = 'terraform_state_snapshot'`
  metadata (lineage and serial).
- `go/internal/storage/postgres/drift_enqueue.go` (Phase 3.5) walks
  `ingestion_scopes` for every `state_snapshot:*` scope with an active
  generation and enqueues one `config_state_drift` reducer intent per scope.

Hand-crafted JSON with the same shape is indistinguishable from
collector- or parser-emitted JSON for query purposes. Drift is not graph-projected
in v1 (per design doc §10); counters and structured logs are the v1 truth surface.

## Scenarios

| Scenario | Backend | Locator hash | Drift kind | Why |
| --- | --- | --- | --- | --- |
| `drift-tfstate-added-in-state` | `s3://eshu-drift-a/prod/terraform.tfstate` | `01c90e0b…` | `added_in_state` | State carries `aws_s3_bucket.unmanaged`; config has no resource block for it. |
| `drift-tfstate-added-in-config` | `s3://eshu-drift-b/prod/terraform.tfstate` | `ac321992…` | `added_in_config` | Config declares `aws_s3_bucket.declared`; state has zero `terraform_state_resource` rows. |
| `drift-tfstate-removed-from-state` | `s3://eshu-drift-c/prod/terraform.tfstate` | `33f0f3a3…` | `removed_from_state` | Prior state generation (serial=1) carried `aws_s3_bucket.was_there`; current generation (serial=2) does not; config still declares the resource. Same lineage so `LineageRotation` does not suppress. |
| `drift-tfstate-ambiguous-{a,b}` | `s3://eshu-drift-d/prod/terraform.tfstate` | `6ef42db5…` | (rejection) | Two repos both emit `terraform_backends` facts at the same `(s3, 6ef42db5…)`. Resolver returns `ErrAmbiguousBackendOwner`; handler logs WARN with `failure_class="ambiguous_backend_owner"` and emits no counter increments. |

`attribute_drift` and `removed_from_config` are out of scope for #166 — the parser does not emit per-attribute values today, and the loader leaves `PreviouslyDeclaredInConfig=false` in v1. Issues #167 and #168 track those follow-ups. Module-nested addresses are also out of scope (#169).

## Regenerating locator hashes

The four hashes are precomputed against `terraformstate.LocatorHash` at `go/internal/collector/terraformstate/identity.go:100`:

```
sha256(BackendKind + "\x00" + Locator + "\x00" + VersionID)
```

with `Locator = "s3://" + bucket + "/" + key` and `VersionID = ""`. To regenerate:

```bash
cd go && mkdir -p internal/collector/terraformstate/_locator_check && cat > internal/collector/terraformstate/_locator_check/main.go <<'EOF'
package main

import (
    "fmt"

    "github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func main() {
    keys := []terraformstate.StateKey{
        {BackendKind: terraformstate.BackendS3, Locator: "s3://eshu-drift-a/prod/terraform.tfstate"},
        {BackendKind: terraformstate.BackendS3, Locator: "s3://eshu-drift-b/prod/terraform.tfstate"},
        {BackendKind: terraformstate.BackendS3, Locator: "s3://eshu-drift-c/prod/terraform.tfstate"},
        {BackendKind: terraformstate.BackendS3, Locator: "s3://eshu-drift-d/prod/terraform.tfstate"},
    }
    for _, k := range keys {
        fmt.Printf("%s %s\n", k.Locator, terraformstate.LocatorHash(k))
    }
}
EOF
go run ./internal/collector/terraformstate/_locator_check/ && rm -rf internal/collector/terraformstate/_locator_check
```

If `LocatorHash` is ever renamed or its construction changes, the seed's hashes drift and the verifier will fail with "expected scope `state_snapshot:s3:<hex>` not present." Regenerate above and update `seed.sql` (the four hashes are pinned in its header comment block).

## Running the proof matrix

The seed is consumed by `scripts/verify_tfstate_drift_compose.sh`. That script brings the compose stack up, applies the seed, reruns bootstrap-index so Phase 3.5 picks up the seeded `state_snapshot:*` scopes, waits for the reducer to drain the queued drift intents, scrapes counters from `localhost:${ESHU_RESOLUTION_ENGINE_METRICS_PORT}/metrics`, asserts the per-kind counter deltas and structured-log shape inline, and writes a proof artifact to `docs/superpowers/proofs/<date>-tfstate-drift-compose.md` when invoked with `ESHU_TFSTATE_DRIFT_PROOF_OUT` set. The `expected/*.json` files in this directory are reviewer-facing documentation of what the inline assertions check; they are not parsed by the verifier today.

## Why this corpus does NOT exercise `collector-terraform-state`

The state collector is a separate long-running binary (`eshu-collector-terraform-state`) gated behind `workflow-coordinator` claims. It is not enabled in `docker-compose.yaml`. Standing it up would require:

- adding a new compose service for the collector,
- enabling `workflow-coordinator` claims (`ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED=true`),
- supplying `ESHU_COLLECTOR_INSTANCES_JSON` with a `terraform_state` instance that approves a local-state candidate,
- and a fixture Terraform repo with `terraform { backend "s3" { … } }` plus a fixture `.tfstate` file.

That's a substantial compose-surface change orthogonal to #166's question ("does the drift handler fire correctly under the production reducer?"). #166 reaches the same handler entry points by writing the durable facts the collector would otherwise emit. A future issue tracks the full E2E proof.
