# Evidence: repository dependency retract attribution

Scope: `repo_dependency` retract execution in the shared Cypher edge writer.
The original change added structured role attribution around the two grouped
retract statements without changing their Cypher text, order, retry wrapper,
batch shape, worker count, or grouped transaction boundary. The follow-up
diagnostic mode keeps that production grouped path unchanged and splits only
bounded diagnostic runs into repository relationship edge, `RUNS_ON`, and
evidence-artifact cleanup timings.

## No-regression evidence

No-Regression Evidence: baseline current-main full-corpus validation on the
NornicDB-backed remote Compose profile showed the `repo_dependency` retract
tail still dominated by no-op grouped retract cycles after the bound-repository
rewrite: two observed cycles spent about 132s and 145s in
`retract_duration_seconds` while writing 0 rows. Backend/version for that run:
Eshu `626bf209e4d2295b40667bcc377ebab15c8cf651` with a NornicDB image built
from the PR #230-equivalent source at `61e05b41`; the production module pin in
this checkout remains `github.com/orneryd/nornicdb v1.0.45`.

After this change, the same `repo_dependency` branch still builds the same two
statements and, when the executor supports `ExecuteGroup`, still sends them in
one grouped call. Statements passed to the executor are sanitized with
`SanitizeStatement`, so `_eshu_*` diagnostic metadata is used only for logs and
does not reach NornicDB as an unreferenced parameter. The grouped-path test
asserts one group call, checks the executed statements are sanitized, and checks
the log carries both grouped statement roles. The diagnostic switch test asserts
the group executor is bypassed only when the flag is enabled and that the three
statement-level logs carry `repository_relationship_edges`,
`runs_on_relationships`, and `evidence_artifacts` with `repo_count` and
`duration_seconds`. The input shape is two synthetic repository intents for the
sequential fallback and one synthetic repository intent for the grouped and
diagnostic paths; terminal row counts are unchanged because this slice only
adds diagnostic statement separation.

Verification:

```bash
cd go && GOCACHE=/tmp/eshu-gocache-perf-repo-dep-retract-timing go test ./internal/storage/cypher -count=1
GOCACHE=/tmp/eshu-gocache-perf-repo-dep-retract-timing make pre-pr
git diff --check
```

`make pre-pr` passed the changed-package tests, selected exactness and
telemetry gates, code coverage report, and the graph-write race lane. The
change does not claim a lower full-corpus runtime; the next full-corpus run must
use the new diagnostic roles to decide whether the atomic grouped retract time
is coming from repository relationship edge cleanup, `RUNS_ON` cleanup,
deployment-evidence artifact cleanup, or backend execution beneath the group
boundary.

## Observability evidence

Observability Evidence: `repo_dependency` retracts now emit
`shared edge retract group completed` on grouped execution with `domain`,
`evidence_source`, `execution_mode=group`, `repo_count`, `statement_count`,
`duration_seconds`, and `statement_summaries`. Each summary names the
relationship family: repository relationships
`DEPENDS_ON|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|USES_MODULE|READS_CONFIG_FROM|RUNS_ON`
or `HAS_DEPLOYMENT_EVIDENCE` evidence artifacts. Sequential fallback execution
emits `shared edge retract statement completed` with `statement_role`,
`repo_count`, `statement_count=1`, `duration_seconds`, and the same statement
summary. With `ESHU_REPO_DEPENDENCY_RETRACT_STATEMENT_TIMING=true`, diagnostic
execution logs three statement roles:
`repository_relationship_edges`, `runs_on_relationships`, and
`evidence_artifacts`.

The grouped log intentionally records one duration for the atomic group instead
of splitting production execution into separate transactions. That preserves
the `GroupExecutor` contract while making the next runtime snapshot
self-describing enough to separate Eshu statement selection from backend/group
execution time.
