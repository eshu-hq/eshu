# Evidence-Packet Dogfood CLI Logic

## Purpose

`evidpacket` holds the logic behind `eshu evidence-packet-dogfood`: getting the
captured benchmark artifact's bytes (from a file or from a reader), rendering a
scored verdict as the text report an operator sees, and joining the failed
criteria into the one-line summary that goes into the command's error.

The grading is not here. `internal/packetdogfood` parses a benchmark and scores
it; this package uses that package's `Verdict`, `Criterion`, and
`CriterionStatus` types and their constants, and calls nothing in it.

## Ownership boundary

This package owns input acquisition and presentation. It does not own process
wiring: cobra flags, the input and output streams, the
`ParseBenchmark`/`Score` call pair, and the mapping from a failing verdict to a
non-zero exit stay in `go/cmd/eshu/evidence.go`. They stay
there because `go/cmd/eshu` is `package main` and nothing can import it, so any
symbol touching flags, process environment, or the exit-code contract has to
live on that side of the seam.

## Exported surface

- `ReadBenchmark(stdin io.Reader, path string) ([]byte, error)` — returns the
  raw benchmark bytes. Reads `path` when it has non-space content, and the
  supplied reader otherwise. Returns bytes only; validation is
  `packetdogfood.ParseBenchmark`'s job.
- `RenderVerdict(verdict packetdogfood.Verdict) string` — the operator-facing
  text report. Returns the text rather than writing it, so the caller owns the
  stream.
- `FailureSummary(verdict packetdogfood.Verdict) string` — the failed criteria
  joined with `; `, or `unknown failure` when no criterion failed.

`marker` (the `[ok]`/`[!!]`/`[--]` glyph) and `quoteIfEmpty` stay unexported;
only `RenderVerdict` calls them.

## What it touches

The list below covers `dogfood.go`, the only non-test file here that carries
code. The package's other non-test file, `doc.go`, holds a package comment and
the `package evidpacket` clause and nothing else, so it touches nothing.
`dogfood_test.go` sits in the same package and does write files: `os.WriteFile`
under a `t.TempDir()`. `t.TempDir()` calls
`os.MkdirTemp(os.Getenv("GOTMPDIR"), …)`, so the variable it reads first is
`GOTMPDIR`; only when that is unset does `os.MkdirTemp` fall back to
`os.TempDir()`, which reads `TMPDIR`. No workflow, script, or Makefile in this
repo sets `GOTMPDIR`, so in practice the directories land under `TMPDIR`. That
fallback is how the wrong variable went unnoticed for six review rounds. All of
this is scaffolding rather than shipped behavior, but the list is only true once
you say which source it covers.

- **Filesystem reads** — one: `os.ReadFile(path)` inside `ReadBenchmark`, where
  `path` is the operator's `--from` value.
- **Filesystem writes** — none. `dogfood.go` creates, truncates, renames, or
  chmods nothing, and makes no temporary file. Nothing here produces an
  evidence packet; the artifact it reads is a captured JSON benchmark *about*
  evidence packets.
- **Process environment** — none, and the indirect routes are absent too: no
  `os.Getenv`/`os.LookupEnv`; no `os.UserHomeDir`/`os.UserConfigDir`, which
  would read `HOME` or `USERPROFILE`; no `os.CreateTemp`/`os.MkdirTemp`/
  `os.TempDir`, which would read `TMPDIR`; no `exec.Command`, which would
  resolve through `PATH`; no HTTP client, whose default transport would honour
  `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`.
- **Subprocesses** — none.
- **Network** — none.
- **Standard streams** — none directly. `ReadBenchmark` calls `io.ReadAll` on
  the reader it is handed and never reaches for `os.Stdin`. `dogfood.go` does
  call `fmt.Fprintf` and `fmt.Fprintln`, but every one of them targets the local
  `strings.Builder` in `RenderVerdict`; there is no `fmt.Print`, `fmt.Printf`,
  or `fmt.Println`, and no write to `os.Stdout` or `os.Stderr`.

## Rendered report shape

`RenderVerdict` emits, in order: an `Evidence-packet dogfood PASSED` or
`... FAILED` header; `  run     : <run id> (<run kind>)`, where an empty or
all-space run id becomes the literal `<repo>`; `  tasks   : <count>`;
`  families: <comma-separated>`; a 44-character `-` rule; then one
`  <glyph> <name>: <detail>` line per criterion. The string ends in a newline,
which is why the wrapper prints it with `fmt.Fprint` and not `fmt.Fprintln`.
Two tests hold that layout: `TestRenderVerdictPassedLayout` here, and
`TestEvidencePacketDogfoodFailsAndExitsNonZero` in `go/cmd/eshu`, which compares
a literal against every byte the command wrote to stdout and so catches an extra
byte the wrapper adds. That test then asserts separately that stderr came back
empty. The two assertions need `runDogfoodCmd` to give each stream its own
buffer, through its `cmd.SetOut` / `cmd.SetErr` pair in
`evidence_packet_dogfood_cmd_test.go`; when the helper merged
them, a wrapper that sent the entire report to stderr still passed.

## Errors an operator can see

