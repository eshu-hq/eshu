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
- Candidate change: repo-wide and file-scoped retracts now bind indexed
  `Function.repo_id` / `Function.path` anchors before expanding
  `EXECUTES_SHELL` relationships.
- Remote after measurement is required before PR creation; this branch must be
  pulled and tested on the remote corpus stack first.

No-Regression Evidence:

- Red test first:
  `go test ./internal/storage/cypher -run 'Test(BuildRetractShellExecEdges|EdgeWriterRetractEdgesShellExec)' -count=1 -v`
  failed because the previous Cypher started from
  `MATCH (source:Function)-[rel:EXECUTES_SHELL]->()` and filtered by
  `source.repo_id IN $repo_ids` / `source.path IN $file_paths`.
- After the query-shape change, the same focused command passed.
- Package gate passed:
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
