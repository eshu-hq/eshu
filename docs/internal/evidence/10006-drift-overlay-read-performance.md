# #10006: Delta Overlay Cost for the Terraform Drift Readers

The config-state drift read took config and the backend anchor from the
active generation only, so a delta-active scope lost its base rows: the
anchor missed and every state address read as `removed_from_config`. The
fix routes the backend-facts, state-candidate, and config/module reads
through the shared `deriveDeltaOverlayBaseQuery` overlay: active pages
unchanged, one delta-target lookup, derived base pages only for reached
delta scopes, resolve, overlay inputs, all on one read-only
repeatable-read snapshot per leg.

## Performance Evidence:

Source: branch `drive/10006-tf-drift-overlay` (drift overlay commits).
Disposable PostgreSQL 18 (`postgres:18-alpine` container, default
settings) on a shared developer host, schema from `ApplyBootstrap`,
ANALYZEd. Fixture: full generation with `backend.tf` plus two resources,
delta touching one file, delta activated; three phases (base, delta,
delete) plus tombstone.

RED on active-only methods: base 1 canonical row, 2 config addresses, 0
`removed_from_config` in 6 statements; delta 0 rows vs 1, 0 addresses vs
2, 2 false `removed_from_config` vs 0 in 8 statements.

GREEN through the overlay: base 8 statements (targets 3, other 5, no
base/resolve/probe/inputs); delta and delete phases 17 statements
(targets 3, base 3, resolve 2, probe 2, inputs 2, other 5) with 1
canonical row at the live generation and 0 `removed_from_config`;
tombstone phase 19 statements (+1 prior walk, +1 prior prefix build):
the really-removed addresses surface as 2 true `removed_from_config`,
so only false positives are suppressed. `union_base_found` counts:
config 3, canonical 3 (one per delta phase).

EXPLAIN (ANALYZE, BUFFERS) after, fixture scale: canonical-active
Incremental Sort rows=0 hit=2 ~0.03ms; canonical-base rows=1 hit=9
~0.10ms; config-active rows=0 hit=3 ~0.02ms; config-base rows=1 hit=7
~0.03ms.

Why safe: the overlay adds statements only on delta-active scopes
(transient, one delta per scope at a time); the no-delta steady state
adds only the target lookup. The incorrect old read paid less because
it read less. Reopen bounds: steady state above +1 statement, a new
derivation seq scan, or production overlay-latency signals.

## Observability Evidence:

`eshu_dp_terraform_drift_read_delta_union_total` (read + closed
`union_*` outcomes) fires once per read reaching a delta scope, with a
span event per such read and a WARN line (scope, generation, both
commits) per refused base; asserted moving on the live overlay path
(`union_base_found` for config and canonical). Coverage rows in the
telemetry-coverage doc; the telemetry verifier agrees. Retraction
needs no new signal: findings stand until the next drift pass for the
same state rewrites and retires them (no cross-generation retraction).
