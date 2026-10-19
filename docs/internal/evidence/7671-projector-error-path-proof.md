# #7671 projector error-path change: regression and observability evidence

Change: fatal codegraph decodes fail loud, empty file paths quarantine, and
empty materializations warn only on file-bearing generations (branch
`fix/7671-file-node-silent-success`). All production hunks sit in
error/empty/quarantine branches; happy-path projection code is untouched
(verified in repeat full review).

## No-Regression Evidence:

- Baseline (pristine `origin/main` throwaway worktree, same host):
  `projector/canonical` 0.010 s, `projector/decode` 0.004 s,
  `projector/runtime` 0.032 s (`go test -count=1`, all ok).
- After (branch head, same host, same command): `canonical` 0.040 s,
  `decode` 0.010 s, `runtime` 0.041 s (all ok, includes 8 new tests).
- Backend: local `go test`, no database or graph backend involved; input
  shape is the focused unit suites above (unit scale, not a corpus replay).
- Row counts: not applicable (no rows read or written by this change).
- Why safe: no happy-path statement was modified, added, or reordered; the
  only new allocation (`attrs` slice) executes inside the already-taken
  `IsEmpty` branch. Suite times match within unit-test noise.

## Observability Evidence:

- New signals (log-only, no new metric or Cypher): `IsEmpty` early-return
  warns with fact/file-fact counts when file facts exist and debugs
  otherwise; files-phase empty warns when repository-identified; phase
  row-drop warns (identity-less) or debugs (undecodable); ref-gate log
  carries fact counts.
- Proof: `TestWriteCanonicalProjectionEmptyWarnsOnlyOnFileFacts`
  (red-first: non-file facts warned pre-fix) plus the fatal-decode and
  quarantine tests, all green on the branch head.
