# 10451 lane B: staged winners compaction and superset diff

Refs #10451.

Lane B compacts the per-(scope, generation) staged batches (lane A) into
per-key source winners and operator overrides in memory, fetches the stored
winners row, and diffs the compacted set as a superset of the SQL FULL JOIN
diff. Production calls none of the new functions yet. The coordinator wires
lane C's cap constants and the tick runner at integration.

## What changed

Two new files beside lane A's staged-scan file, no edits to any existing
path: `supply_chain_impact_winners_staged_compact.go` (`StagedCompactor`
with `AddSourceBatch`/`AddOverrideBatch`, `FetchStoredWinners`,
`DiffCandidates`, `StagedCompactionOverBudgetError`) and its test. Caps
arrive as constructor parameters, so no new cross-package import exists.
Go never reimplements `pg_input_is_valid` or the severity CASE: any key
with an operator override present is always emitted, and winner-payload
wide fields are pinned by `winner_fact_id`.

## Measurement

No-Regression Evidence: no evaluated SQL, index, queue, lease, worker, or
concurrency knob changes. The stored-winners fetch is one
`SELECT ... FROM supply_chain_impact_canonical_winners ORDER BY
canonical_key ASC`, which rides the table's primary-key btree. Focused
suite `go test ./internal/storage/postgres/ -run
'Compact|DiffCand|Staged' -count=1` passes (15 pass, 0 fail), `go vet
./internal/storage/postgres/` is clean, and the RED-first run failed to
build on the undefined lane-B symbols before the implementation landed.
The scale test compacts 400,000 synthetic rows across 20 batches into
50,000 distinct keys in 197 ms on the arm64 worktree host (live footprint
is the per-key map only; peak distinct keys and wall time are logged, no
hard memory assert — the 256 MiB heap gate runs at integration).

No-Observability-Change: no spans, metrics, logs, status, or pprof change.
The new code emits no telemetry and adds no statement to any existing
path. Operator diagnosis stays with lane C's tick telemetry.
