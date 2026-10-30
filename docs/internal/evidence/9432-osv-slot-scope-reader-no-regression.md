# OSV slot scopes: reader projection no-regression (#9432)

## What changed at runtime

`FactStore.ListOwnedPackageDependencyTargets`
(`go/internal/storage/postgres/owned_package_targets.go`) projects one more
output column, `window_targets.key_hash_low24`, and scans it into the new
`workflow.OwnedPackageDependencyTarget.KeyHashLow24` field. The column is the
same expression the `numbered_targets` CTE already computes for the #9422
slot predicate (`get_byte(key_hash, 0) + 256 * get_byte(key_hash, 1) +
65536 * get_byte(key_hash, 2)` over `sha256(eco|pkg|ver)`); no predicate,
join, sort key, or limit changed. The vulnerability planner groups slot
scopes on the returned hash and never rehashes in Go. The plan span
`vulnerability_intelligence.plan` (see Observability Evidence) carries only
counts.

Scope, limits and cardinality:

- Scope: the same predicate as before (active generation,
  `content_entity`, `git`, `Variable`, `config_kind = dependency`, the
  configured `package_manager` values). The scan is unchanged.
- Limit and P: unchanged. The measurement below used target limit 150 on a
  230-target fixture (P = 4, slot 0 holding 51 rows).
- Timeout: no new statement timeout.
- Cardinality: the projection adds one 4-byte integer per returned row; the
  hash itself still runs once per distinct target inside the existing CTE.

## Measurement

No-Regression Evidence: a throwaway shim test (deleted after capture, never
part of the change) ran `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, COSTS OFF)`
on the pre-change projection (the shipped statement minus the one output
column; the shim asserted the removal hit exactly once and left the two
in-CTE `key_hash_low24` references intact) against the shipped projection on
the same isolated-schema fixture (200 npm + 30 rubygems targets, version
1.0.0, `$1={npm,rubygems} $2=1000 $3=0 $4=150`), on local PostgreSQL 18.6,
2026-10-29. One run each, same cache-warm session:

| Variant | Execution Time | Buffers (execution) | Plan nodes |
| --- | --- | --- | --- |
| Before (11 output columns) | 1.470 ms | shared hit=933 | baseline |
| After (12 output columns) | 1.406 ms | shared hit=933 | identical modulo output list |

The node shapes diffed identical after stripping row counts, memory, and
planning-time noise (the only delta was catalog-cache planning buffers,
hit=365 read=40 vs hit=121, which is session warmth, not the statement).
The delta (-0.064 ms) is noise on a 1.4 ms statement: projecting an
already-computed column adds no plan node and no heap read.

Why this generalizes: the added column is computed unconditionally by the
numbered_targets CTE whether or not the final SELECT projects it (the slot
predicate and the slot_size window both read it), so the extra work is
exactly serializing one integer per returned row. The live test
`TestListOwnedPackageDependencyTargetsReportsKeyHashLive` pins the returned
values against an independent Go sha256 oracle, and the #9426 differential
(`TestListOwnedPackageDependencyTargetsSlotDifferentialLive`) proves the
read still follows active generations with identical output.

## Operability

Observability Evidence: `vulnerability_intelligence.plan` span attributes
`slot_scopes_enabled`, `slot_scope_count`, `slot_scope_max_chunk`,
`slot_scope_fallback` (counts only, never scope ids), beside the unchanged
`owned_package_targets.read` span, its window event, and the coordinator
window log. When a slot claim reaches the source-state recorder before the
claim transport lands, it logs one warning with work identity (no scope id)
and records no package keys, so readiness reads pending instead of joining
wrong keys. Operator readback after cutover:

```sql
SELECT scope_id, COUNT(*) FROM scope_generations
WHERE status = 'active' AND scope_id LIKE 'vuln-intel://osv/%/_slot/%'
GROUP BY scope_id;
```

One row per scope with count 1, beside the frozen legacy `_batch` actives
that #9431 retires separately.
