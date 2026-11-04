# #9957 winners counters: count after commit, count skipped audits

Refs #9957. Follow-up to [9805-winners-keyed-resweep.md](9805-winners-keyed-resweep.md).

## What changed on the hot path

The impact writer used to add to `eshu_dp_supply_chain_impact_winners_capture_rows_total`
while its statements ran. A pass that later rolled back, or whose commit failed,
had already moved the counter, so `fact_upsert` and `fact_retract` could exceed
the dirty-log rows that exist. The transaction wrapper now holds the counts and
adds them only when `Commit` succeeds; `Rollback` and a failed commit drop them.
The maintainer also counts an audit that found stale candidates but could not
take the resweep lock, in the new `eshu_dp_supply_chain_impact_winners_audit_skipped_total`.

No SQL, Cypher, lock, claim, lease or queue text changed. The only runtime
addition is a map of at most two keys per transaction (the source set is closed)
and one counter add per skipped audit.

## Measurement

No-Regression Evidence: the cost of holding and flushing the counts is
sub-microsecond per transaction, against statements that cost milliseconds.

| Item | Value |
| --- | --- |
| Harness | throwaway Go benchmark, not committed: build a `supplyChainImpactTx` over a stub transaction with real `telemetry.Instruments`, record `fact_upsert` and `fact_retract` once each, `Commit` |
| Command | `go test -run '^$' -bench ScratchRecordAndCommit -benchtime=2000000x -count=5 ./internal/storage/postgres/` |
| Toolchain and host | go1.26.10, darwin, 18 CPUs, load1 4.7 at the start of the run |
| Result, five runs | 434.3 to 437.5 ns/op, 688 B/op, 15 allocs/op |
| Input shape | one transaction with two sources recorded once each; production records each source once per pass |
| Baseline | NOT_CHECKED. The previous code made the same two counter adds immediately, so the added cost is the map and the deferred flush, inside the figure above |

The whole figure is below one microsecond per impact-writer pass. A pass
runs statements against Postgres, so this does not move any measured path in
[the scale note](9805-winners-keyed-resweep-scale.md). Per-pass allocation
grows with passes, not with rows.

Concurrency: the held counts are written and read only on the goroutine that
owns the transaction. The beginner builds a new wrapper per attempt, so a queue
retry after a rollback starts empty and counts only its own rows. The audit
counter is an OpenTelemetry counter and is atomic.

## Operator signals

Observability Evidence: the counters and their meaning are in
[the winners telemetry page](../../public/reference/telemetry/supply-chain-impact-winners.md).

- `eshu_dp_supply_chain_impact_winners_capture_rows_total{source}`: now equal to
  the dirty-log rows that exist for `fact_upsert` and `fact_retract`, apart from a
  commit whose outcome is unknown.
- `eshu_dp_supply_chain_impact_winners_audit_skipped_total`: rises when an audit
  with candidates lost the lock. The page gives the alert shape
  (`sum(increase(...[1h])) >= 3`) and says one skip is harmless.
- Proof of the counting contract:
  `TestSupplyChainImpactTxCountsCaptureRowsOnlyAfterCommit` (committed,
  rolled-back and failed-commit cases, and accumulation of two calls for one
  source) and the maintainer's audit-skipped tests in
  `winners_keyed_maintainer_test.go`.
