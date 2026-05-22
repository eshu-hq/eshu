# tfstate Drift Tier-1 Fixture

Compose proof corpus for `scripts/verify_tfstate_drift_compose.sh`.

Tier-1 seeds Postgres directly with Terraform config and state facts, then
reruns bootstrap Phase 3.5 so the reducer handles real `config_state_drift`
work. It proves the drift handler and evidence loader without standing up the
Terraform-state collector. Tier-2 lives in `../tfstate_drift_tier2/` and proves
the collector and workflow-coordinator wire.

## Layout

| File | Purpose |
| --- | --- |
| `seed.sql` | Idempotent rows for the six drift scenarios. |
| `expected/added_in_state.json` | Reviewer-facing expected counter/log shape for bucket A. |
| `expected/added_in_config.json` | Reviewer-facing expected counter/log shape for bucket B. |
| `expected/removed_from_state.json` | Reviewer-facing expected counter/log shape for bucket C. |
| `expected/ambiguous_owner.json` | Reviewer-facing zero-counter and rejection-log shape for bucket D. |
| `expected/attribute_drift.json` | Reviewer-facing expected counter/log shape for bucket E. |
| `expected/removed_from_config.json` | Reviewer-facing expected counter/log shape for bucket F. |

The `expected/*.json` files are documentation for review. The verifier asserts
the same behavior inline today.

## Scenarios

| Scenario | Drift kind | Fixture signal |
| --- | --- | --- |
| `drift-tfstate-added-in-state` | `added_in_state` | State has `aws_s3_bucket.unmanaged`; config has no matching block. |
| `drift-tfstate-added-in-config` | `added_in_config` | Config has `aws_s3_bucket.declared`; state has no resources. |
| `drift-tfstate-removed-from-state` | `removed_from_state` | Prior state serial 1 has `aws_s3_bucket.was_there`; current serial 2 does not. |
| `drift-tfstate-ambiguous-{a,b}` | rejection | Two repos claim the same backend, so ownership is ambiguous. |
| `drift-tfstate-attribute-drift` | `attribute_drift` | Config and state both have `aws_s3_bucket.logs`, but the allowlisted SSE value differs. |
| `drift-tfstate-removed-from-config` | `removed_from_config` | State still has `aws_iam_policy.legacy`; current config removed it after a prior generation declared it. |

Module-nested addresses are intentionally out of scope for this corpus.

## What The Fixture Proves

The seed writes the JSON shapes read by:

- `go/internal/storage/postgres/tfstate_backend_canonical.go` for backend
  ownership.
- `go/internal/storage/postgres/tfstate_drift_evidence.go` and its helper files
  for config/state rows and prior-generation evidence.
- `go/internal/storage/postgres/drift_enqueue.go` for Phase 3.5 work enqueue.

Hand-written fact JSON is enough here because the handler reads durable facts,
not collector process state. Drift is not graph-projected in v1; counters and
structured logs are the assertion surface.

## Locator Hash Contract

The seed pins six `state_snapshot:s3:<hash>` scope IDs computed with
`terraformstate.ScopeLocatorHash(BackendKind, Locator)`. That is the
scope-level join key used by the drift resolver.

Do not regenerate these with `terraformstate.LocatorHash`. That function also
includes `VersionID` and is for per-candidate identity, so it will not match
the scope-level hash even when `VersionID` is empty.

If the scope-hash formula changes, update the header comments and pinned values
in `seed.sql`; otherwise the verifier will not find the expected state-snapshot
scopes.

## Where It Is Asserted

`scripts/verify_tfstate_drift_compose.sh` applies `seed.sql`, reruns
bootstrap-index, waits for `config_state_drift` work to drain, checks the
per-kind drift counters, checks the ambiguous-owner log, and can write a proof
artifact through `ESHU_TFSTATE_DRIFT_PROOF_OUT`.
