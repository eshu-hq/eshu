# Evidence — moving the vuln-scan logic out of `package main` (#6059)

Why this file exists: the repo's evidence rules want a no-regression and an
observability answer for a change that moves runtime code, and the parity proof
below is that answer. Note that the `verify-performance-evidence` gate does
**not** fire on this diff, which is worth writing down because the sibling
extraction's evidence doc says the opposite about its own package:

    ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main scripts/verify-performance-evidence.sh
    verify-performance-evidence: no hot Cypher/concurrency/runtime files changed   (rc=0)

That is a real read, not a silent pass on an empty file set. The gate saw all 35
changed files (`git diff --name-only origin/main...HEAD | wc -l` = 35), its
location rule covers neither `go/internal/cli/*` nor `go/cmd/eshu`, and its
content pattern matches none of the 26 changed `.go` files. The pattern itself
is live: run against `go/internal/cli/localsupervisor/*.go` it flags 15 files.

The reason is worth knowing before someone adds a poll loop here. This package
waits on things — `waitForLocalOwner` and `waitLocalAPI` both tick until a
deadline — but it does so with `time.Ticker` and `select`, and every channel,
goroutine, and mutex behind the local service lives in
`internal/cli/localsupervisor`, which this package calls rather than
reimplements. Write `chan`, `Mutex`, `WaitGroup`, `goroutine`, or `go func(`
into a file here and the gate will start firing, and this note is where the
markers it wants already are.

## What changed, and what did not

`go/internal/cli/vulnscan` is `go/cmd/eshu`'s vuln-scan family moved, with the
cobra layer left behind. Eight production files left `cmd/eshu` entirely
(`vuln_scan_{exit,export,local,reachability,report,report_helpers,scope,vex}.go`);
`vuln_scan.go` and `vuln_scan_provider_parity.go` stayed as the wrappers. On
the test side three files moved whole
(`vuln_scan_{reachability,remediation,provider_parity_lifecycle}_test.go`) and
two more were assembled from tests lifted out of `vuln_scan_local_test.go`,
`vuln_scan_report_test.go`, and `vuln_scan_provider_parity_test.go`, whose
remaining cases drive the cobra command and stayed. Beyond the move:

- The renderers write through a `writef` helper instead of calling
  `fmt.Fprintf`/`Fprint`/`Fprintln` directly. The repo's wrapcheck linter
  exempts `go/cmd/*` but not `go/internal/cli/*`, and wrapping each write error
  individually would rewrite the text an operator sees when a write fails.
  `internal/cli/freshness` carries the identical helper for the identical
  reason. The three `fmt` calls produce the same bytes;
  `TestRenderSummaryFindingsLineMatchesTruncation` is what says so.
- `firstNonBlankString` was deleted. Its body was character-for-character
  `firstNonEmpty`'s, and every call site now uses `firstNonEmpty`.
- `LocalRuntime` carries `BaseURL string` where `vulnScanLocalRuntime` carried
  `Client *APIClient`. The API client type is declared in package main; the
  wrapper builds it from the address.
- `Result.Scan` is `any` where `vulnScanRepoResult.Scan` was `scanResult`. That
  type belongs to the `eshu scan` family, still in package main. The wrapper
  assigns it on every path that writes an envelope, so the marshalled bytes are
  unchanged — the JSON parity case below is the check.
- `ParityOptions` carries the resolved provider token instead of the name of the
  environment variable holding it. Reading the environment stays in the wrapper.

No guard, threshold, exit code, timeout, poll interval, endpoint, JSON field
name, or field order changed. No goroutine was added or removed.

## No-Regression Evidence:

Base `origin/main` b66af903bcc660e7eb37e697f0d1e937e08ba622, branch
`6059-cli-vulnscan`, macOS arm64, Go toolchain from `go/go.mod`.

**CLI parity, byte-identical.** 19 cases across `vuln-scan`, `vuln-scan repo`,
and `vuln-scan provider-parity`, including every failure path reachable without
a live API: flag validation, an unresolvable service URL in both text and
`--json` form, a missing repository path, a missing allowlist file, an unset
provider token, an unsupported provider, and a local alert summary. Each case
declares the exit code it expects from the BEFORE binary; a BEFORE mismatch, or
both streams empty, fails the harness rather than reporting a finding.
Environment scrubbed by `ESHU_*` name with `PATH` intact, same working directory
for both runs. Only wall-clock instants and elapsed-time values are normalized,
because they differ between two runs of the same binary.

    case_count=19 parity_fail=0 harness_fail=0

