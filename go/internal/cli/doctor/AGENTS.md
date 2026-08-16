# AGENTS.md — go/internal/cli/doctor guidance for LLM assistants

## Read first

1. `go/internal/cli/doctor/README.md` — the six checks, why they are advisory,
   and why the report is a redaction surface
2. `go/internal/cli/doctor/doc.go` — the godoc contract
3. `go/cmd/eshu/doctor.go` — the cobra `RunE` wrapper that resolves process
   state (`NEO4J_URI` and its settings-file fallback, the config paths, the API
   base URL, `ESHU_POSTGRES_DSN`, `cmd.OutOrStdout()`) and calls into here
4. `go/internal/cli/evidredact/endpoint.go` — the redactor this package depends
   on, and the incident history behind its exact behaviour

## Invariants this package enforces

- **Nothing operator-supplied is printed verbatim.** This is the reason the
  package exists. `eshu doctor` previously printed `NEO4J_URI` straight to
  stdout, and a Bolt URI carries its password in userinfo. Every URL-shaped
  value goes through `evidredact.Endpoint`; the Postgres DSN is reported by
  presence only. Before adding any line to `Run`, ask whether the value can
  carry a credential — doctor output is the first thing pasted into a bug
  report, so printing it is publishing it.
- **The host is deliberately kept.** `evidredact.Endpoint` leaves
  `graph.example.com:7687` intact after removing the userinfo. Do not "improve"
  this by redacting the whole URI: an operator has to be able to tell which
  backend was configured, and a report that says only `[ok] configured` cannot
  distinguish the right backend from the wrong one.
- **Every check is advisory and `Run` returns nil.** Do not add an error return
  for a failed check. An operator running doctor wants the whole picture; the
  combination of findings is what identifies the cause, and returning early
  hides it.
- **No process wiring.** No cobra, no `os.Getenv`, no `os.Stdout`/`Stderr`, no
  `os.Exit`, no `fmt.Print*`. `TestPackageStaysProcessNeutral` parses the
  directory and fails on any of them. `os.Stat` and `os.FileInfo` ARE allowed —
  `os` is a legitimate dependency for the `Deps` seam, and the guard bans the
  process-bound selectors specifically, which is what a `go list -deps` scan
  cannot express.
- **The machine is described, not detected.** Filesystem and `PATH` access go
  through `Deps.Stat` and `Deps.LookPath`, and the probe through
  `Deps.HTTPClient`. A test that needs a missing binary must supply a
  `LookPath` that fails, never depend on the host it runs on.

## Common changes and how to scope them

- **Add a check** → add the value to `Deps`, resolve it in
  `go/cmd/eshu/doctor.go`, render it in `Run`. If the value can carry a
  credential, route it through `evidredact.Endpoint` or report presence only,
  and add a sentinel case to `doctor_redaction_test.go`.
- **Add a service binary** → append to `serviceBinaries`. The list is the set
  of executables an operator must be able to start by name; a missing one
  explains a stack that will not come up.
- **Change the probe timeout** → `healthTimeout`. It is bounded because doctor
  runs while something is already wrong, and a hung endpoint must not hang the
  report; an unreachable API is itself a finding worth printing.
- **Change output wording** → the lines are operator-facing and appear in
  support threads. Check `docs/public/reference/cli-reference.md` for pinned
  sample output before changing a prefix or a label.

## Failure modes and how to debug

- Symptom: a redaction test passes but a credential still appears in real
  output → the sentinel was planted at a token boundary. Plant it inside a
  value, with a varied preceding character, so a whole-field matcher cannot
  pass by accident.
- Symptom: `TestPackageStaysProcessNeutral` fails on a legitimate `os` call →
  check whether the selector is genuinely process-bound. `os.Stat` is fine;
  `os.Getenv` is not, and belongs in the wrapper.
- Symptom: the neutrality test passes on an empty package → it asserts floors
  on files scanned and selectors walked for exactly that reason. If you split
  this package, re-check those floors rather than deleting them.
- Symptom: a check reports differently on CI than locally → it is reading the
  host instead of `Deps`. That is the bug, not the environment.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It is `package main` and cannot be imported.
  Anything needing a flag, real stdin, or an exit code belongs in the wrapper.
- **Printing a raw endpoint "just for debugging".** That is the original
  defect. There is no debug-only path here; every line reaches the operator.
- **Making a check fatal.** See the advisory invariant above.
- **Testing against the real machine.** A test that passes only where the Eshu
  binaries happen to be installed is not a test of this package.
