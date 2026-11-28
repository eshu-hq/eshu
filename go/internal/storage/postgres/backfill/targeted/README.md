# Targeted deferred maintenance

This package runs the partition-scoped counterpart of deferred relationship
backfill. `Store` receives the parent's database, clock, worker settings,
phase check, memo gate, queue reopen, and correlation domains. The root
`IngestionStore` keeps the public entry point and builds this store.

The pass derives bounded queries once from shipped whole-pass SQL in
`backfill`. A missing or repeated marker fails at initialization. Evidence
writes preserve the backfill batch and fan-in paths. Reopen starts one
transaction, processes relationship and correlation domains, and commits once.
`deployable_unit_correlation` reopens through the derived changed-evidence
listing (#8283) and reports `skipped_unchanged` on its reopen block; the other
correlation domains keep their unconditional partition-scoped reopen.
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

The review fixes have fresh affected-path race proof: 14 selected top-level
tests passed with zero failures or skips and both root/child package terminals
passed. The six live child proofs include whole-versus-targeted differentials,
interleavings, outcomes, collision losers, closure, and shipped bounded reads.
The run also covers child configuration and tagged telemetry plus root wiring
and reopen telemetry. Its 21 differential fixtures had zero whole-only,
targeted-only, or outside-target changes, with 17 checked evidence additions
and 132 reopened items. The original 21-test run remains historical evidence;
the review-fix run does not claim to rerun all 21 top-level tests.
