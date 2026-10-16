# Targeted deferred maintenance

This package runs the partition-scoped counterpart of deferred relationship
backfill. `Store` receives the parent's database, clock, worker settings,
phase check, memo gate, queue reopen, and correlation domains. The root
`IngestionStore` keeps the public entry point and builds this store.

The pass derives bounded queries once from shipped whole-pass SQL in
`backfill`. A missing or repeated marker fails at initialization. Evidence
writes preserve the backfill batch and fan-in paths. Reopen starts one
transaction, processes relationship and correlation domains, and commits once.
Callbacks use the supplied database or transaction handle.

Live differentials call the actual root whole and targeted entry points
against isolated PostgreSQL schemas. Shared schema, seed, snapshot, and
comparison logic lives in `postgres/testfixtures`. The proofs cover closure,
SQL rows, outcomes, interleavings, collision losers, and unchanged memo hits.

No metric name, log key, span name, worker setting, lock order, transaction
boundary, or retry behavior changes in this move. The targeted pass still
passes nil instruments to the memo gate.

## Move evidence (#7648)

No-Regression Evidence: the baseline is `ccd5e023`, before the family moved;
the after source is this package on PostgreSQL 18.6 with isolated bootstrap
schemas. Both arms use the real root entry points and the existing batch size
and worker settings of each fixture. In 21 final race-enabled differential
summaries, there were zero whole-only rows in compared partitions, zero
targeted-only rows, and zero targeted changes outside them. The fixtures
reported 17 new evidence edges and 132 reopened work items, each checked
against its explicit expected tuples. The selected run ended with 21 passes,
zero failures, zero skips, and both package terminal passes. The old and new
engine/adapter test-name sets contain the same 18 names. The three shipped
reopen queries, five replay-floor fragments, and catalog baseline SQL resolve
byte-for-byte equal to the baseline. Earlier elapsed samples overlap a cache
copy and are not a performance comparison. This move makes no speedup claim.

No-Observability-Change: the six targeted production signal literals are
identical to `ccd5e023`. The pass still records its outcome on the same span,
metrics, and log line, and passes nil instruments to the reopen block and memo
gate. The engine telemetry proof checks one tagged reopen block per domain,
its reopened and skipped counts, and the shared transaction. Operators retain
the same `deferred_backfill_targeted_completed` line and targeted metric series.
