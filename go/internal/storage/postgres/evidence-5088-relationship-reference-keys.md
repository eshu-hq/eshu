# Relationship Reference Candidate Keys Evidence (#5088)

Performance Evidence: PostgreSQL 18 Alpine, 20,000 deterministic active
relationship-candidate facts, 896 catalog repository IDs, and 1,000
cross-repository references. The baseline used the current lowercased-payload
repo-id regex arm for each candidate catalog value. The candidate populated the
`relationship_reference_candidate_keys` side table once, then tested whole-token
repo-id keys against the precomputed token stream for each selected fact.

| shape | selected facts | execution time |
| --- | ---: | ---: |
| regex payload scan | 1,000 | 6,487.757 ms |
| precomputed reference key scan | 1,000 | 2,936.490 ms |

The candidate reduced the production-shaped fact-load predicate by 54.7% on the
same input. Populating the side table for 20,000 accepted facts took 412 ms.
The side table stores one unindexed token-stream row per candidate fact and
keeps only `fact_id` in the primary key, so large payload-derived token streams
do not become btree index entries.

No-Regression Evidence: old and new selected fact sets matched exactly:
`old_count=1000`, `new_count=1000`, `old_minus_new=0`, and `new_minus_old=0`.
`go test ./internal/storage/postgres -run
TestDeferredScopedFactLoadHoistExactlyEquivalentToPreHoistShape -count=1 -v`
passed against a live PostgreSQL 18 Alpine container after seeding key rows. It
proves the new fast path preserves the existing carve-outs for prefix-overlapping
repo IDs, ArgoCD unconditional over-select markers, empty cloud-scope `repo_id`
values, and orphan rows whose own repo ID differs from the catalog but whose
payload references a real target. `go test ./internal/relationships
./internal/storage/postgres -count=1`, `go test ./internal/reducer -run
TestPayloadUsageManifest -count=1`,
`go test ./internal/payloadusage ./cmd/payload-usage-manifest -count=1`, and
`scripts/test-verify-performance-evidence.sh` passed after the side-table
writer, migration, scoped-load query, and payload-usage exemption were added.

Observability Evidence: the change adds no metric instrument, metric label,
span name, worker, queue domain, lease, route, runtime knob, or status surface.
Operators continue to diagnose relationship backfill through the existing
`deferred_backfill_fact_load_task_completed`,
`deferred_backfill_fact_load_completed`, `deferred_backfill_batch_committed`,
and `deferred_backfill_completed` structured logs, plus existing Postgres query
instrumentation when the store is wrapped by the standard Postgres adapter. The
new table is committed in the same accepted-fact transaction path as
`fact_records`; stale or rejected envelopes therefore cannot publish candidate
keys that the deferred load later treats as authoritative.
