# #5830 workflow-image golden classifier evidence

## Accuracy boundary

Filesystem managed-copy mode copies admitted working-tree bytes into the
collector workspace. A source checkout's `HEAD` is therefore valid commit
evidence only when that working tree is clean. The snapshot now obtains the
branch object ID and dirtiness records in one `git status --porcelain=v2` read.
Dirty tracked content, admitted untracked content, conflicts, dirty submodules,
an unborn branch, and Git command errors all fail closed with no source commit
SHA. Tests exercise the actual copy and fact-emission path for clean,
dirty-tracked, and admitted-untracked workflow files.

GitHub Actions workflow-image evidence is also provider-owned. A non-GitHub run
for the same repository and commit cannot attach that evidence, and the graph
projection independently rejects a non-GitHub decision before emitting a
`BUILT_FROM` row. This keeps the decision provider, evidence kind, source tool,
and graph assertion in agreement.

## Runtime evidence

Performance Evidence: The managed-copy accuracy guard replaces one local
`git rev-parse HEAD` subprocess with one local porcelain-v2 status subprocess
only for a changed filesystem managed-copy repository. Unchanged manifest polls
select no repository, and Git sync, clone, ref-worktree, and filesystem-direct
paths retain their existing behavior. On a clean Eshu checkout using Git 2.50.1
on darwin/arm64, 50 post-warmup executions on the same tree measured:

- prior `git rev-parse HEAD`: p50 15.955 ms, p95 25.346 ms;
- guarded `git status --porcelain=v2 --branch --untracked-files=all
  --ignore-submodules=none -- .`: p50 133.856 ms, p95 169.540 ms.

The measured p50 cost is 117.901 ms per changed managed-copy repository. The
cost is accepted because labeling live, divergent bytes as committed evidence
is an accuracy failure; the single-command design avoids a second subprocess
and a status-versus-HEAD race. Provider isolation adds one bounded string
comparison per run during attachment and one per candidate graph row; it does
not add a query, graph write, queue item, retry, or lock for accepted GitHub
rows.

No-Observability-Change: No metric instrument, attribute key, span, structured
log field, status field, queue domain, worker, lease, batch size, or runtime
knob changes. Operators retain the existing snapshot-stage spans and logs,
durable `scope_generations.source_commit_sha`, emitted workflow fact payloads,
CI/CD correlation outcomes, reducer execution telemetry, and
`eshu_dp_provenance_edges_total`. A divergent managed copy is deliberately
visible as an absent source/workflow commit SHA and cannot be promoted to a
commit-matched exact correlation.

## Verification

No-Regression Evidence: A same-machine B-7 comparison used the same 30-repository
corpus, Postgres path, exact NornicDB source commit
`5d2731ae1b3328708f74f12c21658786abac641a`, start event, and terminal green
gate event. Before the managed-copy and provider guards, B-7 completed in 125
seconds; its reported phases summed to 107 seconds (bootstrap 3, collect 15,
first drain 66, maintenance drains 19, graph/query 4). After the guards, B-7
completed in 154 seconds; its phases summed to 113 seconds (bootstrap 8,
collect 18, first drain 66, maintenance drains 18, graph/query 3). The total
increased by 29 seconds and the instrumented pipeline phases by 6 seconds
(5.6%). Build and backend startup account for uninstrumented variance, so no
speedup is claimed. Every reported phase remained inside its gate ceiling.

The post-fix gate reported 530 passes, zero required failures, and zero
advisory warnings. `fact_work_items`, required shared projection intents, and
cross-scope completion events each had zero nonterminal rows; dead letters were
zero. rc-165 retained 24 exact-digest OCI `BUILT_FROM` assertions. rc-173
retained exactly one `CI_CD_RUN_WORKFLOW_IMAGE_CORRELATION` assertion with
`source_tool=github_actions`. The focused collector/reducer/query/Cypher/gate
suite and the shell mirror also passed after the final source edit.
