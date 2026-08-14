# AGENTS.md — go/internal/cli/evidpacket guidance for LLM assistants

## Read first

1. `go/internal/cli/evidpacket/README.md` — purpose, ownership boundary,
   exported surface, the enumerated list of what this package touches, and
   "How the move was checked", which gives the reproducible source-level
   comparison behind the claim that #6059 changed no behavior
2. `go/internal/cli/evidpacket/doc.go` — the godoc contract, which states the
   same list
3. `go/cmd/eshu/evidence_packet_dogfood_cmd.go` — the cobra `RunE` wrapper that
   resolves flags and streams and calls in here. This is the file that shows how
   the two halves fit together.
4. `go/cmd/eshu/competitive_parity_cmd.go` — the second consumer, and the one
   that is easy to forget. `exerciseEvidencePacketDogfoodFixture`
   scores a committed fixture benchmark under `eshu competitive-parity validate`
   and puts `FailureSummary`'s line in its own error. It calls neither
   `ReadBenchmark` nor `RenderVerdict`.
5. `go/internal/packetdogfood/README.md` — the package that actually parses and
   scores a benchmark. If the change is about *grading*, it belongs there.

## Invariants this package enforces

- **No process wiring here.** `dogfood.go` declares no cobra flag, reads no
  environment variable, writes to no output stream, and decides no exit status.
  Its `fmt.Fprintf` and `fmt.Fprintln` calls all target a local
  `strings.Builder`. `go/cmd/eshu` is `package main`, so nothing can import it;
  any symbol that reads a flag, reads the environment, or maps a verdict to a
  process exit code has to live in `evidence_packet_dogfood_cmd.go` instead.

- **`dogfood.go` writes no file.** Its only filesystem call is
  `os.ReadFile(path)` in `ReadBenchmark`. It creates, truncates, renames, and
  chmods nothing, and makes no temporary file. The first three bullets in this
  section name `dogfood.go` rather than the package on purpose: `dogfood_test.go`
  is in the same package, writes files with `os.WriteFile`, and puts them under
  a `t.TempDir()` — which calls `os.MkdirTemp(os.Getenv("GOTMPDIR"), …)`, so it
  reads `GOTMPDIR` first and falls back to `os.TempDir()`, and therefore
  `TMPDIR`, only when `GOTMPDIR` is unset. `os.MkdirTemp` is one of the calls
  the next bullet rules out for `dogfood.go`.
  The command name misleads as well: the artifact is a captured JSON benchmark
  *about* evidence packets, not a packet this code produces. If a change needs
  to write something, that is new behavior. Say so out loud rather than
  slipping it in as part of a refactor.

