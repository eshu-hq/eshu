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
scope). Transport fix (first commit): 0.3 to 4 ms for ordinary scopes and 240
to 279 ms for a 300,000-file scope with no signal or the signal last, on
PostgreSQL 18.6, over 5,253,002 synthetic `fact_records` rows; the predicate
was unchanged there, only the key transport. Value-aware predicate (second
commit, section below): 0.3 to 1.9 ms for ordinary scopes and 460 to 640 ms
(no signal or signal last) or about 1.3 s (every file carries the QA null
shape) for a 300,000-file scope, bounded by the scope generation and linear
in its file count. Terminal result: every scenario returns the expected
boolean.

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

## Value-aware predicate (#9986, part A)

QA read-only evidence showed the key-existence predicate answers true for every
file-bearing scope: the parsers write every signal key on every file, as JSON
null or an empty list when the file holds no such evidence. The caller reads
the answer one way (`deferUnlessReadyForBulkLoad`): true runs the readiness
gates before the bulk load and can defer there; false, an error, or no probe
loads and gates normally. So key existence did not make the shed inert. It made
the shed fire for candidate-free scopes too, which then waited for the fence
instead of succeeding vacuously. The probe now tests the value the way
`ExtractWorkloadCandidates` reads it:

| key class | counts when | keys |
| --- | --- | --- |
| array bucket | a non-empty list (`len > 0`) | `k8s_resources`, `argocd_applications`, `argocd_applicationsets`, `dockerfile_stages`, `pipeline_calls`, `docker_compose_services`, five `cloudformation_*` |
| presence | present, not null, not a blank or `<nil>` string | `chart_type` |
| token | a string equal to `docker_compose` or `github_actions_workflow` after trimming | `artifact_type` |
| path | `relative_path` equals `Jenkinsfile` after trimming and case folding, with an object `parsed_file_data` | none |

The probe also requires `parsed_file_data` to be a JSON object, joins
`repo_id` to `graph_id` with the same trimming extraction applies, and keeps
the `?|` key-existence test as a cheap first filter. The language and path
gates extraction adds to some signals are not repeated, because dropping a
gate can only turn a false into a true (a cheap storm-window deferral), never
the reverse. Every key travels in a bound array derived from `factload`
(`WorkloadCandidateArrayBucketKeys`, `WorkloadCandidatePresenceKeys`,
`WorkloadCandidateTokenKeys`; a unit test pins that they partition
`WorkloadCandidateSignalKeys`).

### Differential proof

`TestScopeMayHoldWorkloadCandidatesDifferentialLive` seeds one scope
generation per generated case into a private schema, runs the shipped
statement through `FactStore.ScopeMayHoldWorkloadCandidates`, and runs
`reducer.ExtractWorkloadCandidates` on the same payload JSON decoded the way
the loader decodes it. It asserts probe false implies zero candidates, and that
null, empty, and blank-only shapes with no Jenkinsfile path probe false. The
corpus has 7,523 generations: each of the 13 keys times 20 value shapes (null,
empty and non-empty lists, blank, tab, NBSP, `<nil>`, numbers, booleans,
objects, padded and NBSP-padded token strings) times 4 languages times 7 paths
(including Unicode-fold Jenkinsfile spellings), the no-key and non-object
payloads, repository identity shapes, and the QA all-null shape.

| probe | probe true | false positives | false negatives |
| --- | --- | --- | --- |
| key existence (first commit) | 7,318 | 2,720 | 34 (all Jenkinsfile-path generations, plus two padded-identity generations) |
| value-aware (this commit) | 4,685 | 53 | 0 |

4,632 of the 7,523 generations yield a candidate. The 53 false positives are the
language and path gates described above (`dockerfile_stages` outside a
dockerfile, `pipeline_calls` outside groovy or a Jenkinsfile, a workflow
`artifact_type` outside a workflow path) and a repository name of `"  "` or
`"<nil>"`. The corpus is synthetic: its percentages say nothing about
production. The QA all-null shape is the line to read: key existence answers
true for it, the value-aware probe answers false.

Mutants, each killed by this test file and the live matrix: empty list counts
(4 failures), Jenkinsfile branch removed (3), trimmed `repo_id` comparison
removed (1), null `chart_type` counts (3), blank `chart_type` counts (1),
token padding not trimmed (1), any `artifact_type` string counts (3), object
guard on `parsed_file_data` removed (1).

### Plan

Same cluster and data as above, plus two scopes with the QA shape (150 files
and 300,000 files, every file carrying all 13 signal keys as null or empty).
Warm second run, custom plan, ms; the key-existence column is the first
commit's statement on the same data in the same session.

