# tfstate Drift Tier-1 Fixture

Fixture corpus for `scripts/verify_tfstate_drift_compose.sh`.

Tier 1 seeds Postgres with Terraform config and state facts, then reruns
bootstrap Phase 3.5 so the reducer handles `config_state_drift` work. It
exercises the drift handler and evidence loader without the Terraform-state
collector. Tier 2 lives in `../tfstate_drift_tier2/`.

## File Map

| File | Purpose |
| --- | --- |
| `seed.sql` | Idempotent rows for the six drift scenarios. |
| `expected/added_in_state.json` | Expected counter/log shape for bucket A. |
| `expected/added_in_config.json` | Expected counter/log shape for bucket B. |
| `expected/removed_from_state.json` | Expected counter/log shape for bucket C. |
| `expected/ambiguous_owner.json` | Zero-counter and rejection-log shape for bucket D. |
| `expected/attribute_drift.json` | Expected counter/log shape for bucket E. |
| `expected/removed_from_config.json` | Expected counter/log shape for bucket F. |

The `expected/*.json` files are review aids. The verifier asserts the same
behavior inline.

## Scenarios

| Scenario | Drift kind | Fixture signal |
| --- | --- | --- |
| `drift-tfstate-added-in-state` | `added_in_state` | State has `aws_s3_bucket.unmanaged`; config has no matching block. |
| `drift-tfstate-added-in-config` | `added_in_config` | Config has `aws_s3_bucket.declared`; state has no resources. |
| `drift-tfstate-removed-from-state` | `removed_from_state` | Prior state serial 1 has `aws_s3_bucket.was_there`; current serial 2 does not. |
| `drift-tfstate-ambiguous-{a,b}` | rejection | Two repos claim the same backend, so ownership is ambiguous. |
| `drift-tfstate-attribute-drift` | `attribute_drift` | Config and state both have `aws_s3_bucket.logs`, but the allowlisted SSE value differs. |
| `drift-tfstate-removed-from-config` | `removed_from_config` | State still has `aws_iam_policy.legacy`; current config removed it after a prior generation declared it. |

Expected truth:

- Module-nested addresses are outside this corpus.
- Drift is not graph-projected here; counters and structured logs are the
  assertion surface.
- `seed.sql` pins `state_snapshot:s3:<hash>` scope IDs computed with
  `terraformstate.ScopeLocatorHash(BackendKind, Locator)`. Do not replace them
  with `terraformstate.LocatorHash`.
- The verifier applies `seed.sql`, reruns bootstrap-index, waits for
  `config_state_drift` drain, checks per-kind drift counters, and checks the
  ambiguous-owner log.
