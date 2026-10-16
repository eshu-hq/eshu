# Targeted deferred maintenance

This package runs the partition-scoped counterpart of deferred relationship
backfill. `Store` receives the parent's database, clock, worker settings,
phase check, memo gate, queue reopen, and correlation domains. The root
`IngestionStore` keeps the public entry point and builds this store.

The pass derives bounded queries once from shipped whole-pass SQL in
`backfill`. A missing or repeated marker fails at initialization. Evidence
writes preserve the backfill batch and fan-in paths. Reopen starts one
transaction, processes relationship and correlation domains, and commits once.
Callbacks use the supplied database or transaction handle. The full pass
rejects a missing phase, gate, or reopen callback, blank reopen query, or
blank/duplicate correlation domain before starting a span or database read.
The exported reopen entrypoint checks its transaction and reopen collaborators
before `Begin`; an empty affected set remains a no-op. The root supplies the
domain list and all production collaborators.

Live differentials call the actual root whole and targeted entry points
against isolated PostgreSQL schemas. Shared schema, seed, snapshot, and
comparison logic lives in `postgres/testfixtures`. The proofs cover closure,
SQL rows, outcomes, interleavings, collision losers, and unchanged memo hits.

No metric name, log key, span name, worker setting, lock order, transaction
boundary, or retry behavior changes in this move. The targeted pass still
passes nil instruments to the memo gate.

## Move evidence (#7648)

No-Regression Evidence: the baseline is `ccd5e023`, before the family moved.
Baseline and moved proofs ran on PostgreSQL 18.6 with isolated bootstrap
schemas. Both arms use the real root entry points and the existing batch size
and worker settings of each fixture. The original move proof at b0cdc01395895ddc7eae1aa3794140df7d0b0220 recorded
21 race-enabled differential summaries; there were zero whole-only rows in compared partitions, zero
targeted-only rows, and zero targeted changes outside them. The fixtures
reported 17 new evidence edges and 132 reopened work items, each checked
against its explicit expected tuples. That original selected run ended with 21 passes,
zero failures, zero skips, and both package terminal passes. The old and new
engine/adapter test-name sets contain the same 18 names. The three shipped
reopen queries, five replay-floor fragments, and catalog baseline SQL resolve
byte-for-byte equal to the baseline. Earlier elapsed samples overlap a cache
copy and are not a performance comparison. This move makes no speedup claim.

The review fixes have fresh affected-path race proof: 14 selected top-level
tests passed with zero failures or skips and both root/child package terminals
passed. The six live child proofs include whole-versus-targeted differentials,
interleavings, outcomes, collision losers, closure, and shipped bounded reads.
The run also covers child configuration and tagged telemetry plus root wiring
and reopen telemetry. Its 21 differential fixtures had zero whole-only,
targeted-only, or outside-target changes, with 17 checked evidence additions
and 132 reopened items. The original 21-test run remains historical evidence;
the review-fix run does not claim to rerun all 21 top-level tests.

Benchmark Evidence: the retained fext-config-bench run measures the valid-store configuration check adds a median 97.1 ns
per pass over the old DB/Beginner/normalized-owed checks in five 300 ms
microbenchmark rounds on Linux arm64 with 16 CPUs. The prefix measured
139.9 ns/op, 56 B/op, 2 allocs/op; the prefix plus callback, shipped-query,
and four representative correlation-domain checks measured 237.0 ns/op,
56 B/op, 2 allocs/op. `ProductionQueries` supplied the query strings; the
four domain strings matched the root list's count and length range without
copying its membership. This measures the guard only, not pass wall time.

No-Observability-Change: the six targeted production signal literals are
identical to `ccd5e023`. The pass still records its outcome on the same span,
metrics, and log line, and passes nil instruments to the reopen block and memo
gate. The engine telemetry proof checks one tagged reopen block per domain,
its reopened and skipped counts, and the shared transaction. Operators retain
the same `deferred_backfill_targeted_completed` line and targeted metric series.
