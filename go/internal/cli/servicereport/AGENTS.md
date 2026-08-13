# AGENTS.md — go/internal/cli/servicereport guidance for LLM assistants

## Read first

1. `go/internal/cli/servicereport/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/servicereport/doc.go` — the godoc contract
3. `go/cmd/eshu/service_report_cmd.go` — the cobra `RunE` wrapper that
   resolves process state (flags, stdin, stdout) and calls into this
   package. This is the file that shows how the two halves fit together.
4. `go/internal/serviceintel/AGENTS.md` — the report-composition package
   this one adapts captured input into; do not duplicate composition logic
   here.

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, no reads of Eshu
  config or a credential from the process environment, no reading of the
  process's actual `os.Stdin` (the wrapper passes `cmd.InOrStdin()` in as an
  `io.Reader` parameter). `go/cmd/eshu` is `package main`, so nothing can
  import it — any symbol that reads a flag or maps to an exit code has to
  live in `service_report_cmd.go` instead.

  `ReadInput` and `SupplyChainSection` both call `os.ReadFile` directly on a
  path parameter the caller supplies. That is not process wiring — it is
  the same "act on an explicit parameter" shape as
  `internal/cli/mcpsetup`'s `WriteMCPServerConfig`. Do not "fix" it by
  pushing file reads into the wrapper. Those two reads are the package's
  entire operating-system surface: it writes no files, opens no network
  connections, and runs no subprocesses. Keep it that way.
- **No printing from this package except through `RenderReport`'s
  `io.Writer` parameter.** `fmt.Print*` (writing straight to the process's
  real stdout) belongs only in `service_report_cmd.go`.
- **`RenderReport` is text-mode only.** The `--json` path in the wrapper
  marshals `serviceintel.Report` directly and never calls into this
  package; do not route JSON output through `RenderReport` or vice versa.

## Common changes and how to scope them

- **Change what the captured-response envelope accepts** → edit
  `ParseServiceStoryResponse` in report.go. It is the single decode path
  both `ReadInput`'s caller (the wrapper) and `SupplyChainSection` use, so a
  second decoder elsewhere would drift.
- **Change the text report's layout** → edit `RenderReport` (and its
  private helpers `reportSubjectLabel` / `nextCallLabel`) in report.go. The
  JSON shape is untouched by this package — it comes straight from
  `serviceintel.Report`'s JSON tags.
- **Add a new captured-input file the command reads** → follow the
  `SupplyChainSection` shape: a function taking the file path plus whatever
  context it needs, returning a `serviceintel` input type or `nil` when no
  path was given. Wire the new flag in `service_report_cmd.go` and pass the
  flag's value in as a parameter.

## Failure modes and how to debug

- Symptom: `service-report` reports "no service-story response provided"
  even though a file exists → cause is almost always the wrapper: check
  that `--from` is being read and passed to `ReadInput` before assuming
  this package is wrong. `ReadInput` raises that particular message only
  when no `--from` path was given and stdin was empty or all whitespace; a
  non-empty `--from` path always attempts the file read and surfaces the
  underlying OS error instead.
- Symptom: `--json` output differs from the text report's numbers → this
  package cannot be the cause. Both output modes render the same
  `serviceintel.Report` value the wrapper builds once; `RenderReport` never
  runs during `--json` output and vice versa.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has (a cobra flag, real
  process stdin), add a parameter instead.
- **Composing the report here.** `serviceintel.Compose` is called directly
  in the wrapper's `runServiceReport`, next to the JSON/text branch. This
  package only prepares inputs (`ParseServiceStoryResponse`,
  `SupplyChainSection`) and renders the finished report
  (`RenderReport`) — it does not call `serviceintel.Compose` itself.

## What NOT to change without an ADR

- Moving report composition (`serviceintel.Compose`) into this package. The
  current split keeps the JSON-vs-text branch, and the one
  `serviceintel.Compose` call, in the wrapper; duplicating or relocating it
  needs an explicit design decision, not an incidental refactor.
