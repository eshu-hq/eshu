# AGENTS.md — go/internal/cli/opdigest guidance for LLM assistants

## Read first

1. `go/internal/cli/opdigest/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/opdigest/doc.go` — the godoc contract
3. `go/cmd/eshu/operator_digest_cmd.go` — the cobra `RunE` wrapper that
   resolves process state (flags, output streams) and calls into this
   package. This is the file that shows how the two halves fit together.
4. `docs/public/reference/operator-digest.md` — the `operator_digest.v1` /
   `operator_digest_artifact.v1` contract this package implements

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, and nothing that
  writes to `os.Stdout` or `os.Stderr` — `RenderText` writes only to the
  `io.Writer` its caller hands it, and discards the error from every one of
  its `fmt.Fprint*` calls, so a writer that fails mid-report fails silently.
  Give `RenderText` an error return before pointing it at a writer that can
  fail, such as a network connection.
  `go/cmd/eshu` is `package main`, so nothing can import it — any symbol
  that reads a flag or maps to an exit code has to live in
  `operator_digest_cmd.go` instead. `OptionsFromFlags` takes plain
  `rawScope, rawProfile string, questionLimit int` values, not a
  `*cobra.Command`, precisely so it can validate and normalize those values
  without a cobra dependency.
- **The offline renderer never reads graph state.** `BuildDigest` always
  returns every section as `status: "unsupported"` with a limitation naming
  the missing bounded read surface, and `Truth.Reason` is always
  `"bounded_read_surface_not_connected"`. Do not special-case a section to
  look answered without an actual connected read surface behind it.
- **`BuildArtifact` validates before returning.** A caller (including
  `WriteArtifact`) can trust every `Artifact` it receives is contract-valid:
  schema markers set, required digest fields present, every section and
  question source reference resolves inside `Artifact.SourceRefs`, and
  redaction metadata is populated. If you add a new field to `Digest` or
  `Question` that changes the artifact contract, add the corresponding check
  to `validateArtifact` in the same change.
- **Determinism.** `BuildDigest` and `BuildArtifact` must return
  byte-identical output for identical `Options`. Do not introduce
  wall-clock time, random values, or map iteration into anything that
  reaches JSON output. The two `SourceRefs` fields get there differently, so
  do not assume one mechanism covers both. `BuildDigest` assigns
  `Digest.SourceRefs` a fixed slice literal it builds in place (`digest.go`),
  which is deterministic because nothing sorts or ranges a map to produce it.
  `Artifact.SourceRefs` is the only one `dedupeSourceRefs` (`artifact.go`)
  touches, and it preserves insertion order rather than sorting, so the
  determinism there comes from the order `BuildArtifact` appends in.

## Common changes and how to scope them

- **Add or change a digest section** → add an entry to `sectionTemplates`
  in `digest.go` and, once a real bounded read surface exists, replace the
  hardcoded `"unsupported"` status in `buildSections` for that section ID.
  Why: `sectionTemplates` order is the section ordering contract
  `docs/public/reference/operator-digest.md`'s Determinism section pins.
- **Add or change a suggested question** → add an entry to
  `questionTemplates` in `digest.go`. Why: each question must point at a
  `Target` that resolves inside `Artifact.SourceRefs` after
  `BuildArtifact` — `validateArtifact` fails loudly if a template's target
  is not also covered by `questionSourceRefs`' target-kind classification
  (`targetKind` in `artifact.go`).
- **Change what the artifact validates** → edit `validateArtifact` in
  `artifact.go`, not the wrapper. `BuildArtifact` is the single call site
  that must return only contract-valid artifacts.
- **Change the `--artifact-out` write target or mode** → edit
  `writeArtifactFile` in `artifact.go`. It is the single file-write path
  both the initial write and an existing-file overwrite go through, which is
  why the trailing `Chmod(0o600)` runs on every successful write, whether the
  file was created or overwritten. It does not run on a failed one:
  `writeArtifactFile` returns early on the `os.OpenFile` error, on the
  `file.Write` error, and on `io.ErrShortWrite`, so a write that fails partway
  leaves the file at whatever mode `os.OpenFile` gave it — `0600` for a file it
  created, the existing mode for one it overwrote.

