# Persistent stream-drop skip evidence (#8185)

A repository that lists a file it cannot open dropped the file from the fact
stream at every forced reconcile. The store cleared the pre-parse identity on
the drop, so the next equal snapshot read `identity_absent` and stored a new
full generation instead of skipping: one full parse and one stored generation
per interval, with no end. The fix records the dropped paths on the generation,
keeps the identity, persists the normalized set with a truncation flag, and
lets the skip probe re-open each recorded path before skipping. A path that
opens now parses to heal; still-failing paths keep the skip; an overflowed set
parses.

## Added work per forced reconcile

Only scopes whose active generation recorded drops pay anything extra, and only
when the skip attempt runs:

- The probe opens each recorded path (`os.Open`, no data read), at most 32,
  only when the active generation carries a recorded set.
- The stream notes each dropped path into a mutex-guarded set; the store
  marshals at most 32 paths once per committing drop.
- The commit UPDATE evaluates two more `jsonb` expressions on the same single
  row; the state read extracts two more payload keys from the same single row.

No-Regression Evidence: the added Go work measures in microseconds against a
work unit it replaces (a full parse and generation store). Baseline is the
pre-change code, which did no probe and no path recording; after is the head of
this PR. Backend is the repo Go toolchain on this host (AMD EPYC 9R14,
shared-host timing, stable within about 10% across 3 runs of 10000 iterations
each). Input shape is the worst case: a 32-path recorded set.

- Probe, all 32 paths still missing: about 66 microseconds per skip attempt.
- Probe, 32 paths with the last one present (opens all 32, then refuses):
  about 74 microseconds per skip attempt.
- Record, sort, cap, truncation check, and JSON marshal of a 40-path set:
  about 6 to 10 microseconds per committing drop.

Measured with scratch benchmarks on `recordedDropsStillFail`,
`StreamDroppedPaths.Sorted`, `Truncated`, and `json.Marshal` (removed before
commit; the behavior they time is pinned by `TestRecordedDropsStillFail`,
`TestStreamDroppedPathsTruncated`, and `TestStreamDropRecordsTheDroppedPaths`).

No-Regression Evidence: the two touched statements keep identical plans.
Backend is PostgreSQL 18.6 (Alpine) in a local container against a scratch
table with the production row shape (text primary key, `jsonb` payload).
`EXPLAIN (COSTS OFF)` on the old clear-identity UPDATE and the new
record-paths UPDATE both show a single-row primary-key Index Scan; the state
SELECT fragment likewise. `EXPLAIN (ANALYZE, TIMING OFF)` executes both
UPDATEs at about 0.16 milliseconds. The change adds payload expressions, not
rows, locks, or index access, and fires only when the stream actually dropped.

Observability Evidence: a skip that carries still-failing drops increments
`eshu_dp_collector_reconcile_parse_skip_persistent_drops_total` once (no
labels) and logs INFO `git_reconcile_parse_skipped_with_persistent_drops`
with `scope_id`, `generation_id`, `commit_sha`, `drop_count`, and
`dropped_paths`. A recorded drop that opens again logs INFO
`git_reconcile_parse_skip_refused_drop_returned` before the parse that heals
the generation. The per-interval rate of the counter is the count of scopes in
drop debt. Emission is covered by
`TestSkipParseSkipsWhenRecordedDropsPersist` (counter reads 1 after one skip).