stdout, stderr, and exit code identical on all 19. The `--json` case compares a
4507-byte envelope carrying the full `data`, `report`, `scope_plan`, and
`scan_performance` blocks.

**The harness can fail.** Rebuilding the after-binary with one mutated line in
`vulnScanRepoOptionsFromCommand` (`--limit must be 200 or lower` ->
`... lowerX`, a one-line source diff) produced exactly one DIFFERS, on the one
case that reaches that line:

    repo_limit_over  2  2/2  DIFFERS_STDERR
    case_count=19 parity_fail=1 harness_fail=0

**Test accounting is exact.** `=== RUN` lines, `-count=1 -v`:

| | before | after |
| --- | --- | --- |
| `./cmd/eshu` | 721 | 710 |
| `./internal/cli/vulnscan` | — | 14 |
| total | 721 | 724 |

Eleven tests moved into the package, which is exactly the 11 `./cmd/eshu` lost:
the three local-runtime tests, the two reachability tests, the three
remediation tests, the provider-parity evidence test, the parity summary test,
and the summary renderer test.

The remaining three lines are `TestRenderSummaryFindingsLineMatchesTruncation`
and its two subtests, which are new. They exist because rerouting the renderer
through `writef` turned a `fmt.Fprint` and a `fmt.Fprintln` into `fmt.Fprintf`
calls, and nothing else pinned those bytes: the parity harness never reaches the
text summary, since every case it can run without a live API exits first. The
test fails on the mutation it is meant to catch — changing `" (truncated)"` to
`" (TRUNC)"` in `render.go` produced `Findings: 2 (TRUNC)` and a FAIL, and the
restored file passes.

**Gates run, exit codes captured directly.**

| command | rc |
| --- | --- |
| `go build ./...` | 0 |
| `go build -tags nolocalllm ./...` | 0 |
| `go vet ./...` | 0 |
| `go vet -tags nolocalllm ./...` | 0 |
| `go test ./cmd/eshu/... ./internal/cli/... -count=1` | 0 |
| `go test -race ./cmd/eshu/... ./internal/cli/... -count=1` | 0 |
| `go test -tags nolocalllm ./cmd/eshu/... ./internal/cli/vulnscan/... -count=1` | 0 |
| `scripts/dev/precommit-go.sh {fmt,lint,filecap,dirgate}` (argc=26 each) | 0 |
| `golangci-lint run --max-issues-per-linter=0 ./internal/cli/vulnscan/... ./cmd/eshu/...` | 0 (0 issues) |
| `scripts/verify-dirgate.sh --all` | 0 |
| `scripts/verify-package-docs.sh` | 0 |

Both build tags matter here: `PrepareLocalRuntime` starts the local
`local-host watch` owner, whose graph backend is compiled behind the
`nolocalllm` split in `internal/cli/localsupervisor`, so a single-tag build
proves half of what this package depends on.

The directory gate row for `cmd/eshu` was re-pinned from 96 to 88 files, digest
`432d5fc4…` to `8c15ae24…`, by editing `scripts/lib/dirgate-grandfather.tsv` and
regenerating `tools/golangci-lint-dirgate/grandfather.go` with
`scripts/generate-dirgate-grandfather-go.sh`. Perturbing the count up, the count
down, and the digest alone each produced a distinct failure and rc=1; the
restored row passes. No row was added for the new package — it is under the cap.

## No-Observability-Change:

This package emits no `eshu_dp_*` metric, opens no span, and writes no
structured log, before or after — `rg 'telemetry\.|tracer\.Start|slog\.'` over
the directory matches nothing. Its operator-facing signals are unchanged in
content and destination: the JSON envelope's `scan_performance` block (wall
time, repository size and file count, per-family fact counts, cache freshness,
scope mode, stop threshold) and `scope_plan` block (which guard fired and what
evidence was missing) carry the same fields with the same names; the human
summary prints the same lines to the same stream; the two `Starting local Eshu
service` / `Child service logs` notices still reach the writer the wrapper
passes, which is `cmd.ErrOrStderr()`; and the exit codes 0/3/4/5 still classify
the same readiness states. The parity run's `--json` case compares the whole
envelope byte for byte.

## Not measured

No full-corpus or timed run. This change alters no hot path — the scan itself,
the reducer, and the graph are untouched — so a throughput or latency comparison
would measure the machine, not the change. The parity table and the two-tag test
suites are the no-regression proof.