## Failure modes and how to debug

- Symptom: `BuildArtifact` returns "operator digest question %q references
  unknown target %q" → cause: a new entry in `questionTemplates` (digest.go)
  points at a `Target` that `questionSourceRefs`/`targetKind` (artifact.go)
  does not turn into a `SourceRef`. Every question target must resolve.
- Symptom: `TestBuildDigestIsStableAndWellFormed` or
  `TestWriteArtifactWritesStableJSON` fails after adding a field → almost
  always a non-deterministic value (time, random, unordered map range)
  reached the JSON-encoded struct. Trace the new field back to its source.
- Symptom: `TestJSONKeyOrderMatchesContractDoc` fails naming a tag or key
  position → cause: a `Digest` or `Artifact` struct field moved, or its `json`
  tag changed. `encoding/json` emits fields in declaration order, so a move
  reorders the keys in every emitted digest and artifact; a tag option such as
  `,omitempty` lets a documented field vanish from one. The test compares the
  whole tag, options included, and separately the emitted key order. Either put
  the field back, or move the matching row in
  `docs/public/reference/operator-digest.md` and update the hand-transcribed
  lists in `doc_lockstep_test.go` in the same change. The lockstep only binds
  the structs to those lists — editing the doc's tables alone leaves it green,
  so change doc row, list entry, and struct field together. The two determinism
  tests will not catch any of this — they compare two marshals of the same
  input, which agree with each other whatever the order is.
- Symptom: `operator_digest_cmd.go`'s `runOperatorDigest` won't compile after
  a signature change here → this package's exported functions are the only
  contract the wrapper depends on; update the wrapper's call site to match,
  do not add a compatibility shim in either direction.

## Anti-patterns specific to this package

- **Printing from this package.** `RenderText` returns nothing but writes to
  the `io.Writer` it is given; every other function returns data and errors.
  `fmt.Print*` (unqualified, to stdout) belongs only in
  `operator_digest_cmd.go`.
- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has (a cobra flag, an output
  stream), add a parameter instead.
- **Wrapping `writeArtifactFile`'s `os` errors to quiet wrapcheck.** They are
  already `*fs.PathError` and name the path, and `WriteArtifact` adds the
  `write operator digest artifact: ` prefix, so a wrap makes
  `eshu report --artifact-out` print the path twice. wrapcheck asks for one
  because `go/.golangci.yml` turns the linter off for `cmd/` **by file path** —
  the `- path: 'cmd/'` entry under `linters.exclusions.rules` — and this code
  no longer lives under `cmd/`. The `//nolint:wrapcheck` directives are the
  intended answer. `TestWriteArtifactDoesNotDoubleWrapPathInError` compares the
  whole error string against `"write operator digest artifact: "` plus the
  `*fs.PathError`, so it goes red on any wrap, including one that does not
  repeat the path.
- **Adding this package to wrapcheck's `ignore-package-globs`.** It will not
  work and it will look like the linter is broken. That setting matches the
  package of the function that *returned* the error — `os` for everything in
  `writeArtifactFile` — not the package being analysed. The
  `github.com/eshu-hq/eshu/go/cmd/*` entry sitting in that list today exempts
  nothing; the `- path: 'cmd/'` exclusion rule is what actually silences
  `cmd/`, and the two carry identical comments, which is how they get mixed up.
- **Skipping `BuildArtifact`'s validation to hand-build an `Artifact`.**
  Every artifact this package produces must go through `BuildArtifact` so
  the contract checks in `validateArtifact` run.

## What NOT to change without an ADR

- The `operator_digest.v1` / `operator_digest_artifact.v1` schema markers
  (`Schema`, `artifactSchema`), the JSON field names, any `json` tag option on
  a top-level field, and the order those fields are declared in — they are
  stated by `docs/public/reference/operator-digest.md`, checked by
  `doc_lockstep_test.go`, and read by any downstream tooling that consumes
  committed `--artifact-out` files.
- Section and question ordering (`sectionTemplates`, `questionTemplates`
  iteration order) — the Determinism section of the operator digest contract
  requires stable section and question ordering across identical inputs.
