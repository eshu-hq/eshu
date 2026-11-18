<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# 10909: retirement census row for the winners dirty staging queue

`supply_chain_impact_winners_dirty_staging` (migration 309, columns
`repository_id` and `scope_id`) had no row in the closed retirement
table census, so both live census tests failed on a fully migrated
schema. The row records disposition `kept` with no purge step, the same
disposition as its downstream sibling `supply_chain_impact_winners_dirty_log`.

## Why kept, proven from code

- Every capture owner appends to staging: `fact_upsert`
  (`captureWinnersDirtyFactsSQL`), `fact_retract` atomically with the
  tombstoning update (`writer_retract.go`), the activation and scanner
  triggers (migrations 310 and 311), and the audit queue.
- The purge itself writes here: the `BEFORE DELETE` trigger
  `ingestion_scopes_winners_dirty_capture_delete` on `ingestion_scopes`
  stages the leaving scope's finding identifiers (migration 310).
- Survivors drain without a purge step: each maintainer tick runs
  `collapseWinnersStaging` before admission, folding staged marks into
  the dirty log and deleting exactly the folded seqs; the keyed resweep
  then recomputes those keys. A scope-kind row naming a retired scope
  expands to no identifiers (its facts are gone) and settles as applied.
- A purge delete would lose durable capture: the scope-delete trigger
  recaptures only non-tombstone findings of the leaving scope, so a
  `fact_retract` mark for an already-tombstoned finding exists only as
  the staged row. Deleting it would strand that finding's stale winner.

## Evidence markers

No-Regression Evidence: data-only change to a census map literal plus
one hermetic pin test. `RetireTableDispositions` is a lookup table read
by the census join and the disposition accessor; the change adds one
map entry and edits one reason string, adds no query, worker, lease,
batch, concurrency knob, or runtime surface, and changes no execution
path. Baseline and after are identical by construction; the proof is
the test pair below, not a benchmark, because there is no measurable
runtime surface to time. RED/GREEN: with `ESHU_POSTGRES_DISPOSABLE=1`
against a disposable PostgreSQL 18,
`TestRetirementTableCensusClassifiesEveryScopeKeyedTable` and
`TestRetirementTableCensusSeededViolationProvesSensitivity` failed
before (one unclassified table: `supply_chain_impact_winners_dirty_staging
[repository_id scope_id]`) and pass after; the hermetic
`go/internal/reducer/retirement` suite passes.

No-Observability-Change: no new span, metric, log key, status field,
env var, or operator surface. The staging queue's existing collapse
counter and union depth/age gauges are untouched; the census row is
read only by tests and the disposition accessor.
