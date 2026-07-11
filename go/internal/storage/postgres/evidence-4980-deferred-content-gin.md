# Deferred Content Trigram GIN Evidence (#4980)

Performance Evidence: PostgreSQL 18 Alpine, 75,000 deterministic
`content_files` rows and 150,000 deterministic `content_entities` rows spread
across 896 synthetic repositories. The baseline created the production
`gin_trgm_ops` indexes before inserting; the candidate loaded identical rows,
then built the same indexes and analyzed both tables.

| stage | incremental GIN | deferred GIN |
| --- | ---: | ---: |
| file load | 5.271 s | 0.520 s |
| entity load | 10.058 s | 0.770 s |
| final file index build | included above | 2.393 s |
| final entity index build | included above | 4.138 s |
| final analyze | included above | 0.108 s |
| total | 15.329 s | 7.929 s |

The deferred shape reduced cold content load plus finalization by 48.3% on the
same input. Bulk-built index sizes were 14 MB versus 18 MB for files and 34 MB
versus 52 MB for entities.

No-Regression Evidence: full-row symmetric differences were 0/0 for both
tables. Far-tail `ILIKE` result-id differences were also 0/0. With the shipped
durable readiness function in each predicate, PostgreSQL planned a one-time
readiness filter followed by `Bitmap Index Scan` on
`content_files_content_trgm_idx` (76 matches, 4.382 ms) and
`content_entities_source_trgm_idx` (152 matches, 2.758 ms). A live lifecycle
test proved deferred schema starts `not_built`, unscoped reads fail closed,
finalization publishes `ready`, exact far-tail reads return 1/1, and a ready
rerun does not rebuild. Same-name B-tree and partial GIN indexes both remain
`failed`; removing either invalid shape and retrying heals the lifecycle to
`ready`.

Concurrency proof: while a content writer held `RowExclusiveLock`, the standard
non-concurrent index builder requested an ungranted `ShareLock` and waited.
After the writer committed, the builder acquired the lock and completed. Thus
an already-active writer completes before the build, and later writers cannot
mutate either table during its build. The normal Compose topology drains
source-local projection first; the PostgreSQL lock is the final exclusion
boundary, not a worker-count or serialization workaround.

A live two-finalizer test first disproved relying on
`CREATE INDEX IF NOT EXISTS`: simultaneous builders raced in `pg_class`, and
one failed with SQLSTATE `23505`. The finished implementation takes one
transaction-scoped, namespaced advisory lock before reading lifecycle state or
running either DDL statement. It commits `building` under that lock before the
DDL transaction, so operators and a replacement process can observe an
interrupted build. Repeating the same simultaneous two-finalizer test completed
both callers successfully in 3.50 s; the waiter rechecked the committed `ready`
state and performed no DDL. Crash recovery remains immediate because the next
finalizer reclaims a durable `building` state under the same lock.

Observability Evidence: `content_substring_index_state` persists the bounded
states `not_built`, `building`, `ready`, and `failed`, including build start,
completion, failure timestamps, and a bounded failure class. Bootstrap-index
emits start/terminal logs with `index_state`, `duration_seconds`, and
`content_substring_index_build_failure`; the existing
`eshu_dp_bootstrap_pipeline_phase_seconds` histogram records the total build,
validation, and `ANALYZE` phase under the bounded
`content_index_finalization` value.
