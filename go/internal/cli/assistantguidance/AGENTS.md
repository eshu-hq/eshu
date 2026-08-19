# AGENTS.md — go/internal/cli/assistantguidance guidance for LLM assistants

## Read first

1. `go/internal/cli/assistantguidance/README.md` — purpose, ownership
   boundary, the managed-block rules, and the file-write surface table
2. `go/internal/cli/assistantguidance/doc.go` — the godoc contract
3. `go/cmd/eshu/assistant.go` — the cobra `RunE` wrapper that
   resolves process state (flags, `--path` against the working directory,
   `cmd.OutOrStdout()`) and calls into this package. It shows how the two
   halves fit together.
4. `docs/public/reference/assistant-guidance.md` — the operator-facing
   contract for the commands this package implements

## Invariants this package enforces

- **The managed block is the only region that may change.** Every write path
  goes through `Upsert` or `Remove`, and both are defined by byte slicing
  around the marker offsets `FindBlock` returns. Any change that rebuilds a
  file from parsed lines rather than slicing around those offsets breaks the
  property this package exists for.
- **A file with operator content is never deleted.** `Uninstall` deletes only
  when the post-removal content is whitespace-only. Do not relax that to "the
  file was created by install" — install does not record that, and a file
  Eshu created may have been edited since.
- **Nothing operator-supplied reaches the written block.** `GuidanceBody`
  reads only package constants and the `Platform` value. If you add a
  substitution into the body, you have created a redaction surface where
  there was none; `redaction_test.go` is the screen, and the sentinel must be
  planted inside a value with a varied preceding character, not at a token
  boundary.
- **No process wiring in this package.** No cobra, no `os.Stdout`/`Stderr`,
  no `os.Getenv`/`os.Getwd`, no `os.Exit`, no `fmt.Print*`.
  `TestPackageStaysProcessNeutral` parses the directory and fails on any of
  them. `go/cmd/eshu` is `package main`, so nothing can import it — a symbol
  that needs a flag or an exit code belongs in `assistant.go`.
- **Printing happens only through the caller's `io.Writer`.** `writeSuccess`
  and `writeTable` in render.go are deliberate copies of `go/cmd/eshu`'s
  `printSuccess` and `printTable` (unimportable from here). Their literals —
  `"OK %s\n"`, the two-space tabwriter padding, the 40-dash rule — must stay
  byte-identical to those helpers, because both render into the same CLI.

## Common changes and how to scope them

- **Change the guidance text** → edit `sharedGuidanceBody` in content.go.
  Every installed project's block goes stale until reinstalled, which is by
  design: `Classify` reports `BlockStale` and `status` shows "out-of-date".
- **Add a supported assistant** → add a row to `SupportedPlatforms` in
  content.go and, if it needs its own framing, a branch in `GuidanceBody`
  (Cursor's MDC front matter is the existing example). Update the
  `--platform` flag help and the "(supported: ...)" list in
  `SelectPlatforms`' error — that list is hand-written and does not derive
  from `SupportedPlatforms`.
- **Change the marker text** → this is an identity migration, not an edit.
  Every already-installed project carries the old markers, so the old pair
  would stop being found and a second block would be appended. Do not change
  it without a migration path.
- **Change output layout** → edit `renderInstallResults`, `RenderStatus`, or
  `RenderUninstall` in render.go, and check `docs/public/reference/cli-reference.md`
  and `docs/public/reference/assistant-guidance.md` for pinned sample output.

## Failure modes and how to debug

- Symptom: a reinstall rewrites the file every time → `Classify` is comparing
  a body that is not byte-stable. `RenderBlock` trims surrounding newlines for
  exactly this reason; a body whose trailing whitespace varies will churn.
- Symptom: two managed blocks in one file → the first block's markers stopped
  matching (marker text changed, or the end marker was hand-deleted, which
  makes `FindBlock` report "not found" by design). Look at the file, not at
  `Upsert`.
- Symptom: `assistant status --verify` fails on a fresh checkout → that is
  correct. `guidanceStage` reports OK only when every *selected* platform is
  current, so `--platform` narrows what has to be installed.
- Symptom: an operator's file lost content → reproduce through
  `block_test.go`'s round-trip test with their exact bytes. It asserts the
  surrounding bytes are byte-identical after insert, update, and remove.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported. If new logic needs
  something only the wrapper has, add a parameter.
- **Adding a `Stat` (or any other) method to `FileSystem` "for symmetry".**
  The interface carries exactly the four operations the engine calls. A
  previous version of this code exported a fifth that nothing ever called.
- **Wrapping the errors inside `OSFileSystem`.** Each method returns the os
  error unwrapped, guarded by a `//nolint:wrapcheck` that explains why:
  `readFileOrEmpty` already adds the path (wrapping would print it twice) and
  `os.IsNotExist` does not unwrap, so a wrap would turn "file does not exist"
  into a hard failure. `TestReadFileOrEmptyTreatsMissingAsAbsent` is the
  regression guard.
- **Making writes conditional on something other than a content diff.**
  `Install` skips the write when `Upsert` returned the existing content
  unchanged. That is what makes a reinstall leave mtime alone.

## What NOT to change without an ADR

- The marker strings (`BeginMarker`, `EndMarker`) — see the identity
  migration note above.
- Making `Install` write atomically (temp file plus rename). It is a real
  improvement, but it changes inode behavior on files editors and watchers
  hold open, so it needs a decision rather than an incidental refactor.
