# Workload Candidate Probe: Defect Proof And Plan Evidence (#9986)

The #9697 pre-gate probe (`FactStore.ScopeMayHoldWorkloadCandidates`) was a
SQL syntax error, so the bulk-load shed from #9700 never fired. This note
records the proof of the defect, the proof of the fix, and the first plan
evidence for a statement that had never executed against a real planner.

## Defect proof

`workloadCandidatePreconditionQuery()` wrapped each signal key in single
quotes and joined them inside a single-quoted array literal, which rendered
`?| '{'k8s_resources','…'}'`. The existing live test
(`TestScopeMayHoldWorkloadCandidatesLive`, class `scheduled`, never run
green) was run unchanged against a private native PostgreSQL 18.6 cluster on
the unfixed text:

```text
ScopeMayHoldWorkloadCandidates: probe workload candidate precondition:
ERROR: syntax error at or near "k8s_resources" (SQLSTATE 42601)
```

All 8 subtests failed. After the matrix grew to 27 cases the unfixed text
failed all 27 (26 subtests plus the parent) with the same SQLSTATE.

## Fix proof

The keys now travel as one bound `text[]` parameter (`$3::text[]`, encoded by
`storage/postgres/array.StringArray`); no key text is interpolated.

| check | result |
| --- | --- |
| live test, fixed text, 27 cases (13 per-signal-key positives derived from `factload.WorkloadCandidateSignalKeys`, other generation, unknown generation, empty `graph_id`, tombstone, repo-only, foreign `repo_id`, nameless repository, signal-free payload, bare Jenkinsfile path) | 27 pass, exit 0 |
| mutant: quoting back to `'{'k'}'` | killed (29 failures) |
| mutant: bind only the first key | killed (18 failures) |
| mutant: drop the key argument | killed (28 failures) |

The live matrix corrected one row the SQL had never been able to check:
`{"dockerfile_stages":[]}` was expected to probe false, but the shipped
predicate is key existence and its contract calls it a structural superset,
so an empty bucket counts. The row now expects true. The predicate is
unchanged.

```bash
cd go && ESHU_POSTGRES_TEST_DSN='postgres://postgres@127.0.0.1:55986/eshu9986?sslmode=disable' \
  go test -tags integration ./internal/storage/postgres/ -run TestScopeMayHoldWorkloadCandidatesLive -count=1 -v
```

## Performance Evidence

Performance Evidence: baseline none (the statement never executed in
production; the comparison is the 89.195 ms DB-side bulk-load read of the same
scope). After: 0.3 to 4 ms for ordinary scopes and 240 to 279 ms for a
300,000-file scope with no signal or the signal last, on PostgreSQL 18.6, over
5,253,002 synthetic `fact_records` rows. Terminal result: all six scenarios
return the expected boolean. The change is safe because the predicate is
unchanged, only the key transport changed, and the plan is bounded by the
scope generation.

Stage: the workload projection input loader's pre-gate probe, one statement
per workload-materialization intent. Metric boundary: server-side
`EXPLAIN (ANALYZE, BUFFERS)` execution time of the shipped statement. No
baseline exists because the statement never executed in production; the
comparison is the DB-side read of the bulk load that follows it.

Setup: a private native PostgreSQL 18.6 cluster (`shared_buffers=1GB`,
`fsync=off`, Apple silicon), all 301 repository `.sql` migrations applied (146
indexes on `fact_records`), then 5,253,002 synthetic rows (5,990 MB total,
3,423 MB heap): 3,002 repository facts, 1,050,000 file facts, 3,600,000
`content_entity` facts, 600,000 `content` facts. 3,000 ordinary scopes hold
150 files and 1,400 entities each. Two monorepo scopes hold 300,000 files
each (about 1.7 KB of `parsed_file_data` per file). The SQL under test is
extracted by script from the shipped Go constant, and the key list from
`factload.WorkloadCandidateSignalKeys`. The data is clustered by scope on
disk, so production heap scatter can only add I/O (NOT_CHECKED).

The planner uses `fact_records_scope_generation_idx (scope_id, generation_id,
fact_kind, observed_at DESC)` or `fact_records_scope_generation_keyset_idx
(scope_id, generation_id, observed_at, fact_id)`. It never scans the table
sequentially, so cost is bounded by the scope generation, not the table.

Scan order: the keyset index is ascending on `observed_at`. A signal on file
1 is first in scan order; a signal on file 150 is last.

