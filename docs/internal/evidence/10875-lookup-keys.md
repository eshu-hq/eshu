# Lookup Keys For Reducer Input Loads (#10875)

The reducer's busiest load finds cross-repository candidates by matching
about 1,029 alias/marker substrings and 1,024 repo_id values against every
fact payload in a partition (60-second QA sample: 8,588 calls at 12.7 ms
mean, 42% of database time). A key table already replaced the repo_id arm
(evidence-5088: 6,488 ms before, 2,936 ms after on 20,000 facts). This change
extends that table over the two remaining text-search arms and converts the
security-alert six-arm load the same way. Computation moves to fact-save
time: one extra row per value. Every stored value derives from the fact's
own envelope alone (#10861 D2), so a catalog change needs no rebuild.

## Theory proof (prior shim, hot cache)

Disposable PostgreSQL 18 seeded to the production scale drivers (2,474
scopes, 15,892 generations, 1029-term $1 and 1024-value $2/$7 binds,
~3KB payloads, $1 = 1029 terms, $2/$7 = 1024 values): the existing key
access (arm2 unnest x position nested loop, 69% of 3.1 s) is the cost
center, not the remaining text search (arm1 LIKE, 2%). Naive position-join
variants were rejected (+107% / +76%). The shipped design attacks arm2
with keyed equality over version-gated sidecar rows and keeps the legacy
text arms verbatim for rows without version-1 keys.

## What shipped

- Migration 315: version-1 columns on `relationship_reference_candidate_keys`
  (keys_version, fact kind, artifact/path norms, two content-marker bits,
  Flux bit, five unconditional-marker bits) plus the
  `security_alert_reconciliation_keys` exploded sidecar with partition and
  lookup indexes and its backfill marker table.
- Writer: same-pass derivation in the streaming save transaction
  (Deletes lock order: relationship, manifest, registry, security alert).
  Marshal failures propagate; empty token streams still write a row.
- Deferred load: keyed anchor (`position()` over raw $8 anchors) and marker
  (`um_*`) UNION arms on version-1 rows with the family predicate
  recomposed from sidecar columns; legacy arms kept byte-identical except
  the payload fallback skips versioned rows; per-partition fallback probe.
- Security-alert load: marker-gated EXISTS semi-join reader (no bare JOIN,
  so ORDER BY fact_id/LIMIT cursor paging stays exact); legacy query runs
  until the backfill marker exists.
- Rebuild: bounded resumable scope-paged backfill reusing the save-path
  refresh funcs, plus the explicit ready-marker setter.
- Delete coupling: the new table joins Deletes last, the orphan census and
  cleanup, the partition-index check, the 3 AM runbook fences, and the
  fact-delete key-kind guard.

## Signals

No-Regression Evidence: live PostgreSQL 18.6 differentials on seeded
fixtures — the deferred keyed load returns exactly the frozen pre-hoist
set (TestDeferredScopedFactLoadHoistExactlyEquivalentToPreHoistShape, 0/0
both directions, evidence agreement included); the keyed security-alert
load returns exactly the legacy six-arm set across scalar, array,
object-shaped, padded, tombstone, wrong-kind and noise cases
(TestListActiveSecurityAlertReconciliationFactsKeyedEquivalentToLegacy);
the backfill writes version-1 rows plus keys and reruns as a row-identical
no-op before the marker flips readiness
(TestLookupKeyBackfillRebuildsVersionedRowsAndKeys). The shipped EXPLAIN
shape stays on the sidecar partition index with semi-join anchor matching
and no sequential scan. Focused suites: 56 passing tests across the
postgres, backfill, keys and migrations packages, plus the updated guard,
retention, ingestion and memo suites. Not yet measured: deferred-load
buffers and batch-save wall delta on a production-scale copy (needs the
#10862 replay baseline), cold-cache toast traffic, and the keyless-tombstone
evidence question — the legacy fallback stays until that proof lands.

Observability Evidence: no new metric or label key and no new cardinality.
Two structured log lines carry the new state:
`event_name=deferred_relationship_key.fallback` with
`missing_versioned_key_rows` per partition load (silence is the steady
state; the first line after silence names the regression), and
`event_name=lookup_keys.backfill.scope` with facts, key rows and missing
counts per rebuilt scope (rows-per-fact save-cost evidence). No new
dashboard or alert; the existing loader and retention signals are unchanged.

## Follow-up cost measurement (#10971 P1)

No-Regression Evidence: shipped partition load matches legacy (2818ms vs 2955ms, identical 1550-fact set) on a migrated PostgreSQL 18 schema with a 2000-fact seed at production bind widths; probe sampling keeps steady-state telemetry near zero.

Observability Evidence: the new readiness_probe_failed signal line fires only on readiness-check errors with filter-class counts; the sampled fallback probe keeps the first-line-after-silence contract.

Method: `TestDeferredPartitionLoadCostLive` on a migrated disposable
PostgreSQL 18 schema with a 2000-fact partition at production bind widths
(1029 LIKE terms, 1024 repo values, ~3KB payloads), EXPLAIN ANALYZE medians
over 3 runs, hot cache. Frozen pre-#10875 SQL is embedded in the test as
the before side.

| Shape | Median latency | Result set |
|---|---|---|
| Legacy-only partition load | 2955ms | 1550 facts |
| Shipped partition load (legacy plus keyed arms) | 2818ms | 1550 facts, identical set |
| Fallback probe COUNT alone | 268ms | 0 missing on the backfilled seed |

The enlarged UNION costs nothing measurable (the shipped load is 5%
faster, inside run noise, with an identical set). The probe COUNT is the
material addition at ~10% of a partition load, so it is now sampled: the
first partition load of the process and every 64th run it
(`TestDeferredRelationshipKeyFallbackProbeIsSampled` pins 2 probes per 65
loads). Sampling never stops entirely because a later save path that
skips sidecars must still surface. Still open: cold-cache behavior at the
production call rate and the corpus-scale save-cost delta, which need the
#10862 replay baseline.

## Follow-up id-shape census (#10971 P2a)

Question: can a security-alert id field ever hold a scaled float
(`12345.0`) or a >2^53 integer, where the sidecar coercion provably
diverges from `->>`? Live PostgreSQL 18.6 renders `12345.0` scale-preserved
and `1e21` fully expanded, while the Go float64 path cannot reproduce the
scale. Exhaustive `jsonb_typeof` census over the QA environment (11,392
scopes, 88M facts; read replica, bounded indexed queries): all four scalar
id fields across all 7.7M security-kind facts are 100% strings
(`repository_id` 7,694,139; `package_id` 7,715,712; `cve_id` 7,720,036;
`advisory_id` 7,730,102; `cve_ids` elements 9,824 strings). Array elements
need no census: `?|` only matches string elements (verified live:
`[123] ?| ['123']` is false), so non-string elements are invisible to the
legacy arms and correctly ignored by the writer. Conclusion: the divergent
shapes do not occur in practice; the coercion stays as defense-in-depth
with its boundary pinned hermetically.