- **`dogfood.go` reads no process environment.** The indirect routes are absent
  as well, and they are what makes this easy to get wrong:
  `os.UserHomeDir`/`os.UserConfigDir` read `HOME`/`USERPROFILE`;
  `os.CreateTemp`/`os.MkdirTemp`/`os.TempDir` read `TMPDIR`; `exec.Command`
  resolves through `PATH`; an `http.Client` with a nil `Transport` honours
  `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`. Adding any of those is an environment
  read whether or not `os.Getenv` appears in the diff, and it makes the
  statements in `doc.go` and `README.md` false — update them in the same change.
  A sibling extraction in this epic drafted exactly that mistake: an
  `os.UserHomeDir` passed in as a callback made `HOME` decide an output path
  while the package documents still said the package read nothing. Review caught
  it before that PR merged (commit `7dcb9d693`, PR #6104), so the mistake is not
  in the tree: `go/internal/cli/graphinstall/doc.go` names `ESHU_HOME`, `HOME`,
  `XDG_DATA_HOME`, `LOCALAPPDATA`, and `USERPROFILE` today. A draft got that
  far, which is why this invariant is written down rather than assumed.

- **Render functions return strings.** `RenderVerdict` builds a
  `strings.Builder` and hands back text; the caller owns the stream. Do not add
  an `io.Writer` parameter and do not print.

- **Grading stays in `internal/packetdogfood`.** This package references that
  one's types and constants and makes no call into it. A new criterion, a new
  threshold, or a schema change belongs there, not here.

## Common changes and how to scope them

- **Change what the text report shows** → edit `RenderVerdict`. Why: it is the
  single renderer, and its exact byte layout is pinned twice —
  `TestRenderVerdictPassedLayout` here, and `wantFailingDogfoodReport` in
  `go/cmd/eshu/evidence_packet_dogfood_cmd_test.go`, which holds every byte the
  command wrote to stdout. Update both in the same change so the new layout is
  stated, not merely observed.
- **Add a status glyph** → edit `marker`. Why: it is the one switch over
  `packetdogfood.CriterionStatus`; a second one elsewhere would drift the
  `[ok]`/`[!!]`/`[--]` column.
- **Change where the benchmark comes from** → edit `ReadBenchmark`. Keep it
  taking an `io.Reader`; the wrapper passes `cmd.InOrStdin()`, and a direct
  `os.Stdin` reference here would make the function untestable and would break
  the cobra tests in `cmd/eshu` that feed a string reader.
- **Change the failure summary** → edit `FailureSummary`, then read both callers
  before you decide the change is contained. Why: two commands carry its line
  into an error an operator reads. `eshu evidence-packet-dogfood` returns
  `evidence-packet dogfood FAILED: <summary>`
  (`go/cmd/eshu/evidence_packet_dogfood_cmd.go`), and
  `eshu competitive-parity validate` returns `dogfood fixture failed: <summary>`
  (`go/cmd/eshu/competitive_parity_cmd.go`). Both prefixes are grep-able; the
  call in each file is the `FailureSummary` one. The join separator and the
  `unknown failure` fallback are pinned by
  `TestFailureSummaryJoinsEveryFailedCriterion` and
  `TestFailureSummaryFallsBackWhenNothingFailed` here; nothing in
  `go/cmd/eshu` asserts either command's composed message, so a reword shows up
  in this package's tests and nowhere else.
- **Change the error text** → update
  `TestReadBenchmarkWrapsMissingFileWithoutHidingTheCause` and
  `TestReadBenchmarkWrapsStdinFailure` in the same change. Both assert a prefix
  verbatim, so the package's tests fail until you do. Those prefixes are what an
  operator reads on a bad `--from`.

## Failure modes and how to debug

- Symptom: the command prints the report and then the shell prompt lands on the
  last criterion line, or a blank line appears before the prompt → cause: the
  wrapper switched between `fmt.Fprint` and `fmt.Fprintln`. `RenderVerdict`
  already ends its string in a newline; the wrapper must use `fmt.Fprint`.
  `TestEvidencePacketDogfoodFailsAndExitsNonZero` compares
  `wantFailingDogfoodReport` against every byte the command wrote to stdout, so
  the swap now shows up as a red test in `go/cmd/eshu` instead of only at an
  operator's prompt. Stderr is a second assertion in the same test, not part of
  the constant; `runDogfoodCmd` in `evidence_packet_dogfood_cmd_test.go` gives
  each stream its own buffer, through its `cmd.SetOut` / `cmd.SetErr` pair, so
  the two can be told apart.
- Symptom: `--from somefile` reports a stdin error instead of a file error →
  cause: the path was empty or all whitespace, so `ReadBenchmark` took the
  reader branch. That fallback is deliberate; check the flag value first.
- Symptom: an operator reports the run line shows `<repo>` → cause: the
  benchmark artifact has an empty `run_id`. `quoteIfEmpty` substitutes that
  placeholder. It is a copy of `go/cmd/eshu/first_run.go`'s helper and its
  wording is wrong for a run id; it is kept as-is deliberately (see below).
- Symptom: a criterion renders as `[--]` → cause: a `CriterionSkip` status.
  `packetdogfood.Score` does not emit one today, so this means the scorer
  changed; check `internal/packetdogfood/score.go`.

## Anti-patterns specific to this package

- **Writing to a stream from here.** `RenderVerdict` returns a string on
  purpose. The `fmt.Fprint` onto the command's stdout belongs only in
  `evidence_packet_dogfood_cmd.go`.
- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`). If
  logic here needs something only the wrapper has, add a parameter or a narrow
  interface.
- **"Improving" `quoteIfEmpty`'s `<repo>` placeholder while doing something
  else.** It is operator-visible output. Changing it is a behavior change and
  needs to be the subject of its own change, with its own before/after.
- **Exporting a helper with no caller.** `marker` and `quoteIfEmpty` are
  unexported because only `RenderVerdict` uses them. Check for a real caller
  before exporting anything.

## What NOT to change without an ADR

- The `read dogfood benchmark file %q: %w` and `read dogfood benchmark from
  stdin: %w` wrappings. Both are what an operator reads on a bad `--from`. The
  file one also keeps the raw `*fs.PathError` reachable through `errors.Is`; the
  stdin one has no `*fs.PathError` to keep, because it passes through whatever
  the reader returned, which `TestReadBenchmarkWrapsStdinFailure` supplies as a
  plain `errors.New` sentinel. One test pins each prefix:
  `TestReadBenchmarkWrapsMissingFileWithoutHidingTheCause` for the file one,
  `TestReadBenchmarkWrapsStdinFailure` for the stdin one. Reword either and its
  test fails, turning `go test ./internal/cli/evidpacket` red.

  Two required checks on `main` go red with it:

  - `go-race-complete`, the umbrella over the sharded race job. Each shard runs
    `go test -race` on its slice of `go list ./...`, so the union covers this
    package (`.github/workflows/test.yml`, job `go-race`, step `Run Go tests
    with race detector`).
  - `required-gates-complete`, which aggregates every path-selected blocking
    gate. The `macos-build` gate in `specs/ci-gates.v1.yaml` is `blocking: true`
    and triggers on `go/**`; its job runs `go test ./... -count=1` with
    `working-directory: go` (`.github/workflows/macos.yml`, step `Run Go
    tests`). Cite both by a stable anchor rather than by line number:
    `rg -n 'id: macos-build' specs/ci-gates.v1.yaml` finds the gate block, and
    the step `name:` finds the job's test step. An insertion above shifts every
    line below it, and nothing in this repo checks a `file:line` reference into
    `specs/`, into a workflow, or into a script.

  Both context names come from the repository's mirror of the `main` ruleset,
  the `required_status_checks:` block at the top of
  `specs/ci-gates.v1.yaml`. That mirror lists a third required context,
  `go-core-complete`, which stays green: the `go-core` job behind it runs no
  `go test` against the `go/` module. Rewording both prefixes was run as a probe
  with `go test -overlay` over a scratch copy of `dogfood.go`: rc=1, and those
  two tests were the only failures. Rerun it that way rather than editing
  `dogfood.go` in place.

  A third CI job runs this package's tests and is required by nothing:
  `coverage-report` in `.github/workflows/code-coverage-report.yml`, which
  reaches `go test ./...` through `scripts/generate-code-coverage-report.sh`
  rather than a `go test` line of its own. Four review rounds missed it because
  they swept `.github/workflows/` and not the scripts those workflows call.
  README's "Which CI checks run these tests" carries the accounting and the two
  sweeps that produce it; run both before changing any count in either file.
- The rule that `--from` wins over the reader, and that a whitespace-only
  `--from` does not — operators pipe benchmarks into this command in scripts.
