# Evidence — moving the local Eshu service out of `package main` (#6059)

Why this file exists: the `verify-performance-evidence` gate content-flags nine
files in this package. It should — a process supervisor is full of `chan`,
`Mutex`, `goroutine`, `go func(`, lease and queue identifiers, and Cypher verbs
in the schema-bootstrap path. The gate reads the whole file on disk, not the
diff, so a move trips it even though no supervised behaviour changed.

## What changed, and what did not

`go/internal/cli/localsupervisor` is `go/cmd/eshu`'s local-service cluster moved
verbatim. Sixteen production files and their tests changed package; identifiers
the CLI wrapper still needs became exported. Beyond the move:

- Operator progress output takes an `io.Writer` instead of writing to
  `os.Stderr`, so a caller can capture it. The CLI passes `os.Stderr` and
  `os.Stdout`, which is what those calls already used.
- Two dead functions were removed (`graphLifecycleNotWired`,
  `configureLocalChildProcessIO`), each with one production occurrence — its
  own declaration — and a test.

No worker count, batch size, poll interval, timeout, lock scope, claim order,
retry policy, or Cypher statement changed. No goroutine was added or removed.
`git diff` on the moved files is package clause, identifier case, writer
threading, and the two deletions.

## No-Regression Evidence:

Base `origin/main` 6db694d0628406764ca67a62f4bfdaea0b7b0313, branch
`6059-cli-localsupervisor`, macOS arm64, Go toolchain from `go/go.mod`.

**CLI parity, byte-identical.** 32 cases covering `local-host`, `graph`
(status/start/stop/logs/upgrade), `install nornicdb`, `mcp` (help/tools/start),
`api start`, and `serve start`, including failure paths. Each case declares the
exit code it expects from the BEFORE binary; a BEFORE mismatch, or both streams
empty, fails the harness rather than reporting a finding. Environment scrubbed
by `ESHU_*` name with `PATH` intact, same working directory for both runs.

    cases=32 harness_failed=0 parity_status=0   (rc=0)

stdout, stderr, and exit code identical on all 32.

**The harness can fail.** Rebuilding the after-binary with one mutated line in
`ValidateProgressMode` (`expected` -> `MUTANT expected`, a one-line source diff)
produced exactly one DIFFERS, on the one case that reaches that line:

    graph-start-bad-progress  1  1  DIFFERS(stderr)
    cases=32 harness_failed=0 parity_status=1   (rc=1)

**Test accounting is exact.** `=== RUN` lines, `-count=1 -v`:

| | before | after |
| --- | --- | --- |
| `./cmd/eshu` | 799 | 696 |
| `./internal/cli/localsupervisor` | — | 103 |
| total | 799 | 799 |

One test was deleted with its dead function and one added for the
`eshu graph upgrade` wrapper's flag forwarding, so the total is unchanged.

**Gates run, exit codes captured directly.**

| command | rc |
| --- | --- |
| `go build ./...` | 0 |
| `go build -tags nolocalllm ./...` | 0 |
| `go vet ./...` | 0 |
| `go vet -tags nolocalllm ./...` | 0 |
| `go test ./cmd/eshu/... ./internal/cli/... -count=1` | 0 |
| `go test -tags nolocalllm ./cmd/eshu/... ./internal/cli/localsupervisor/... -count=1` | 0 |
| `golangci-lint run --max-issues-per-linter=0 ./cmd/eshu/... ./internal/cli/...` | 0 (0 issues) |
| `scripts/verify-dirgate.sh --all` | 0 |
| `scripts/verify-package-docs.sh` | 0 |
| `mkdocs build --strict` | 0 |

Both build tags matter here: `graph_embedded_nornicdb.go` (`nolocalllm`) and
`graph_embedded_stub.go` (`!nolocalllm`) declare the same two names, so only one
is ever compiled and a single-tag build proves half the package.

The four `TestLocalAuthoritative*Envelope` tests pinned by
`specs/backend-conformance.v1.yaml` still match and still run — one `=== RUN`
each, before and after, all rc=0. That check matters because a `-run` filter
matching nothing exits 0 and reads exactly like a pass.

## No-Observability-Change:

This package emits no `eshu_dp_*` metric and opens no span, before or after.
Its operator signals are unchanged in content and destination: the progress
lines (`bootstrapping local postgres schema...`, `local graph schema ready`, the
two `warning:` lines) still reach `os.Stderr` because the CLI passes
`os.Stderr`; `eshu graph logs` still copies the backend log to `os.Stdout`; the
child service logs still land under the workspace log directory; the graph
backend log is still `<logs>/graph-nornicdb.log`; and the owner record
`eshu graph status` reads back is byte-identical, which the parity run's
`graph-status` cases exercise. The `slog.Info` line that reports a child exiting
cleanly is unchanged. The services this package supervises carry their own
instrumentation and were not touched.

## Not measured

No full-corpus or timed run. This change alters no hot path, so a throughput or
latency comparison would measure the machine, not the change. The parity table
and the two-tag test suites are the no-regression proof.