| scenario | answer | key existence | value-aware custom | value-aware generic |
| --- | --- | --- | --- | --- |
| signal on the last of 150 files | true | 0.365 | 0.467 | 0.274 |
| signal on the first of 150 files | true | not re-run | 0.710 | 0.473 |
| 150 files, no keys | false | 0.782 | 0.877 | 0.668 |
| 150 files, empty `k8s_resources` | false (key existence: true) | not re-run | 0.950 | 0.744 |
| 150 files, QA all-null shape | false (key existence: true) | 0.035 | 0.693 | 0.631 |
| 300,000 files, no keys | false | 311.535 | 524.034 | 458.859 |
| 300,000 files, signal on the last file | true | 460.167 | 527.660 | 464.185 |
| 300,000 files, QA all-null shape | false (key existence: true) | 81.830 | 1306.879 | 1300.219 |

No run uses a sequential scan. Two things cost more than key existence, and
both are the price of a correct false: a false answer must test every file of the
generation, and the QA null shape needs 13 key tests per file where key
existence stopped at the first file. Three cheaper-looking forms were measured
and rejected: per-key `payload #> path` extraction (about 20 s on the 300,000
file QA scope, because every call decompresses the whole TOASTed payload
again), a `jsonb_path_exists` single call (8.8 to 10.8 s, `like_regex` recompiles
per row), and `->>` per field (about 1.7 s). Reading `repo_id`,
`relative_path`, and `parsed_file_data` through one `jsonb_to_record` call
brought the QA 300,000-file case to 1.3 s and the no-key case to 0.46 to 0.52 s.
The cost is linear in the file count and paid only for a candidate-free
generation, whose normal load follows; a 150-file scope pays under 1 ms.
The decision is whether that probe cost is worth keeping candidate-free scopes
out of the storm-window deferral loop. It removes an extra deferral round for
each of them, so the recommendation is yes.

```bash
psql -d perf9986 -f explain10.sql   # shipped statement, custom then generic
```

## Repository identity cost (#9986, part B)

QA read-only plans showed the repository fact's stored payload size driving the
repository-side buffers: a 4,756,465 byte payload read 1,246 buffers for one
row, against 9 when the payload was not touched. The cause is that
`repo.payload->>'graph_id'` and `->>'name'` each decompress the TOASTed
payload (the multi-megabyte part is `imports_map`, see
`RepositoryFactEnvelope`). This was measured on a repository fact with a
15 MB stored payload (QA's was 4.76 MB, so scale by about 0.3) and the same
23,329 files as QA pair A: one scope with no signal in any file (QA all-null
shape), one with the signal on the last file. Warm second runs, custom plan.

| variant | no signal, ms | signal last, ms | repository node, ms | total buffers (no signal) | same result as shipped |
| --- | --- | --- | --- | --- | --- |
| shipped statement | 165.302 | 154.403 | 8.415 | 7,268 hit | (reference) |
| one `jsonb_to_record(repo.payload)` read | 229.441 | 255.423 | 0.051 plus 3.146 for the function scan | 5,291 hit | yes |
| identity from `stable_fact_key`, no name test | 177.526 | 232.200 | 0.026 | 3,314 hit | no |

Reading it:

- The repository side is 8.4 ms of 165 ms warm. The join filter that
  mentions `repo.payload->>'graph_id'` is not evaluated per file: the file side
  rejects the row first, so the TOASTed payload is read for the filter
  (twice) and once for the match, not once per file.
- Reading the identity in one `jsonb_to_record` call has the same result and
  fewer buffers, but the whole statement got slower in both scenarios (the
  planner changed the file-side shape), so it is not a pure win and is not
  adopted.
- `repository:` plus `graph_id` is the stable key of the only production
  repository emitter (`RepositoryFactEnvelope`), so the key could replace the
  `graph_id` read. The `name` exists only in the payload, so dropping its test
  to avoid the payload changes results (a nameless repository would probe
  true), and relying on the key format changes results for any fact written
  otherwise. Wall time did not improve either (the buffers fell by about
  3,950, the runs did not get faster). Not adopted.
- The cost that remains is the first read of a cold repository payload, which
  is a one-time page fetch per cache-cold repository fact (606 reads on QA for
  its 4.76 MB payload). It was not reproduced here: the synthetic cluster was
  warm, so the cold figure is the QA measurement, not this one.

Decision: leave the repository-side read as is. Record: 8.4 ms warm at 15 MB
stored payload, about 4,000 buffers, 5% of the statement on a 23,329-file scope.
A cache-cold repository payload read is NOT_CHECKED here.

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
