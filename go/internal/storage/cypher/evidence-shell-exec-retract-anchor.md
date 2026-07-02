# Shell Exec Retract Anchor Evidence

Scope: `shell_exec` shared projection retracts for
`Function-[:EXECUTES_SHELL]->ShellCommand`.

Performance Evidence:

- Baseline run: `4510-currentmain-pr230-source40-20260702T045529Z`.
- Baseline Eshu commit: `1038a56c327661fd93f469075799f5f32082d048`.
- Backend/version: NornicDB #230 branch image
  `eshu-nornicdb-pr230:c4901451`, commit
  `c4901451963aac00b069722178958e1c99755884`.
- Input shape: 895 git config roots; bounded stop after `source_local >= 40`
  with shared-projection evidence.
- Terminal queue sample: `projector/source_local` had 45 succeeded and 3
  claimed at stop; `reducer/shell_exec_materialization` had 41 succeeded and 5
  pending.
- Baseline `shell_exec` cycles: count 4, total duration `253.754896s`, max
  duration `121.605655s`.
- Baseline `shell_exec` retract time: total `253.733723s`, max `121.598721s`.
- The slow cycles were almost entirely retract time, not selection, write, or
  mark-complete.
- Candidate 1 change: repo-wide and file-scoped retracts bound indexed
  `Function.repo_id` / `Function.path` anchors before expanding
  `EXECUTES_SHELL` relationships.
- Candidate 1 remote run: `4512-shell-exec-anchor-pr230-20260702T051029Z`.
- Candidate 1 remote Eshu commit: `d8e9f97b2e064c866696a951fefc6ce9204cd4f4`.
- Remote stop: manually stopped at `2026-07-02T05:18:47Z` once evidence was
  sufficient; final queue had `projector/source_local` 77 succeeded, 7 claimed,
  3 pending, and 1 running.
- Candidate 1 `shell_exec` cycles: count 11, total duration `427.320337s`,
  max duration `149.150518s`.
- Candidate 1 `shell_exec` retract time: total `427.290141s`, max
  `149.146957s`.
- Candidate 1 result: the anchor-only rewrite is not sufficient and is not
  PR-ready. It
  removes the broad post-filter shape from the source text, but NornicDB #230
  still spends nearly all `shell_exec` shared-projection time inside retracts at
  corpus scale.
- Candidate 2 change: repo-wide and file-scoped retracts now bind
  `ShellCommand.repo_id` / `ShellCommand.path` anchors before expanding inbound
  `EXECUTES_SHELL` relationships, with graph schema indexes on both properties.
  This preserves the same edge deletion semantics because shell execution writes
  stamp every target `ShellCommand` with the source repo and path and target IDs
  are derived from repo, path, function, line, and API.
- Candidate 2 local status: focused regression and package gates pass. This is
  not PR-ready until the same branch is pulled onto the remote NornicDB #230
  stack and the bounded corpus proof shows a material retract/runtime win.

No-Regression Evidence:

- Candidate 1 red test first:
  `go test ./internal/storage/cypher -run 'Test(BuildRetractShellExecEdges|EdgeWriterRetractEdgesShellExec)' -count=1 -v`
  failed because the previous Cypher started from
  `MATCH (source:Function)-[rel:EXECUTES_SHELL]->()` and filtered by
  `source.repo_id IN $repo_ids` / `source.path IN $file_paths`.
- Candidate 1 after the query-shape change, the same focused command passed.
- Candidate 2 red tests first:
  `GOCACHE=$WORKTREE/.gocache go test ./internal/storage/cypher -run 'Test(BuildRetractShellExecEdges|EdgeWriterRetractEdgesShellExec)' -count=1 -v`
  failed because the production Cypher still matched
  `Function {repo_id: repo_id}` / `Function {path: file_path}` instead of
  `ShellCommand` target anchors. The schema regression
  `GOCACHE=$WORKTREE/.gocache.graph-red go test ./internal/graph -run 'TestSchemaStatementsIncludeShellExecRetractLookupIndexes' -count=1 -v`
  failed because `shell_command_repo_id` and `shell_command_path` indexes did
  not exist.
- Candidate 2 focused gates passed:
  `GOCACHE=$WORKTREE/.gocache go test ./internal/storage/cypher -run 'Test(BuildRetractShellExecEdges|EdgeWriterRetractEdgesShellExec)' -count=1 -v`
  and
  `GOCACHE=$WORKTREE/.gocache.graph-green go test ./internal/graph -run 'TestSchemaStatementsIncludeShellExecRetractLookupIndexes' -count=1 -v`.
- Candidate 2 package gates passed:
  `GOCACHE=$WORKTREE/.gocache.graph-package-2 go test ./internal/graph -count=1`
  and
  `GOCACHE=$WORKTREE/.gocache.storage-package go test ./internal/storage/cypher -count=1`.
- Candidate 1 package gate passed:
  `go test ./internal/storage/cypher -count=1`.
- `git diff --check` passed.
- Commit hooks also passed changed-package linting, gofumpt, package docs, file
  cap, and staged-content attribution checks.

No-Observability-Change:

- This change does not add or remove metrics, spans, log keys, queues, worker
  counts, environment variables, status fields, or API/MCP response fields.
- Existing reducer shared-projection telemetry continues to report
  `duration_seconds`, `retract_duration_seconds`, selection, write,
  mark-complete, lease-claim, and intent-wait timings for the same domain.
