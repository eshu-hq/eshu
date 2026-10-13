# #8077 relative-path fix: no-regression note

`RepositoryRelativePath` (`go/internal/collector/repo/git/model/factstream.go`)
runs once per collected file fact. The #8077 fix only restructures the
symlink-resolution helper `canonicalPathForRel`; it changes no query, no
concurrency, no batching, and no telemetry.

## No-Regression Evidence:

- Baseline: one `filepath.Abs` + one `filepath.EvalSymlinks` per call. On a
  missing file the EvalSymlinks errors and the old code returned the
  unresolved spelling (the bug: `../` segments).
- After: identical single Abs + single EvalSymlinks on every existing path
  (the hot case: every file that still exists). The new parent-walk loop
  runs only when the full-path EvalSymlinks already failed (the rare
  deleted-file path), bounded by path depth, one EvalSymlinks per ancestor
  until the first success, then one `filepath.Rel` + `filepath.Join`
  (pure string ops, no syscalls).
- Why safe: zero additional syscalls on the hot path; the fallback path was
  previously a wrong answer, so there is no slower-but-correct baseline to
  regress against. No goroutine, lock, retry, or batching change.
- Measured: `go test ./internal/collector/repo/git/model/ -count=1` 0.003s;
  `go test ./internal/collector/repo/git/ -count=1` 32.6s, both PASS on
  Linux (this host). The new symlink-fixture unit test fails on old code
  (`../link/sub/missing.go`) and passes on new code.

## No-Observability-Change:

No metric, span, log line, or status output touched. The function returns a
string; callers and their telemetry are unchanged.
