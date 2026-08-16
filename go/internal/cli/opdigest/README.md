# Operator Digest

## Purpose

`opdigest` owns the business logic behind `eshu report`: validating a
share-safe scope and profile, rendering the deterministic
`operator_digest.v1` model, and building the shareable
`operator_digest_artifact.v1` handoff wrapper around it. The current
implementation is an offline presentation path -- it validates the contract,
emits deterministic unsupported sections, and points operators to bounded
follow-up routes, without reading graph state, writing graph state, claiming
reducer work, or calling providers.

## Ownership boundary

This package owns digest *logic* -- what scope inputs are valid, what the
digest model looks like, and how to wrap and persist it as an artifact. It
does not own process wiring: reading cobra flags, or printing to
stdout/stderr and mapping errors to exit codes. Those stay in
`go/cmd/eshu/operator_digest_cmd.go`, the cobra `RunE` wrapper, because
`go/cmd/eshu` is `package main` and nothing can import it. The wrapper
resolves process state and passes it into this package as plain values;
this package never touches `os.Stdout` or `os.Stderr`. `RenderText` formats
the report into an `io.Writer` the caller supplies; every other function
returns data and errors.

## Exported surface

- `Options`, `OptionsFromFlags` -- the validated, share-safe input to
  `BuildDigest`. `OptionsFromFlags` takes the already-extracted
  `--scope`/`--profile`/`--question-limit` values, not a `*cobra.Command`, so
  scope parsing and share-safe validation stay unit-testable without cobra.
- `Digest`, `Scope`, `Truth`, `Section`, `Entry`, `Question`, `Limitation`,
  `SourceRef` -- the `operator_digest.v1` model types
- `BuildDigest` -- renders the deterministic digest for `Options`
- `RenderText` -- writes the plain-text report `eshu report` prints by
  default (no `--json`)
- `Schema`, `DefaultProfile`, `DefaultQuestionMax` -- the contract constants
  the wrapper's flag defaults and the digest's `schema` field share
- `Artifact`, `ArtifactMetadata`, `ArtifactRedaction`, `ArtifactValidation`,
  `ArtifactCheck` -- the `operator_digest_artifact.v1` wrapper types
- `BuildArtifact` -- wraps a `Digest`, computes its content-derived
  `Artifact.ID`, and validates it against the artifact contract
- `WriteArtifact` -- builds and persists the artifact as mode-0600 JSON at a
  caller-supplied path

The artifact contract constants -- `artifactSchema`, `artifactFormat`,
`artifactWriterCLI`, `redactionProfile` -- are deliberately **unexported**.
They were unexported in `go/cmd/eshu` before this package existed and no
caller outside the package reads them; the extraction moves code without
widening the contract. Export one only when a real caller needs it.

See `doc.go` for the full godoc contract.

## Dependencies

- Standard library only: `crypto/sha256`, `encoding/hex`, `encoding/json`,
  `fmt`, `io`, `os`, `strings`, and `unicode` in the non-test files. The tests
  import all of that except `crypto/sha256`, `encoding/hex`, `fmt`, `io`, and
  `unicode`, and add `bytes`, `errors`, `io/fs`, `path/filepath`, `reflect`,
  and `testing`. Re-derive both lists rather than editing them by hand:
  `go list -f '{{.Imports}}' ./internal/cli/opdigest` and the same with
  `{{.TestImports}}` and `{{.XTestImports}}`. No third-party or intra-repo
  imports, so `go list -deps` on this package resolves to nothing outside the
  standard library and the package itself.
- Imported by three files: the `report` command wrapper
  (`go/cmd/eshu/operator_digest_cmd.go`); the `competitive-parity validate`
  exerciser (`go/internal/cli/compparity/exercises.go`'s
  `exerciseOperatorDigestArtifact`), which reuses `OptionsFromFlags`,
  `DefaultProfile`, `BuildDigest`, and `BuildArtifact` to prove the digest and
  artifact paths stay wired at release-verification time; and the wrapper's
  own test (`go/cmd/eshu/operator_digest_cmd_test.go`), which decodes the
  command's stdout into `Digest` and `Artifact` and checks the result against
  `Schema`. That test is the reason `Schema` stays exported.

## Telemetry

