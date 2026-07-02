# Evidence: repository dependency retract attribution

Scope: `repo_dependency` retract execution in the shared Cypher edge writer.
The original change added structured role attribution around the grouped
retract path. The follow-up diagnostic mode split bounded proof runs into
repository relationship edge, `RUNS_ON`, and evidence-artifact cleanup timings.
This slice makes that three-role split the production grouped shape too: the
transaction boundary, retry wrapper, batch ownership, and worker count stay the
same, but single-repository cycles use direct `$repo_id` anchors instead of
`UNWIND $repo_ids` for the repository relationship delete.

## No-regression evidence

No-Regression Evidence: baseline current-main full-corpus validation on the
NornicDB-backed remote Compose profile showed the `repo_dependency` retract
tail still dominated by no-op grouped retract cycles after the bound-repository
rewrite: two observed cycles spent about 132s and 145s in
`retract_duration_seconds` while writing 0 rows. Backend/version for that run:
Eshu `626bf209e4d2295b40667bcc377ebab15c8cf651` with a NornicDB image built
from the PR #230-equivalent source at `61e05b41`; the production module pin in
this checkout remains `github.com/orneryd/nornicdb v1.0.45`.

After this change, the same `repo_dependency` branch builds three statements
and, when the executor supports `ExecuteGroup`, still sends them in one grouped
call. Statements passed to the executor are sanitized with `SanitizeStatement`,
so `_eshu_*` diagnostic metadata is used only for logs and does not reach
NornicDB as an unreferenced parameter. The grouped-path test asserts one group
call, checks the executed statements are sanitized, and checks the log carries
all three grouped statement roles. The single-repo test asserts the repository
relationship delete avoids `UNWIND` and passes a direct `repo_id` parameter so
the NornicDB #230 bound relationship delete route is eligible. The diagnostic
switch test asserts the group executor is bypassed only when the flag is
enabled and that the three statement-level logs carry
`repository_relationship_edges`, `runs_on_relationships`, and
`evidence_artifacts` with `repo_count` and `duration_seconds`.

Verification:

```bash
cd go && GOCACHE=/tmp/eshu-gocache-4508-post-rebase go test ./internal/storage/cypher -count=1
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
scripts/test-verify-query-plan-regression.sh
scripts/verify-query-plan-regression.sh
git diff --check
```

## Performance evidence

Performance Evidence: bounded remote proof
`4508-repo-dep-bound-delete-20260702T033032Z` ran Eshu
`656110c934b1050834fbd14df952bd4bfbc69dd6` against NornicDB PR #230 image
`eshu-nornicdb-pr230:c4901451` / commit
`c4901451963aac00b069722178958e1c99755884`. The run used the same tuning shape
as the #4507 diagnostic baseline: `GOMAXPROCS=16`, `GOMEMLIMIT=48GiB`,
`ESHU_SNAPSHOT_WORKERS=16`, `ESHU_PARSE_WORKERS=16`,
`ESHU_LARGE_REPO_MAX_CONCURRENT=4`, `ESHU_PROJECTION_WORKERS=8`,
`ESHU_REDUCER_WORKERS=16`, `ESHU_SHARED_PROJECTION_WORKERS=4`,
`ESHU_SHARED_PROJECTION_PARTITION_COUNT=8`,
`ESHU_CODE_CALL_PROJECTION_WORKERS=4`, `ESHU_POSTGRES_MAX_OPEN_CONNS=96`,
`ESHU_GRAPH_WRITE_MAX_IN_FLIGHT=0`, and
`ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING=true`.

The #4507 baseline run
`4507-repo-dep-diagnostic-20260702T022632Z` recorded
`repository_relationship_edges` at count `23`, avg `5.352s`, max `39.889s`;
`runs_on_relationships` at count `22`, avg `1.872s`, max `10.215s`; and
`evidence_artifacts` at count `22`, avg `2.008s`, max `11.200s`.

The #4508 bounded run stopped at the short cap after 20
`repository_relationship_edges` samples, before full-corpus completion. It
recorded `repository_relationship_edges` at count `20`, avg `2.950s`, max
`8.155s`; `runs_on_relationships` at count `20`, avg `1.366s`, max `5.221s`;
and `evidence_artifacts` at count `20`, avg `1.598s`, max `6.690s`. Queue
state at stop showed `source_local` at 40 succeeded / 2 claimed and
`code_import_repo_edge` at 40 succeeded. The table-count query used the wrong
table name after stop, so table counts are not claimed for this bounded proof.
This is a handler/query-shape win for the measured repo-dependency retract
role, not a full-corpus wall-clock completion claim.

## Observability evidence

Observability Evidence: `repo_dependency` retracts now emit
`shared edge retract group completed` on grouped execution with `domain`,
`evidence_source`, `execution_mode=group`, `repo_count`, `statement_count`,
`duration_seconds`, and `statement_summaries`. Each summary names the
relationship family:
`repository_relationship_edges`,
`runs_on_relationships`, or `evidence_artifacts`. Sequential fallback execution
emits `shared edge retract statement completed` with `statement_role`,
`repo_count`, `statement_count=1`, `duration_seconds`, and the same statement
summary. With `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING=true`, diagnostic
execution logs the same three statement roles separately.

The grouped log intentionally records one duration for the atomic group instead
of splitting production execution into separate transactions. That preserves
the `GroupExecutor` contract while making the next runtime snapshot
self-describing enough to separate Eshu statement selection from backend/group
execution time.
