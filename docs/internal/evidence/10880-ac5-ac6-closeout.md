# #10880 AC5/AC6 closeout (Lane D, docs-only)

**Refs #10880. This report satisfies the duty to report AC5 honestly (updates-to-zero by construction, index count unchanged, save time NOT_CHECKED) and confirms AC6 verbatim; it closes nothing — not #10880, not AC3, not AC4, not AC5-as-measured.**

Lane D base: `origin/main` at `8b7156f110`. Lane D edits docs only: this
file, one in-place row fix in `postgres-tuning.md`, one measured-stores note
in `capability-catalog.md`, and a one-line pointer in the design Execution
record. No production code touched; no `make pre-push` / `make pre-pr` run
in this lane (coordinator owns the floor).

## Summary

Refs #10880. Date: 2026-11-28. The question was whether the
supply-chain-impact family's fact-table updates, index count, and save time
(AC5) can be reported from the existing write-stop proof ledger, and whether
the remaining-family order (AC6) still holds. The ledger shows finding-fact
writes at zero by construction (reject trigger 358), all 12 impact-dedicated
fact indexes retained with no drop migration landed, and no comparable
save-time run anywhere — so AC5 is reported-but-partial with save time
NOT_CHECKED, AC6 is confirmed unchanged, and this note closes nothing.

## AC5 report: fact-table updates, index count, save time

| Metric | Before | After | Boundary / source | Comparable? |
| --- | --- | --- | --- | --- |
| Finding-fact writes for `reducer_supply_chain_impact_finding` | Batch upsert per finding per (scope, generation) via `factwrite` (`ON CONFLICT (fact_id)`, fencing-token guard); quantitative rate NOT_MEASURED in any Lane B output | 0 by construction: migration `358` trigger rejects every INSERT/UPDATE with SQLSTATE `55000` (`fact_records_reject_supply_chain_impact_finding`); corroborated live by 6/6 reader differentials + 15/15 query readers passing with zero finding facts | Before: design write path (verified code read); After: DDL in `go/internal/storage/postgres/migrations/358_supply_chain_impact_finding_fact_write_stop.sql` + Lane B proof at `230e2764b3` | No: After is a by-construction zero, not a measured delta |
| `fact_records` index count for this family | 12 impact-dedicated indexes live (listed below); 152 total on `fact_records` per the 2026-11-17 issue census (149 partial, 115 on expressions) — live-unverified | 12 retained, UNCHANGED: no drop migration for any of the 12 has landed (293 drops only the legacy `package_lookup` triple index, retired by #10544) | Measured at HEAD `8b7156f110` from migration files + `facts/impact/indexes.go`; total from #10880 issue evidence, dated | No (Before total is a dated census, not a same-day A/B) |
| Save (collector/reducer write) time for this family | NOT_MEASURED | NOT_MEASURED | No comparable before/after collector-or-reducer save-time run exists in any Lane B output | No — NOT_CHECKED, owed by a future lane |
| Buffer-parity context (NOT save time) | 61789 vs 63943 buffers (1.035, budget-2000); 408314 vs 409921 (1.004, budget-10000) | Same run (single 2b leg, disposable Postgres 18, 26.77s) | Lane B proof at `230e2764b3` | Buffers only; never seconds, never a speedup |

Live impact-dedicated `fact_records` indexes at HEAD (12, all
`CREATE INDEX CONCURRENTLY IF NOT EXISTS`, none dropped except as noted):

- `..._active_scan_idx` (003), `..._lookup_idx` (003),
  `..._priority_lookup_idx` (003), `..._repository_lookup_idx` (003),
  `..._status_lookup_idx` (003)
- `..._finding_id_idx` (084), `..._package_id_idx` (218),
  `..._payload_scope_id_idx` (225), `..._foreign_key_guard_idx` (229)
- `..._advisory_lookup_idx` (288), `..._group_members_idx` (289),
  `..._winners_keyed_idx` (291)
- Dropped: `..._package_lookup_idx` (created 003, dropped 293 per #10544;
  the 291 keyed index covers the same triple prefix).

The design's "ten" (Decision 1, slice 4) predates 218/225/229; the
slice-4 `152→142` After figure is therefore FORBIDDEN here — no
read-replica census at execution time exists and no drop migration has
landed. The 152 total keeps its 2026-11-17 date.

## AC6: the order the other families move (confirmed, no reorder)

Verbatim from the design ("Remaining families, in migration order"): (1)
security-alert reconciliations; (2) drift findings as one group of four
(AWS, multi-cloud, Terraform config-state, code-drifted); (3) package
correlations (mixed declaration likely — package identity is
graph-resident); (4) CI/CD + Kubernetes + service-catalog +
observability-coverage + workload-identity correlations together.
`reducer_cloud_asset_resolution` does not move (reducer-internal, no read
surface). Slice 4 (replica census + index drop) for THIS family still
precedes claiming the family fully moved. AC6 is the only AC this lane
marks met.

## Disposition: reported-but-partial

- AC5: reported with honest NOT_CHECKED save time; NOT ticked as measured.
- AC3 (golden-corpus equality): NOT PROVEN — gate image `nornicdb 1.3.3`
  is below the #11535 floor `1.5`; gate-owner pin bump owed. #11598 (QA
  winners re-mark storm) stays OPEN and must be dispositioned first.