None. `eshu report` runs inline with the CLI invocation and renders a
terminal report or a local file; there is no background pipeline stage to
instrument. See the Verification Expectations section of
[Operator Digest Contract](../../../../docs/public/reference/operator-digest.md#verification-expectations)
for the contract's current no-observability-change posture.

## Gotchas / invariants

- Every section the offline renderer produces is `status: "unsupported"`
  with a limitation naming the missing bounded read surface --
  `BuildDigest` does not have a live read surface to call yet. A future
  implementation that connects one must update `buildSections` per section,
  not add a parallel code path.
- The top-level field order and `json` tags of `Digest` and `Artifact` are
  stated by the Output Shape and Shareable Artifact tables in
  `docs/public/reference/operator-digest.md`.
  `TestJSONKeyOrderMatchesContractDoc` holds the structs to those tables from
  two sides: the whole `json` tag of every field in declaration order, read by
  reflection, and the key order the encoder actually emits. Both are needed.
  The tags on their own say nothing about what comes out of `encoding/json`;
  the emitted keys on their own miss a tag option, because `,omitempty` leaves
  the key name alone and the test's fixture populates every field, so not one
  byte moves. Swapping two fields, renaming a tag, adding a field, and adding
  `,omitempty` each fail it.
- That lockstep runs one way only. Both tables are transcribed into arrays in
  `doc_lockstep_test.go` by hand, so the test catches the structs drifting from
  the arrays, not the doc drifting from them. Reorder the rows in the contract
  doc's tables without touching the structs and the test stays green while the
  doc is wrong. Move the doc row, the array entry, and the struct field in one
  change.
- Neither `TestBuildDigestIsStableAndWellFormed` nor
  `TestWriteArtifactWritesStableJSON` covers any of that. They marshal one input
  twice and compare the two results, so they catch non-determinism -- wall-clock
  time, map iteration -- and say nothing about the order the keys come out in.
- `BuildArtifact` validates before returning, so a caller can trust every
  `Artifact` it gets back is contract-valid, and `WriteArtifact` will not write
  an artifact that failed validation -- it returns before opening the file.
  That is a check on content, not a guarantee about the file: `writeArtifactFile`
  opens with `O_TRUNC` and writes in place, with no temp-file-and-rename, so a
  write that fails partway leaves a truncated file at the target path. This
  matches the behaviour before the extraction.
- On a successful write, `WriteArtifact` always ends the file at mode `0600`,
  including when it overwrites an existing wider-permission file
  (`writeArtifactFile`'s trailing `Chmod`). A failed write never reaches that
  `Chmod` -- `writeArtifactFile` returns early on the `os.OpenFile` error, the
  `file.Write` error, and `io.ErrShortWrite` -- so a truncated overwrite keeps
  the mode the existing file already had.
- `RenderText` returns nothing and discards the error from all eleven of its
  `fmt.Fprint*` calls, so a failing write to the caller's `io.Writer` is
  silent. That is fine for the one caller there is -- `eshu report` writing to
  stdout -- and would need a signature change before this is safe over a
  writer that can fail partway, such as a network connection.
- `writeArtifactFile` returns its `os`/`*os.File` errors **unwrapped**, behind
  `//nolint:wrapcheck`. Those are already `*fs.PathError` and render as
  `open <path>: <cause>`, and `WriteArtifact` adds the
  `write operator digest artifact: ` prefix -- wrapping here prints the path
  twice. The linter asks for a wrap because `go/.golangci.yml` switches
  wrapcheck off **by file path**: the `- path: 'cmd/'` entry under
  `linters.exclusions.rules`. That rule matches the file being analysed, so
  this code lost its exemption the moment it moved out of the `cmd/` tree.
  Satisfying the linter here changes what an operator reads; do not.
  `TestWriteArtifactDoesNotDoubleWrapPathInError` compares the full error
  string against `"write operator digest artifact: " + <the *fs.PathError>`, so
  any wrap fails it, including one that does not repeat the path.
- Do not reach for wrapcheck's `ignore-package-globs` when a file in this epic
  loses the exemption. That setting matches the package of the function that
  **returned** the error -- `os` here, not the package under analysis -- so
  adding `github.com/eshu-hq/eshu/go/internal/cli/*` to it changes nothing for
  a standard-library error and silently looks like a broken linter. The
  `github.com/eshu-hq/eshu/go/cmd/*` entry already in that list is a no-op for
  the same reason; the comment above it reads word for word like the comment
  above the `- path: 'cmd/'` rule, which is how the two get confused.

## Performance and observability of the extraction

This package arrived by moving files out of `go/cmd/eshu`, not by changing what
they do, so there is no before/after measurement to report and inventing one
would be dishonest. What follows is what was actually established.

No-Regression Evidence: the extraction is behaviour-preserving. Two independent
proofs say so, and they cover different things.

The first is a CLI parity run. One `eshu` binary was built from `840763ed2` and
one from this branch's first extraction commit, and their stdout, stderr, and
exit code were diffed across eleven invocations: `eshu report --help`, the root
command list, the default text render, `--json`, a non-default
`--profile`/`--question-limit`, a successful `--artifact-out` (including the
written file's bytes and its 0600 mode), the two flag-validation errors, and
three artifact write failures -- target is a directory, parent directory
missing, and parent directory not writable. All eleven are byte-identical.

Those captures were taken before the documentation, comment, and test edits
that followed the extraction commit, and before this branch was rebased forward
more than once as main moved. They are evidence about the two trees they ran
on, and they say nothing about the binary that ships: `#6104`
and `#6105` have since rewritten other files in `go/cmd/eshu`, so the shipping
`eshu` binary is not byte-identical to either binary that was captured.

The second proof does cover the shipping tree -- but only the files the logic
moved between: the two pre-move files in `go/cmd/eshu` and the three that
replaced them. The other two non-test `.go` files this commit writes under
`go/cmd/eshu` and this package are handled after the bullets. It also survives
the next rebase, because it compares source rather than compiler output. Parse
each file with `go/parser` without `ParseComments`, so no comment reaches the
AST and `go/printer` cannot emit one. Rewrite every `*ast.Ident` through the
extraction's rename map -- `operatorDigestArtifactSchema` to `artifactSchema`,
and so on. Drop the `import` declarations, sort the remaining top-level
declarations by name, print each one through `go/printer` with gofmt settings,
then re-tokenize that text with `go/scanner` and emit one token per line.
Tokenizing last is what makes the comparison whitespace-insensitive without
making it wrong: it erases gofmt's alignment padding, which shifts whenever an
identifier changes length, while leaving string literals whole, so a literal
carrying runs of spaces -- `RenderText`'s `"  scope    : %s (%s)\n"` -- is never
collapsed.

Run that against `ef38ae92e`, the commit the comparison was executed against,
not against `origin/main`, which has moved on since. `ef38ae92e` carries both
pre-move files, `operator_digest_artifact.go` and `operator_digest_cmd.go`,
exactly as `840763ed2` left them, and so does every commit from `840763ed2`
through the base this branch sits on: all four `operator_digest*` blobs --
those two sources and their `_test.go` siblings -- are byte-identical the whole
way. That is why rebasing this branch forward does not weaken the comparison,
and it is stated as a range on purpose, so the next rebase does not falsify the
sentence. Re-check it with
`git log 840763ed2..<the branch's base> -- 'go/cmd/eshu/operator_digest*'`,
which lists no commit.

- `artifact.go` against `go/cmd/eshu/operator_digest_artifact.go`: identical
  token streams, `diff` exit 0.
- `digest.go` plus this branch's `go/cmd/eshu/operator_digest_cmd.go` against
  `go/cmd/eshu/operator_digest_cmd.go`: twelve extra tokens on the new side and
  nothing else. All twelve are the `opdigest` package qualifier and its dot, on
  the six places the wrapper now reaches across the package boundary --
  `DefaultProfile`, `DefaultQuestionMax`, `OptionsFromFlags`, `BuildDigest`,
  `WriteArtifact`, and `RenderText`. Strip the qualifier and `diff` exits 0.

The walker used here counted 1667 tokens for the first pair and 2547/2559 for
the second, but read those as tool-specific and do not treat them as the
result. The steps above do not pin every counting choice -- whether the
scanner's auto-inserted semicolons get a line of their own is the one that
moves the total most -- so a second implementation reproduces the structure,
the two exit-0 diffs and the six-qualifier delta, while landing on different
integers. The diffs are the claim.

Several things sit outside that comparison, all deliberately. Import
declarations are excluded, because the extraction splits one import block
across two files;
the union of the new imports, minus `opdigest` itself, is the old set exactly.
Comments are excluded, which is the whole point of dropping them -- but it also
means the three `//nolint:wrapcheck` directives are not covered. Those change
what golangci-lint reports and nothing the compiler emits. Then the two files
neither bullet names. `competitive_parity_cmd.go`: its five changed lines
qualify four existing references in `exerciseOperatorDigestArtifact` with
`opdigest.`, which the compiler resolves and
`go test ./cmd/eshu/... -count=1` runs. And this package's `doc.go`, which the
commit adds: it holds nothing but the package comment, and a method that drops
comments has nothing in it to compare. The commit writes one more non-test
`.go` file, `tools/golangci-lint-dirgate/grandfather.go`, but that one is
generated by
`scripts/generate-dirgate-grandfather-go.sh` from the ledger TSV and carries no
extraction logic.

Dropping comments is also a precondition on reusing this method, not just a
property of it. `//go:build`, `//go:embed`, and `//go:generate` are comments to
`go/parser`, so they go the same way, and one planted between the two trees
passes the comparison silently. Check both trees for `//go:` before trusting a
green run. Nothing this comparison covers carries one, but `go/cmd/eshu` does,
in three files the comparison never reaches:
`local_graph_embedded_stub.go` (`//go:build !nolocalllm`),
`local_graph_embedded_nornicdb.go` (`//go:build nolocalllm`), and
`local_graph_embedded_nornicdb_test.go`. The first two are a matched build-tag
pair, which is the case that passes this comparison while changing which file
the compiler sees. The remaining extractions in epic #6053 come out of that
same directory, so sweep it -- `rg -n '^//go:' go/cmd/eshu/` -- before reusing
the method on them.

`go build ./...`, `go vet ./cmd/eshu/... ./internal/cli/...`, and
`go test ./cmd/eshu/... ./internal/cli/... -count=1` all pass, and no
`testdata/` path appears in the diff, so the golden-corpus gate (B-7, which
indexes 20 real repositories and compares the answers against a saved snapshot)
and the end-to-end snapshot (B-12) are untouched.

The three failure invocations are in that list for a reason. An earlier revision
of this extraction wrapped the file-I/O errors in `writeArtifactFile`, which
doubled the path in operator-facing stderr, and a parity run over happy paths
alone reported clean. `TestWriteArtifactDoesNotDoubleWrapPathInError` now pins
that text, one subtest per mode.

The first version of that assertion was itself too weak, which is worth knowing
because the next extraction will be tempted to write the same one. It counted
how many times the path appeared and checked the message still started with
`write operator digest artifact: open `. A wrap of `open artifact file: %w`
satisfies both -- it repeats no path, and its own text starts with `open ` --
while changing the operator's stderr. The assertion now compares the entire
string against `"write operator digest artifact: "` plus the `*fs.PathError`
pulled back out with `errors.As`, so there is no room between the prefix and
the cause for anything to hide. The not-writable subtest skips when the tests
run as uid 0: root ignores a directory's write bit, so the write succeeds and
there is no error text to pin. In a root-running CI container that leaves two
of the three modes guarded.

`artifact.go` trips the hot-path filter on content, not on cost: it imports
`crypto/sha256` for source-ref digests and iterates the digest's sections and
refs when it validates them. Both loops are bounded by the digest the same
process just built in memory -- sections are a fixed small set and
`QuestionLimit` caps at 25 -- and the move changed neither loop, neither bound,
nor the hashing. The edits to this file were identifier renames for the package
boundary, the doc comments the exported names now need, and the three
`//nolint:wrapcheck` directives on `writeArtifactFile` -- which is exactly what
the zero-token `go/ast` comparison above reports.

No-Observability-Change: this package emits no metrics, spans, or logs, and
the move neither added nor removed an instrument. Operator-visible output is
unchanged -- the parity captures show that for the trees they ran on, and the
`go/ast` comparison carries it forward to the tree that ships. The
redaction profile and the share-safe token rules are the same code in a new
package, so an artifact built before and after this change is identical.

## Related docs

- `docs/public/reference/operator-digest.md` -- the operator digest and
  artifact contract this package implements
- `docs/public/reference/cli-reference.md` -- the `eshu report` CLI summary
