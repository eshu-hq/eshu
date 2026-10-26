# AGENTS.md — internal/collector/repo/runtime/declaration guidance

## Read First

1. `README.md` — purpose and the classification table.
2. `classify.go` — `Classify` is the authority on spec kinds and range bounds.
3. `parse.go` and `source.go` — the per-file line policy and the admitted names.
   `manifest.go`, `packagejson.go`, `gomod.go` and `jsonscan.go` — the
   `package.json` and `go.mod` readers and the credential guard (`safeSpec`).
   `dockerfile.go` and `workflow.go` — the `FROM` tag and `with:` version
   readers (#8818); both read through `safeSpec` and the manifest byte cap.
4. `extract.go` / `emitter.go` / `envelope.go` — extraction, caps and fact
   envelope construction.
5. `go/internal/facts/runtime_declaration.go` — the fact-kind constant, schema
   version and family declaration this package emits into.
6. `sdk/go/factschema/runtimedeclaration/v1/version.go` — the typed payload;
   `sdk/go/factschema/decode_runtime_declaration.go` — the encode/decode seam.
7. `go/internal/collector/repo/git/runtime/declaration/facts.go` — the Git
   collector glue that calls `Emit` during content streaming.

## Invariants

- Keep this package a pure normaliser: no file discovery, no scope or
  generation identifier minting, no reducer or query imports in production
  code.
- Never guess a version. An alias or unresolved placeholder stays an alias or
  unresolved with every version field nil. A range never fills
  `version_major`, `version_minor` or `version_patch`.
- Every nullable field is an omitempty pointer. Never zero-fill one.
- The admitted names in `source.go` mirror the parser registry's `raw_text`
  version-pin names (`go/internal/parser/registry_definitions.go`).
  `source_registry_test.go` fails when a `raw_text` exact name is in neither
  `pinFileSourceKinds` nor its `rawTextNonPinNames` list. Do not add
  `.npmrc`, `.yarnrc.yml`, `.pypirc`, `.netrc` or `mise.toml`: they can carry
  credentials, and the content store has no write-time redaction.
- A manifest value reaches a fact only through `safeSpec`. Never widen its
  character set, length bound or opaque-run bound to admit a case; a value it
  rejects is a counted drop, not a bug. `package.json` and `go.mod` are `store`
  policy files whose sections may be edited by anyone, so the guard is the
  only thing between a pasted credential and a fact.
- The JSON scan allocates for no skipped value and recurses to a fixed depth
  (`maxJSONDepth`). Keep it that way: it runs on every `package.json` in every
  repository, and a hostile file must cost time linear in its size only.
- A manifest is never retained: the collector keeps its `Extraction`, not the
  body. The readers hand out substrings of the body, and a substring pins the
  whole backing array, so `extractManifest` copies every kept string
  (`detach` in `extract.go`). `TestNoteRuntimeDeclarationCandidateRetainsNoManifestBody`
  in the git hook package fails when a kept string aliases the body. Route any
  new retained manifest field through `detach`. Do not add a field to
  `declaration` that grows the struct without re-running `TestEmitOverTheTokenCapUnderTheByteCapCountsWithoutStoring`
  (a pin file stores up to `MaxTokensPerFile` of them).
- Build every payload from `runtimedeclarationv1.Version` via
  `factschema.EncodeRuntimeDeclarationVersion`. Never hand-build a
  `map[string]any` (Contract System v1 §3.1; see `eshu-contract-rigor`).
- The stable key is `(repo_id, relative_path, line_number, runtime,
  order_index)`. Changing how it is derived is a major contract change.

## Common Changes

- A new pin file name: add it to the parser registry first, then to
  `pinFileSourceKinds`, add a `fileRuntime` or parser entry, a table-test row,
  a row in the stream test (`git/pin_file_emission_test.go`),
  and a row in `docs/public/languages/admitted-files.md` through
  `scripts/generate-parser-admission-doc.sh`.
- A new `source_kind` family (Dockerfile, CI) emits through the same contract
  and documents its `source_kind` values (#8818: `dockerfile_from`,
  `workflow_setup_version`). A new `package.json` section, `go.mod`
  directive, `FROM` shape or `with:` input adds a `section` constant, its
  `source_kind` and a table-test row, and its raw text goes through `safeSpec`.
  Never invent a priority field: reads order by the graph key, so a
  lower-priority source is documented, not ranked.
- A new value grammar: keep whitespace-split values going through
  `eachValue` (`values.go`) so a spaced operator or range is never cut into
  fragments, and extend the oracle in `emitter_oracle_test.go`.
- A payload shape change is a Contract System v1 change: update
  `sdk/go/factschema/runtimedeclaration/v1` and the decode seam first, then
  this package.
