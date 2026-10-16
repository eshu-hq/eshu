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
