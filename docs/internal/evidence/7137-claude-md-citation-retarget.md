# Issue 7137 CLAUDE.md citation retarget: no runtime change

#7137 retargets comments and docs that cited the deleted root `CLAUDE.md`.
Three of the touched Go files sit on paths the performance-evidence gate
treats as hot (`go/cmd/bootstrap-index/main.go`,
`go/internal/telemetry/contract.go`, `go/internal/telemetry/instruments.go`),
so this note records why the change cannot alter runtime behavior.

No-Regression Evidence: every changed Go file under `go/` is token-identical
to its base version once plain `//` comments are stripped, measured with the
repository's own `go/cmd/token-diff` (the comment-only exemption used by
parser-relationship-kit).

- Baseline: `84b70f5640^1`, the first parent of #7186's squash merge.
- After: `84b70f5640`, the #7186 squash-merge commit on main.
- Backend/version: go1.27.1 darwin/arm64; no database or graph backend is
  involved because no query, write, queue, or worker code changed.
- Input shape: the 30 `.go` files in
  `git diff --name-only 84b70f5640^1 84b70f5640 -- '*.go'`; extract each side
  with `git show <rev>:<path>` and pass both files to `token-diff`.
- Result: 29 files exit 0 (identical token streams, directives and block
  comments still compared). The one non-zero file is
  `tools/golangci-lint-filelength/filelength.go`, whose analyzer `Doc` string
  now reads `AGENTS.md` instead of `AGENTS.md / CLAUDE.md`; it is a lint
  plugin and is not linked into any Eshu binary.
- Terminal counts: no queue or row counts apply; `go test -count=1` on the 13
  changed packages passed before and after.
- Line counts: every changed `.go` file keeps its base line count, so no
  source position shifts in panics, traces, or logs.

Rejected probe: comparing binary hashes built at base and head. With cgo on
(tree-sitter grammars require it), building the same tree three times on this
host produced two alternating hashes for `cmd/ingester`, so a hash mismatch
cannot distinguish a real change from link nondeterminism. With cgo off the
binaries do not build. The token comparison above is the reliable probe.

No-Observability-Change: no metric, span, log key, or label changed. The
touched telemetry lines are doc comments; the registered instruments, the
`eshu_dp_*` metric names, and the span and attribute constants in
`go/internal/telemetry` are token-identical to base.