From this package:

- `read dogfood benchmark from stdin: <cause>`
- `read dogfood benchmark file "<path>": <cause>` — a missing file, a directory
  passed as `--from`, and an unreadable file all arrive here, each carrying the
  raw `*fs.PathError` underneath. The wrap repeats the path that the
  `*fs.PathError` already names, so an operator reads it twice. That is a
  pre-existing defect, not a choice. Correcting it changes operator-visible
  output, so it belongs in its own change rather than riding along with one.

Those two are not everything `eshu evidence-packet-dogfood` can print. Two other
sources reach the same operator:

- `packetdogfood.ParseBenchmark`'s decode and validation errors, which the
  wrapper returns unwrapped: `decode dogfood benchmark: <cause>` for malformed
  JSON, then the schema, empty-task, missing-family, approach-vocabulary, and
  non-positive-measurement checks. This is what the commonest bad input
  produces — an empty stdin gives `decode dogfood benchmark: unexpected end of
  JSON input` and exit 1.
- The wrapper's own `evidence-packet dogfood FAILED: <FailureSummary output>`
  and `write dogfood verdict JSON: <cause>`.

## Dependencies

- `internal/packetdogfood` — types and constants only. That package itself
  imports nothing beyond `encoding/json`, `fmt`, `sort`, and `strings`, so no
  environment read, subprocess, or network call hides behind it.
- Consumed by `go/cmd/eshu`'s `evidence.go` (all three
  exported functions) and by `go/internal/cli/compparity/exercises.go`
  (`FailureSummary`, in `exerciseEvidencePacketDogfoodFixture`).

## Telemetry

None. Scoring runs inline with a single CLI invocation and reports through the
command's own output and exit code; there is no background pipeline stage to
instrument.

## Gotchas / invariants

- `quoteIfEmpty` is a verbatim copy of the helper in `go/cmd/eshu/first_run.go`,
  which this package cannot import. Its `<repo>` placeholder reads oddly for a
  run id. That is pre-existing behavior, kept unchanged because #6059 is a move.
  Fixing the wording would change operator-visible output and belongs in its own
  change.
- `packetdogfood.Score` never emits `CriterionSkip` today, so `marker`'s `[--]`
  glyph is reachable only from a hand-built verdict. `TestRenderVerdictSkipMarker`
  pins it anyway, so a future skip criterion does not render as a blank.
- A `--from` value that is empty or all whitespace falls back to the reader. A
  caller must not treat "path was supplied" as "a file will be opened".
- Both error prefixes this package emits are pinned, one test each.
  `TestReadBenchmarkWrapsMissingFileWithoutHidingTheCause` asserts
  `read dogfood benchmark file "<path>": `, and
  `TestReadBenchmarkWrapsStdinFailure` asserts
  `read dogfood benchmark from stdin: `. Nothing in the repo greps for the
  strings, so rewording one breaks no script. It turns
  `go test ./internal/cli/evidpacket` red, and two required checks on `main` go
  red with it — `go-race-complete` and `required-gates-complete`, described
  under "Which CI checks run these tests" below. Whether the wordings survived
  the move into this package is a separate question, answered under "How the
  move was checked". Reword a prefix only when the rewording is the point of
  the change, and update both tests in the same change.

## Which CI checks run these tests

Two required checks on `main` go red when this package's tests fail. Three CI
jobs run the tests; the third feeds no required check. Both sweeps behind that
count are written out below the table — rerun them rather than trusting the
number.

| Required check | Job that runs the tests | Command |
| --- | --- | --- |
| `go-race-complete` | `go-race` in `.github/workflows/test.yml`, step `Run Go tests with race detector`, four shards over `go list ./...` | `go test -count=1 -race -timeout 900s -p 2` on the shard's slice |
| `required-gates-complete` | `macos` in `.github/workflows/macos.yml`, step `Run Go tests`, reached through the `macos-build` gate in `specs/ci-gates.v1.yaml`, which is `blocking: true` and triggers on `go/**` | `go test ./... -count=1 -timeout 300s`, `working-directory: go` |

`required-gates-complete` is the status that aggregates every path-selected
blocking gate; `.github/workflows/required-gates.yml` lists `macOS CI` among the
workflows it waits on. All three required context names live in the repository's
mirror of the `main` ruleset, the `required_status_checks:` block at the top of
`specs/ci-gates.v1.yaml`.

Nothing in this section is cited by line number. Line ranges here rotted once
already: PR #6107 inserted a gate into `specs/ci-gates.v1.yaml` above both of
the blocks cited here and moved everything below it down 27 lines, and nothing
flagged it. No gate in this repo checks a `file:line` reference into `specs/`,
into a workflow, or into a script. The one citation gate,
`scripts/verify-doc-citations.sh`, validates `<path>.go::TestName` citations and
`tests/fixtures`/`testdata` paths, and it stayed green across the rot. It could
not have caught it either way: its scan root is `docs/public`, so it never opens
this file or the `AGENTS.md` beside it. Nothing checks the citations here. Gate
`id:` values, job ids, and step `name:` values all survive an insertion above
them, so this section cites those instead — `rg -n 'id: macos-build'
specs/ci-gates.v1.yaml` finds the gate block whatever moves above it.

