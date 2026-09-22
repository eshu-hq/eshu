# docs — agent instructions

This package holds the documentation fact family's declarations and payload
encoders. It moved out of the facts root in issue #6776.

## Invariants

- **Never import `go/internal/facts` from here.** The facts root imports
  this package to build `schemaVersionFamilies`; the reverse edge is an
  import cycle. Shared payload substrate belongs in
  `go/internal/facts/encode`.
- **The package is `docs`, not `documentation`.** `go/build` excludes every
  `.go` file in a package named `documentation`
  (`go/build/build.go`: `if pkg == "documentation"`), so such a package
  reports "build constraints exclude all Go files" and compiles nowhere.
  Do not rename it back.
- **A new or changed fact kind is a Contract System v1 change.** Add it to
  `specs/fact-kind-registry.v1.yaml`, add the family to the facts root's
  `schemaVersionFamilies` if it is versioned, and run
  `bash scripts/verify-fact-kind-registry.sh`,
  `bash scripts/verify-factschema-diff.sh`, and
  `bash scripts/verify-payload-usage-manifest.sh`. Load the
  `eshu-contract-rigor` skill first.
- **A stable-id derivation is a frozen contract.** `SectionStableID` and its
  siblings deliberately exclude mutable content — heading text, rendered
  body — so re-reading an edited page does not mint a second node. Changing
  the hashed field set orphans existing graph nodes and is a breaking change.
- **No package-name stutter.** Exported names here must not repeat `docs` or
  re-acquire the old `Documentation` prefix (`docs/internal/naming.md`
  rule 4). A file name must not repeat its directory
  (`scripts/verify-filename-stutter.sh`).
- **Removing a `facts.Documentation*` alias is a separate, caller-driven
  step.** `compat_docs.go` in the facts root carries the pre-move spellings
  for callers that still use them; delete an entry only once its last caller
  has moved.

## Proof expected for a change here

- `cd go && go test ./internal/facts/... -count=1`
- `cd go && go vet ./...` — the compat surface means a rename here breaks
  callers in other packages, and only a whole-module vet sees them
- For a move or rename, `go test -list '.*' ./internal/facts/...` diffed
  against the base, so a test left behind is visible
- The three contract gates above when a kind, payload field, or encoder
  changes
