# AGENTS.md — go/internal/cli/servicereport guidance for LLM assistants

## Read first

1. `go/internal/cli/servicereport/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/servicereport/doc.go` — the godoc contract
3. `go/cmd/eshu/service_report_cmd.go` — the cobra `RunE` wrapper that
   resolves process state (flags, stdin, stdout) and calls into this
   package. This is the file that shows how the two halves fit together.
4. `go/internal/cli/servicereport/render_contract_test.go` — `renderKeyContract`,
   the per-JSON-key record of what the text output prints and what it leaves
   out. When the prose in the docs and that map disagree, the map is right.
5. `go/internal/serviceintel/AGENTS.md` — the report-composition package
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
  pushing file reads into the wrapper. Outside its tests, the package's
  entire operating-system surface is those two `os.ReadFile` calls, the
  `io.ReadAll` of the `io.Reader` `ReadInput` is handed, and the `io.Writer`
  `RenderReport` formats into — every one of them behind a caller-supplied
  parameter. Production code here writes no files, opens no network
  connections, and runs no subprocesses. Keep it that way. (The package's
  tests do call `os.WriteFile`, into `t.TempDir`, to build the
  captured-response fixtures they feed back in; that is fixture setup, not
  package surface.)
- **No printing from this package except through `RenderReport`'s
  `io.Writer` parameter.** There is no `fmt.Print*` here — and none in the
  wrapper either: `service_report_cmd.go` writes through `cmd.OutOrStdout()`
  so cobra's output stays capturable in a test. New output goes to a writer
  the caller passed in, on both sides of the boundary; neither file should
  reach for the process's real stdout.
- **`RenderReport` is text-mode only.** The `--json` path still calls
  `ReadInput`, `ParseServiceStoryResponse`, and `SupplyChainSection` — both
  output modes share the whole input half — but it marshals
  `serviceintel.Report` itself and never reaches `RenderReport`. Do not
  route JSON output through `RenderReport` or vice versa.

## Common changes and how to scope them

- **Change what the captured-response envelope accepts** → edit
  `ParseServiceStoryResponse` in report.go. It is the single decode path for
  both captured inputs — the wrapper runs it over `ReadInput`'s bytes, and
  `SupplyChainSection` runs it over the supply-chain file — so a second
  decoder elsewhere would drift.
- **Change the text report's layout** → edit `RenderReport` (and its
  private helpers `reportSubjectLabel` / `nextCallLabel`) in report.go. The
  JSON shape is untouched by this package — it comes straight from
  `serviceintel.Report`'s JSON tags. If the change adds or drops a *field*
  rather than moving one around, update `renderKeyContract` in
  `render_contract_test.go` in the same edit and carry the wording into
  `doc.go`, `README.md`, and this file — the test fails until all of them
  agree.
- **Add a new captured-input file the command reads** → follow the
  `SupplyChainSection` shape: a function taking the file path plus whatever
  context it needs, returning a `serviceintel` input type or `nil` when the
  path is blank. Wire the new flag in `service_report_cmd.go` and pass the
  flag's value in as a parameter.

## Failure modes and how to debug

- Symptom: `service-report` reports "no service-story response provided"
  even though a file exists → check that `--from` is being read and passed
  to `ReadInput` before assuming the decode side is wrong. `ReadInput`
  raises that message only when the `--from` path is blank or
  whitespace-only *and* stdin was empty or all whitespace. A path holding
  any non-whitespace character always attempts the file read and surfaces
  the underlying OS error instead. The whitespace-only path is the trap:
  `--from "   "` is a non-empty flag value, but `ReadInput` branches on
  `strings.TrimSpace(path) != ""`, so it reads stdin and never opens the
  file. `TestReadInputWhitespacePathReadsStdin` pins that.
- Symptom: `--json` output differs from the text report's numbers → the
  wrapper composes one `serviceintel.Report` before it branches on
  `--json`, so the two modes cannot be computing different values. What
  differs is the rendering, and `RenderReport` — in this package — is the
  sole producer of the text form, so a numeric mismatch points here.
- Symptom: a field is in the `--json` output and missing from the text →
  usually not a bug. The text prints a fixed subset: the subject label, the
  report-level `supported` / `partial` / `truth_class`, per-section `status`,
  `title`, `summary`, `unsupported_reasons` and `limitations`, the next-call
  labels and reasons, and the suggested investigations. Everything else the
  JSON carries is absent by design — including `schema`, the report-level
  `truth` envelope and aggregated `limitations`, `subject.repo_id` /
  `subject.repo_name`, section `kind`, and the rest of every section's answer
  packet, `evidence_handles` among them. Treat it as a bug only when the
  missing field is in that printed subset. Do not answer this from the lists
  above: read `renderKeyContract` in `render_contract_test.go`, which is the
  classification in machine-checkable form, and the prose in `doc.go`,
  `README.md`, and here that has to agree with it.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has (a cobra flag, real
  process stdin), add a parameter instead.
- **Composing the report here.** `serviceintel.Compose` is called directly
  in the wrapper's `runServiceReport`, next to the JSON/text branch. This
  package prepares inputs (`ReadInput`, `ParseServiceStoryResponse`,
  `SupplyChainSection`) and renders the finished report (`RenderReport`) —
  those four exported functions are its whole surface, and none of them
  calls `serviceintel.Compose`.

## What NOT to change without an ADR

- Moving report composition (`serviceintel.Compose`) into this package. The
  current split keeps the JSON-vs-text branch, and the `serviceintel.Compose`
  call on this path, in the wrapper. That is a claim about the CLI path only:
  `Compose` has other production callers, in
  `go/internal/serviceintelhttp/handler.go` and
  `go/internal/answerquality/report_corpus.go`. Duplicating or relocating the
  CLI call needs an explicit design decision, not an incidental refactor.
