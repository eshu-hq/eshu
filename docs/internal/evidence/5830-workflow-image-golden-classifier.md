# #5830 workflow-image golden classifier evidence

## Accuracy boundary

Filesystem managed-copy mode copies admitted working-tree bytes into the
collector workspace. A source checkout's `HEAD` is therefore valid commit
evidence only when that working tree is clean and the copied bytes match the
observed commit. The filesystem selector obtains the branch object ID and
dirtiness records immediately before `copyRepositoryTree` with
`git status --porcelain=v2`. After the copy, it builds a temporary index from
that immutable commit, removes commit paths intentionally filtered from the
managed copy, and verifies every copied regular file through Git's own
worktree comparison. Changed or extra copied paths fail closed; submodule
content also fails closed unless it can be proven as an outer-tree blob. The
snapshot never re-reads the source checkout for a managed copy. Dirty tracked
content, admitted untracked content, conflicts, dirty submodules, an unborn
branch, Git command errors, and a clean-to-dirty-to-clean change during copy
all produce no source commit SHA. Tests exercise the actual copy and
fact-emission path for clean, dirty-tracked, admitted-untracked, and
dirty-during-copy-then-clean-reset workflow files.

GitHub Actions workflow-image evidence is also provider-owned. A non-GitHub run
for the same repository and commit cannot attach that evidence, and the graph
projection independently rejects a non-GitHub decision before emitting a
`BUILT_FROM` row. This keeps the decision provider, evidence kind, source tool,
and graph assertion in agreement.

## Runtime evidence

Performance Evidence: The managed-copy accuracy guard replaces one local
`git rev-parse HEAD` subprocess with one clean/OID status read and one immutable
tree validation
only for a changed filesystem managed-copy repository. Unchanged manifest polls
select no repository, and Git sync, clone, ref-worktree, and filesystem-direct
paths retain their existing behavior. On darwin/arm64 with Git 2.50.1, the
committed two-file managed-copy benchmark ran 20 iterations per sample and five
samples on the same synthetic repository. The median sample measured:

- prior `git rev-parse HEAD`: 45.392 ms/op;
- complete clean/OID plus immutable-tree validation: 243.937 ms/op.

Exact command:
`go test ./internal/collector -run '^$' -bench 'BenchmarkFilesystemManagedCopyCommitAttribution/(prior-rev-parse-head|copy-bound-attribution)$' -benchtime=20x -count=5`.

The measured cost over the prior command is 198.545 ms per changed managed-copy
repository. The
cost is accepted because labeling live, divergent bytes as committed evidence
is an accuracy failure; the temporary index binds the identity to the copied
content instead of observing a mutable source later. Provider isolation adds
one bounded string comparison per run during attachment and one per candidate
graph row; it does not add a query, graph write, queue item, retry, or lock for
accepted GitHub rows.

The representative large-repository rung used the clean Eshu checkout at
`bf6e6b480119e1eacdbdfb3626167da1a02df459`: 17,886 admitted regular files and
117,362,746 copied bytes on the same Apple M5 Max. Five one-iteration samples
measured the complete old copy-plus-`rev-parse` path at 7.919, 7.984, 7.480,
7.131, and 7.188 seconds (median 7.480 seconds). The complete copy-bound path
measured 13.916, 19.806, 8.612, 8.679, and 18.637 seconds (median 13.916
seconds). The median cost is 6.436 seconds, or 86.0%, for a changed clean
managed-copy repository of this size. Exact command:
`ESHU_BENCHMARK_REPOSITORY=<clean-eshu-checkout> go test ./internal/collector -run '^$' -bench 'BenchmarkFilesystemManagedCopyCommitAttributionLargeRepository/(prior-full-managed-copy|copy-bound-full-managed-copy)$' -benchtime=1x -count=5`.

The scaling cost is accepted under the repository's accuracy-first contract:
the old path could publish false commit provenance, while the new path verifies
every admitted byte against the immutable tree. It is bounded to changed,
clean filesystem managed-copy repositories; unchanged manifest polls perform
neither the copy nor the verification, and other source modes are unaffected.
The same-machine B-7 rung below kept collection at 15 seconds and total wall
time within the required ceiling.

No-Observability-Change: No metric instrument, attribute key, span, structured
log field, status field, queue domain, worker, lease, batch size, or runtime
knob changes. The selector work remains visible through the existing
`scope.assign` span, `eshu_dp_scope_assign_duration_seconds`, and
`collector discovery completed` structured log. Operators also retain the
downstream snapshot-stage spans and logs, durable
`scope_generations.source_commit_sha`, emitted workflow fact payloads, CI/CD
correlation outcomes, reducer execution telemetry, and
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
completed in 129 seconds; its phases summed to 110 seconds (bootstrap 5,
collect 15, first drain 66, maintenance drains 21, graph/query 3). The total
increased by 4 seconds and the instrumented pipeline phases by 3 seconds
(2.8%). Build and backend startup account for uninstrumented variance, so no
speedup is claimed. Every required timing remained inside its gate ceiling;
maintenance drains exceeded the advisory 19-second ceiling by 2 seconds.

The post-fix gate reported 529 passes, zero required failures, and one timing
advisory. `fact_work_items`, required shared projection intents, and cross-scope
completion events each had zero nonterminal rows; dead letters were zero.
rc-165 retained 24 exact-digest OCI `BUILT_FROM` assertions. rc-173 retained
exactly one `CI_CD_RUN_WORKFLOW_IMAGE_CORRELATION` assertion with
`source_tool=github_actions`. The focused collector/reducer/query/Cypher/gate
suite and the shell mirror also passed after the final source edit.
