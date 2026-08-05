# #5830 workflow-image golden classifier evidence

## Accuracy boundary

Filesystem managed-copy mode copies admitted working-tree bytes into the
collector workspace. A source checkout's `HEAD` is therefore valid commit
evidence only when that working tree is clean and the copied bytes match the
observed commit. The filesystem selector obtains the branch object ID and
dirtiness records immediately before `copyRepositoryTree` with
`git status --porcelain=v2`, then loads the immutable commit's blob identities
with `git ls-tree`. The managed-copy walk hashes each regular file as a Git
blob while streaming those same bytes to the destination, so validation does
not reread the copied tree. The walk accounts for every immutable tracked
path: a copied path must match its committed blob, while a path omitted by an
observed collector-policy decision is discharged without opening its excluded
payload. Each directory's `.eshuignore` is read once. A tracked control must
match its immutable blob; an ignored, untracked local control remains
authoritative operator policy. Changed, extra, or unexpectedly missing copied
paths fail closed; submodule content also fails closed unless it is an
outer-tree blob.

On Unix, the copy pins the source root and current directory ancestry, opens
directories and files relative to those descriptors without following
symlinks, and compares pre-open, opened, and post-open file identity before
reading bytes. The Windows path is root-confined and uses the same identity
checks. The snapshot never re-reads the source checkout for a managed copy.
Dirty tracked content, admitted untracked content, conflicts, dirty
submodules, an unborn branch, Git command errors, clean-to-dirty-to-clean
changes during the copy, symlink swaps, and working-tree clean-filter,
encoding, or line-ending transforms all produce no source commit SHA. The
validator does not ask Git to apply clean filters, because a repository-local
filter driver can execute an external command. Tests exercise clean and
divergent fact emission, omitted tracked workflows, interleaving and
untracked-file races, policy-control mutation, excluded-payload privacy,
bounded descriptor use, symlink swaps, CRLF fail-closed behavior, and
hostile-filter non-execution.

GitHub Actions workflow-image evidence is also provider-owned. A non-GitHub run
for the same repository and commit cannot attach that evidence, and the graph
projection independently rejects a non-GitHub decision before emitting a
`BUILT_FROM` row. This keeps the decision provider, evidence kind, source tool,
and graph assertion in agreement.

## Runtime evidence

Performance Evidence: The managed-copy accuracy guard replaces one local
`git rev-parse HEAD` subprocess with one clean/OID status read, one immutable
`git ls-tree` read, and inline blob hashing during the existing copy, only for
a changed filesystem managed-copy repository. It adds no second full-tree
filesystem read. Unchanged manifest polls select no repository, and Git sync,
clone, ref-worktree, and filesystem-direct paths retain their existing
behavior. Provider isolation adds one bounded string comparison per run during
attachment and one per candidate graph row; it does not add a query, graph
write, queue item, retry, or lock for accepted GitHub rows.

The final-parent representative large-repository rung used the clean parent
checkout at `4f2a5f8b6d73bb23c52a7b34edd40e98e197602d`: 17,898 admitted
regular files and 117,513,254 copied bytes on the same Apple M5 Max. Five
adjacent pairs alternated the complete old copy-plus-`rev-parse` path with the
integrated copy-bound path to prevent a changing host load from biasing one
whole sample group. Old-path samples were 7.523, 6.614, 7.212, 8.204, and
6.720 seconds (median 7.212 seconds). Copy-bound samples were 6.863, 7.222,
7.593, 7.478, and 7.411 seconds (median 7.411 seconds). Independently sorting
the groups yields a +0.200-second (+2.77%) median difference; the adjacent-pair
deltas were -0.661, +0.607, +0.382, -0.726, and +0.691 seconds, whose median
is +0.382 seconds (median pair percentage +5.29%). The ranges overlap and the
pair distribution straddles zero, with two of five copy-bound pairs faster, so
the run establishes no consistent regression and makes no speedup claim.
Exact invocation, run in `prior, copy-bound` order five times:
`GOMAXPROCS=1 ESHU_BENCHMARK_REPOSITORY=<clean-parent-checkout> go test ./internal/collector -run '^$' -bench '^BenchmarkFilesystemManagedCopyCommitAttributionLargeRepository$' -benchtime=1x -count=1`.

The skip-heavy worst case used 1,000 sibling directories with 100 immutable
paths each, all omitted by observed collector policy. The prior repeated map
scan measured 474.130, 509.606, and 482.807 milliseconds (median 482.807
milliseconds). Sorted-prefix discharge measured 19.048, 17.767, and 19.021
milliseconds (median 19.021 milliseconds): 96.06% lower, or 25.38x faster.
Exact command:
`GOMAXPROCS=1 go test ./internal/collector -run '^$' -bench '^BenchmarkManagedCopyDischargeSkippedDirectories$' -benchtime=1x -count=3`.

The old path could publish false commit provenance, while the integrated path
verifies every admitted regular-file byte against the immutable tree during
the required copy. It is bounded to changed, clean filesystem managed-copy
repositories; unchanged manifest polls perform neither the copy nor the
verification, and other source modes are unaffected. The rebased code-head B-7
run below kept collection at 16 seconds and total wall time within the required
ceiling.

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

No-Regression Evidence: An earlier same-machine B-7 comparison used the same
30-repository corpus, Postgres path, exact NornicDB source commit
`5d2731ae1b3328708f74f12c21658786abac641a`, start event, and terminal green
gate event. Before the managed-copy and provider guards, B-7 completed in 125
seconds; its reported phases summed to 107 seconds (bootstrap 3, collect 15,
first drain 66, maintenance drains 19, graph/query 4). After the guards, B-7
completed in 129 seconds; its phases summed to 109 seconds (bootstrap 4,
collect 16, first drain 66, maintenance drains 20, graph/query 3). The total
increased by 4 seconds and the instrumented pipeline phases by 2 seconds
(1.9%). Build and backend startup account for uninstrumented variance, so no
speedup is claimed. Every required timing remained inside its gate ceiling;
maintenance drains exceeded the advisory 19-second ceiling by 1 second.

The rebased code head `36c3dbd3cf21d40e853338dbd9a44c9aabc0a6a0`
completed B-7 in 139 seconds. Its phases summed to 121 seconds (bootstrap 4,
collect 16, first drain 64, maintenance drains 35, graph/query 2). All required
timings remained inside their gate ceilings; maintenance drains produced the
single timing advisory. The repeated isolated final-parent benchmark above is
the collector-path no-regression evidence; the variable maintenance phase is
reported here without attributing it to the collector or claiming a speedup.

That exact run reported 529 passes, zero required failures, and one timing
advisory. `fact_work_items`, required shared projection intents, and cross-scope
completion events each had zero nonterminal rows; dead letters were zero.
rc-165 retained 24 exact-digest OCI `BUILT_FROM` assertions. rc-173 retained
exactly one `CI_CD_RUN_WORKFLOW_IMAGE_CORRELATION` assertion with
`source_tool=github_actions`. The focused collector/reducer/query/Cypher/gate
suite and the shell mirror also passed after the final source edit.