| scenario | plan mode | time ms | top-node buffers |
| --- | --- | --- | --- |
| true, signal on the last of 150 files (`scope-00002`) | custom (cold) / generic | 4.146 / 0.809 | hit 309 read 1578 / hit 1887 |
| true, signal on the first file (`scope-00001`) | custom / generic | 0.733 / 0.336 | hit 1457 read 127 / hit 1584 |
| true, every file carries an empty bucket (`scope-00004`) | custom / generic | 0.976 / 0.425 | hit 1328 read 258 / hit 1586 |
| false, 150 files, no signal (`scope-00003`) | custom / generic | 0.685 / 0.504 | hit 3136 read 24 / hit 3160 |
| false, 300,000 files, no signal | custom / generic | 268.069 / 270.265 | hit 5004 read 27278 / hit 58351 read 3799 |
| true, 300,000 files, signal on the last file in scan order | custom / generic | 260.834 / 278.834 | hit 32285 / hit 70327 |

Reading the plans:

- Ordinary scopes finish in about 0.3 to 4 ms. The 4.146 ms row is a cold
  first run with 1,578 buffer reads.
- The worst cases are inherent to the predicate. A `false` answer, or a signal
  at the end of the scan, must test `parsed_file_data ?| keys` on every file
  payload of the scope, because no index covers parsed keys: about 270 ms at
  300,000 files, warm. The EXISTS does stop at the first matching pair.
- A `false` answer does not shed a load: the caller continues to the normal
  load. The DB-side read of the same 300,000-file scope that follows
  (`SELECT fact_id, fact_kind, payload … fact_kind = ANY(…)`) took 89.195 ms
  warm. A candidate-free monorepo therefore pays the probe on top of a load it
  could not shed, about 3 times the DB-side read. The probe saves Go-side
  decode and allocation only when the answer is `true` and the fence is
  closed. This is a design property of #9697, not a defect of the fix; the
  new counter shows the true/false mix on QA.
- The repository lookup sometimes scans the whole scope generation. In the
  generic plan the planner prices the keyset index at one row with `fact_kind`
  as a filter, and it removed 300,000 rows (35,164 buffers, 50 ms warm,
  162 ms cold in the first custom run) to find one repository fact. The cause
  is the repository fact sorting late in `observed_at` order in this data. Its
  position in production is NOT_CHECKED.

Smallest fix, not applied, with no change to predicate semantics: a partial
index `ON fact_records (scope_id, generation_id) WHERE fact_kind =
'repository'` (136 kB for 3,002 repositories). With it the repository lookup
is an index probe in every plan. Ordinary-scope true cases fell to 0.018 to
0.530 ms, and the 300,000-file scope with the signal on the first file fell
from 162.656 ms (custom) and 42.370 ms (generic) to 0.058 and 0.050 ms. It
does not help the file-side worst case (273.307 ms custom and 272.911 ms
generic for the false scope; 276.756 and 240.106 ms for the last-position true
scope). A materialized CTE for the repository lookup was also tried and did
not fix the generic plan, so it is not proposed. The index would need a
numbered migration, `TestDDLMigrationParity` coverage, and the QA read-only
plan first. The decision is the owner's.

```bash
psql -d perf9986 -f explain.sql   # PREPARE probe(text,text,text[]); EXPLAIN (ANALYZE, BUFFERS) EXECUTE probe(...)
                                  # custom plans first, then SET plan_cache_mode = force_generic_plan
```

## Observability Evidence

Observability Evidence: `eshu_dp_reducer_workload_candidate_probe_total`, the
rate-limited probe error log, and the existing
`eshu_dp_postgres_query_duration_seconds` timing of the probe statement.

`eshu_dp_reducer_workload_candidate_probe_total{outcome,cause_class}` counts
every probe call: `outcome` is `may_hold`, `no_candidates`, or `error`;
`cause_class` is `none`, `canceled`, `deadline`, `sql_invalid`, `connection`,
`resource`, or `other`. A probe error also logs once per cause class per ten
minutes with `failure_class`, `sqlstate`, `scope_id`, `generation_id`, and a
`suppressed` count. An always-erroring probe (the #9986 shape) shows as a
steady `error`/`sql_invalid` rate plus one log line per ten minutes. Tests:
`TestPregateProbeRecordsOutcomeCounter`,
`TestPregateProbeErrorLogsOncePerCauseClass`,
`TestPregateProbeErrorFallsBackToBulkLoad`,
`TestClassifyWorkloadCandidateProbeError`, `TestWorkloadCandidateProbeContract`.
Four reducer mutants (counter adds 0, no rate limit, one global class, class
42 mapped wrong) were each killed.
