# #6782: row-total divergences are advisory at the gate (option 2)

Owner direction 2026-09-22: implement options 1+2 of the transient-read
characterization (issue #6782, 11:29 table note). This slice is option 2;
option 1 (capture-side transient-read exclusion for digest-disagreeing
reads such as the Module orphan page) follows separately.

## Symptom

PR #6962 run 35756330128 attempt 1 (job 106842906473 `differential
nornicdb vs neo4j`, head 2a808d10b) failed quorum
with 42 reproduced `rowcount` divergences, first:

`MATCH (fn:Function)-[:INVOKES_CLOUD_ACTION]->(action:CloudAction)
WHERE fn.uid IN $function_uids MATCH (fn)-[:RUNS_IN]->(workload:Workload)
RETURN fn.uid, action.action, workload.id`
`row count differs (nornicdb=8, neo4j=7) with equal digests`.

From the run's `differential-capture` artifact: the extra NornicDB row
carries an already-seen digest. Per exploded element the recordings are
0/1-row poll executions — the cloud-sink value-flow loader polls this read
in the reducer until the pair resolves, and one leg observes the converged
row in one more poll iteration. Final answers agree on both backends; only
the observation counts differ. The re-run (job 106859287633) went green,
consistent with poll timing rather than truth — the mechanism case rests on
the digest-set agreement plus the 0/1-row poll shape above.

## Change

`AdvisoryKind` holds `rowcount` advisory alongside `executions`
(`go/internal/backendconformance/differential_kinds.go`). The `rowcount`
allowlist tier is removed (no committed entry used it) so retired entries
cannot linger; the advisory finding, the #6941 ceiling, the flag help, and
the docs wording now cover row totals. Finding and flag names are unchanged
for CI-contract stability.

A systematic duplicate-row regression still inflates the advisory total and,
where the ceiling is configured (CI: 200), trips it once the total exceeds
it; small totals stay advisory by design.

## End-to-end proof on the failing capture

Binaries built from main 44e5607db and this branch, `-phase=backend-diff`
in quorum mode over the exact failing capture (run 35756330320 pairings,
committed allowlist each side):

- main: exit 1, `1 pass, 1 required-fail` (the rowcount quorum failure).
- branch: exit 0, `2 pass, 0 required-fail, 1 advisory-warn`, advisory
  detail `57 reproduced scheduling-noise divergence(s) with agreeing
  results held advisory (execution counts or row totals)`, top offender
  the INVOKES_CLOUD_ACTION statement (47).

## RED/GREEN proof commands run

- `go test ./internal/backendconformance/ -run
  TestSplitAdvisoryTreatsRowCountAsAdvisory`: RED before the AdvisoryKind
  change (rowcount partitioned required), GREEN after.
- `go test ./cmd/golden-corpus-gate/ -run
  TestRunBackendDiffQuorumReproducedRowcountIsAdvisory`: reproduced
  rowcount shape (same exec counts, same digest sets, 2v1 totals) passes
  quorum as advisory.
- Focused suites green: `backendconformance`, `graph/capture`,
  `golden-corpus-gate`; real allowlist YAML parses; gofumpt clean.

No-Regression Evidence: the files the perf-evidence gate names as hot
(`backendconformance/differential_kinds.go`, `capture/allowlist.go`,
`capture/diff.go`) run only inside the gate's offline backend-diff phase,
never on a service path. Baseline main binary (44e5607db) vs branch binary
on the run 35756330320 capture (1,541/1,484 statement groups, 57 advisory),
four interleaved runs each on the same host: branch 412/413/418/428 ms,
main 420/421/425/401 ms — indistinguishable (±2%, alternating first-mover).
Both runs load and compare every recording; the branch adds one kind
comparison per divergence, which cannot dominate the phase. Verdicts differ
only as designed (main required-fails, branch holds advisory).

No-Observability-Change: no metric, span, log key, or status field changes;
finding names (`nornicdb_vs_neo4j_executions`,
`nornicdb_vs_neo4j_executions_ceiling`) and the flag name are unchanged.
Only the advisory/ceiling detail prose widened, on the gate's stdout.
