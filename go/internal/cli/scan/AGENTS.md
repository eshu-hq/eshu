# AGENTS.md — go/internal/cli/scan guidance for LLM assistants

## Read first

1. `go/internal/cli/scan/README.md` — purpose, ownership boundary, exported
   surface
2. `go/internal/cli/scan/doc.go` — the godoc contract, including the exact
   environment this package reads
3. `go/cmd/eshu/scan.go` — the thin cobra `RunE` wrapper. `defaultScanRuntime`
   there is where every process seam is wired; it is the file that shows how
   the two halves fit together.
4. `go/cmd/eshu/first_run_index.go` and `go/internal/cli/vulnscan/run.go`
   (`RunRepo`, with the `scan.Runtime` wired by `go/cmd/eshu/vuln_scan.go`) —
   the two other production callers of `Execute`, and the reason its exported
   surface is wider than `eshu scan` alone needs.

## Invariants this package enforces

- **No process wiring here.** No cobra, no `os/exec`, no `net/http`, no
  `os.Exit`, no printing to the process streams. `go/cmd/eshu` is
  `package main`, so nothing can import it — any symbol that reads a flag,
  runs a child process, or maps to an exit code belongs in `scan.go` there.

  The environment this package reads is scan-scoped, not wiring. There are
  **two deliberate reads**, and the second resolves a different variable per
  platform:

  1. `ESHU_GRAPH_BACKEND` (`CurrentGraphBackend`) — the truth envelope's
     backend label.
  2. `eshulocal.BuildLayout(os.Getenv, os.UserHomeDir, runtime.GOOS, root)`
     (`ReposDir`) — the managed home. `ESHU_HOME` when set, used with `~`
     expanded and **no** `eshu` segment appended. Otherwise `LOCALAPPDATA`
     then `os.UserHomeDir` on Windows, and `XDG_DATA_HOME` then
     `os.UserHomeDir` everywhere else. `HOME` and `USERPROFILE` count —
     `os.UserHomeDir` is defined as reading them, and this package passes
     that callback in itself.

  Two stdlib boundaries also read process state with no visible `os.Getenv`:
  `filepath.Abs` resolves against the working directory, and `os.UserHomeDir`
  is reached through the callback above.

  A new *deliberate* env read belongs in the wrapper as a `Runtime` field, not
  here. Before editing this list, re-derive it — `rg 'os\.[A-Z]|filepath\.[A-Z]'`
  over the package, then follow each callee — rather than amending the sentence
  a reviewer complained about.

- **`WaitFlag` is a name, not a flag read.** `internal/cli/vulnscan` prints
  `--wait=true` in its not-ready message, and `go/cmd/eshu/AGENTS.md` requires
  a flag name printed by an `internal/cli` package to be declared by one owner,
  so this package declares the scan family's flag name and `go/cmd/eshu/scan.go`
  registers it from here. It is the only flag name here; the flag itself is
  still read in the wrapper.
- **Every process collaborator arrives through `Runtime`.** `Execute` validates
  `Client`, `Environ`, `LookPath`, `RunBootstrap`, `FetchStatus`, and
  `FetchQueryProbe` and returns a `scan: Runtime.X is required` error naming the
  gap. Do not add a package-level `var` seam to make a test easier; that is the
  shape this extraction removed, and it lets one test's stub leak into another.
  `Now` and `Wait` are the only optional fields, because neither touches the
  process, PATH, or the network.

- **Error text is an operator contract.** `go/.golangci.yml` excludes
  `cmd/` from `wrapcheck` but not `internal/cli/`, so returns moved here draw
  wrap suggestions. Wrapping `ResolveTarget`'s `filepath.Abs` /
  `eshulocal.ResolveWorkspaceRoot` errors, or the context errors in
  `waitForReadiness` and `waitInterval`, changes what `eshu scan` prints and
  what callers match on. The `//nolint:wrapcheck` comments on those lines are
  intentional — do not "fix" them.

- **A `Result` is returned with every error.** `newResult` seeds
  `Status: "failed"` so an early return cannot read as success. Never return a
  bare `Result{}` next to an error; the wrapper renders that result into the
  failure envelope.

- **Readiness is drained-and-healthy, never process health.** `EvaluateReadiness`
  is the single rule, and `go/cmd/eshu`'s first-run and hosted-verify paths
  reuse it precisely so there is only one. A status report with no completed or
  active generation is not-ready, not drained.

- **`mergeEnv` and `pathExists` are deliberate copies** of `go/cmd/eshu`'s
  `mergeEnvironment` and `pathExists`, which stay there for their callers
  outside the scan family. A behavior change to one is a bug unless made to
  both. The parity tests in `go/cmd/eshu/scan_parity_test.go` enforce this:
  `TestScanMergeEnvMatchesMergeEnvironment` and
  `TestScanPathExistsMatchesScanCommandProbe` run shared input tables through
  both sides, and `TestScanEnvAndPathCopiesAreTokenIdentical` pins the
  function bodies token-identical.

## Common changes and how to scope them

- **Add a readiness condition** → edit `EvaluateReadiness` in `status.go` and
  add a case to `TestEvaluateReadinessTerminalCases`. Why: three call sites
  outside this package (`first_run_index.go`, `first_run_diagnostics.go`,
  `hosted_setup_verify.go`) depend on that one function agreeing with `Execute`;
  a condition added at a call site instead makes them disagree.
- **Change what the bootstrap child receives** → edit `Options.BootstrapArgs`
  or `Options.BootstrapEnv`. They are the only place the child's argv and
  environment are built, and `TestBootstrapEnv*` pins the override precedence.
- **Add a field to the result envelope** → `Result` and its blocks in
  `status.go`. The JSON tags are a CLI wire contract; `Truth` is rendered as a
  sibling of `data` by the wrapper, which is why it carries `json:"-"`.

## Failure modes and how to debug

- Symptom: `scan: Runtime.<field> is required` → a caller built a `Runtime`
  literal instead of going through `go/cmd/eshu`'s `scanRuntimeFor`. In tests,
  it usually means `stubScanRuntime` was not called, or a field was cleared
  after it was.
- Symptom: the bootstrap child cannot find a binary or a config it used to
  find → check `Runtime.Environ`. `Options.RuntimeEnv`, when set, *replaces*
  the process base rather than merging into it.
- Symptom: `eshu scan` prints a longer error string than before → something
  added a `fmt.Errorf` wrap to satisfy `wrapcheck`. See the invariant above.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported. If new logic needs
  something only the wrapper has, add a `Runtime` field or a parameter.
- **Reintroducing a package-level mutable seam** (`var Now = time.Now` and
  friends). Exported mutable globals were what this extraction replaced.
- **Reporting a scan ready on process health.** Readiness comes from
  `EvaluateReadiness` on a status report, nothing else.

## What NOT to change without an ADR

- The `Runtime` field set and the `Client` interface. `go/cmd/eshu` wires both
  structurally, and four further command families (first-run, vuln-scan,
  hosted, demo) are scheduled to extract against this exact surface under epic
  #6053 — a change here lands in all of them.
- The `Result` JSON tags. They are the `eshu scan --json` wire contract.