- AC4 (read latency): RED pre-existing, docs-only-delta-proven — story
  route 307.0 > 197 calls; winners-truth leg vacuous post-write-stop.
  Two separate defects owed; no Lane D fix.
- B-12 replay: NOT RUN — hardcodes a root `docker-compose.yaml` gone
  since #8871; gate-owner re-point owed.
- Merged scope this report rests on: #11188 (winners reads, default-off
  gate), #11251 (layer-2 audit), #11271 (supports table), #11476 (write
  stop, trigger 55000) — all MERGED.
- Corrections owed outside Lane D scope: `go/internal/query/read-models.md`
  still says aggregates "re-use the existing partial indexes on
  `fact_records`" — proposed replacement is recorded below; the query
  lane lands it. AC1 enforcement stays partial: layer-2 audit live
  (#11251), waiver burn-down OPEN (#11212), graph/content reads not
  recorded — the catalog must not claim full enforcement.

Proposed `read-models.md` replacement (query lane to land): aggregates,
grouped inventory, and explain read `supply_chain_impact_finding_supports`
(and the winners table behind the default-off gate), not the legacy
`fact_records` partial indexes; the gate-off legacy list path is the only
remaining finding-fact reader.

## Excluded evidence (not comparable, not claimed)

Design-doc shims (92 ms vs 260 ms @300k, 9.57/7.56 vs 12.11/9.61 ms @2k,
resweep 92 ms, 1,136 B payload), #11271 synthetic WAL percentages, QA
121 GB / 88 M rewrites as After state, and every Lane B wall-time figure
(2b 26.77 s, digest 7.03 s, T-phase 5.28/1.43/4.21/0.69 s, query 34.1 s —
disposable-Postgres-18 test durations on differing boundaries) are NOT
save time and NEVER a speedup. They appear here only to forbid their use.

## Number trace (every figure → source)

| Figure | Source |
| --- | --- |
| 0 finding-fact writes (by construction) | Migration 358 trigger text + Lane B 6/6 + 15/15 at `230e2764b3` |
| 12 impact indexes retained, names above | Migration files 003/084/218/225/229/288/289/291/293 at HEAD `8b7156f110` |
| 152 / 149 partial / 115 expression / 2026-11-17 | #10880 issue evidence (live-unverified) |
| Buffer parity 1.035 / 1.004 | Lane B leg 2b at `230e2764b3` (buffers, not seconds) |
| Latency 307.0 > 197; vacuous winners-truth leg | Lane B proof at `230e2764b3` |
| nornicdb 1.3.3 < 1.5 floor; B-12 compose gone | Lane B proof at `230e2764b3` |
| Answering stores below | `catalog.generated.json` at HEAD `8b7156f110`, re-parsed at writing time |
| #11188, #11251, #11271, #11476 MERGED; #11212, #11598 OPEN | `gh` API reads at writing time |

Measured answering stores at HEAD (162 entries; impact capabilities still
mixed — none is winners-only):

- `supply_chain.impact_findings.list` →
  `read_table:supply_chain_impact_finding_supports`,
  `read_table:supply_chain_impact_canonical_winners`,
  `fact_records_reducer`, `fact_records_collector` (+ readiness tables)
- `supply_chain.impact_findings.aggregate` →
  `read_table:supply_chain_impact_finding_supports`,
  `read_table:supply_chain_impact_canonical_winners`,
  `fact_records_reducer`
- `supply_chain.impact_explanation.read` →
  `read_table:supply_chain_impact_finding_supports`,
  `fact_records_reducer`, `fact_records_collector` (+ readiness tables)
- `supply_chain.advisory_evidence.list` →
  `read_table:supply_chain_impact_finding_supports`,
  `fact_records_collector`, `fact_records_reducer`

No-Observability-Change: this lane adds no metric, span, log, route, or knob.