The job that feeds no required check is `coverage-report`, named "Generate Go
code coverage report", in `.github/workflows/code-coverage-report.yml`. It has
no `go test` line of its own: it runs
`scripts/generate-code-coverage-report.sh`, whose only `go test` invocation is
`(cd "${repo_root}/go" && go test ./... -covermode=count …)` under
`set -euo pipefail` with no `|| true`, so a red test here fails the job. Three
separate things keep it off the required list: the `code-coverage-report` gate in
`specs/ci-gates.v1.yaml` is `blocking: false`, its workflow is not among the ones
`.github/workflows/required-gates.yml` waits on, and the job's
`if: github.event_name != 'pull_request'` keeps it off the PR path entirely.

Four rounds of review missed that job, all for the same reason: each sweep ran
`rg 'go test' .github/workflows/`, which cannot see a job that reaches `go test`
through a script. The sweep that finds it is
`rg -n 'go test' scripts/ | rg '\./\.\.\.'`. It currently returns two files:
`scripts/generate-code-coverage-report.sh`, and `scripts/dev/pre-pr.sh`, where
the hit sits in `step_race`'s `ESHU_PRE_PR_FULL_RACE=1` branch, the local
`make pre-pr-full` whole-module race lane rather than a CI job. Run both sweeps
before changing any count in this section.

The remaining required context, `go-core-complete`, stays green through a reword
here, because its `go-core` job runs no `go test` against the `go/` module at
all. Its steps, in the `go-core` job of `.github/workflows/test.yml`, are:
install ripgrep;
install golangci-lint; build the `filelength` and `dirgate` golangci-lint plugin
modules under `tools/`; run `scripts/test-verify-dirgate.sh` and
`scripts/test-generate-dirgate-grandfather-go.sh`; run the `dirgate` plugin
module's own `go test ./... -count=1`; run `scripts/verify-dirgate.sh --all`;
lint `go/` with `golangci-lint run ./...`; check gofumpt formatting with
`golangci-lint fmt --diff`; and finish with `go build ./...`. The one `go test`
in that list runs in `tools/golangci-lint-dirgate`, which is its own Go module
outside `go/`, so it never reaches this package.

## How the move was checked

#6059 moved this code out of `go/cmd/eshu` and changed no behavior. The check
behind that claim compares source, not compiled output, because a binary check
does not survive a rebase: the `eshu` binary carries every other command in
`go/cmd/eshu`, so an unrelated commit landing on the base changes its bytes
without anything here changing.

The method, which anyone can rerun:

1. Side A is the pre-move `go/cmd/eshu/evidence.go` as of the
   commit before the move, plus `quoteIfEmpty` from `go/cmd/eshu/first_run.go`.
   Side B is today's `evidence.go` together with
   `dogfood.go` here.
2. Parse both sides with `go/parser` and without `parser.ParseComments`, which
   drops every comment.
3. Rewrite identifiers through the move's rename map: `readDogfoodBenchmark` to
   `ReadBenchmark`, `renderDogfoodVerdict` to `RenderVerdict`, `dogfoodMarker`
   to `marker`, `dogfoodFailureSummary` to `FailureSummary`.
4. Reprint each top-level function with `go/printer` so whitespace is canonical,
   then delete the `evidpacket.` package qualifier as text.
5. Compare the two strings per function name.

Eight functions on each side, none present on only one side. Six come out
byte-identical: `init` (3 normalized lines), `newEvidencePacketDogfoodCommand`
(26), `ReadBenchmark` (14), `marker` (10), `FailureSummary` (12), and
`quoteIfEmpty` (6).

Two differ, both from the single intended change — `RenderVerdict` returning a
string instead of taking an `io.Writer`:

- `RenderVerdict` drops the `w io.Writer` parameter, gains a `string` result,
  opens with `var b strings.Builder`, ends with `return b.String()`, and retargets
  its six writes from `w` to `&b` — the pre-move side has six writes to `w`, and
  the count is unchanged. Every format string and every argument is unchanged,
  which is why the emitted bytes are unchanged.
- In `runEvidencePacketDogfood`, `RenderVerdict(cmd.OutOrStdout(), verdict)`
  became `fmt.Fprint(cmd.OutOrStdout(), RenderVerdict(verdict))`. `fmt.Fprint`
  adds no newline, so the report still ends where it did.

Step 2 throws comments away, so the `#nosec G304 ... //nolint:gosec` directive
on the `os.ReadFile` call was compared on its own as text. The two lines match.

Limits worth knowing. The comparison covers the moved functions and not the
tests, and it says nothing about `internal/packetdogfood`, which the move did
not touch. It also proves sameness of source, not of behavior; the rendered
bytes are held by `TestRenderVerdictPassedLayout` here and by
`wantFailingDogfoodReport` in `go/cmd/eshu`.

## Related docs

- `docs/public/reference/local-testing/evidence-packet-dogfood.md` — how to
  capture and score a benchmark run
- `docs/public/reference/cli-reference.md` — the command's operator-facing entry
