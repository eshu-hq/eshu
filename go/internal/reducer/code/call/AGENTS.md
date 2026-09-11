# call — agent instructions (issue #6061, #6609)

This directory is the code-call dispatcher. The handler and the symbol-runtime
builders moved to the sibling `materialization/` package (issue #6061); the
seven code_call_projection_* runner files still stay in the reducer root.
Read `doc.go` before moving anything else in or out. The entity-index
substrate and per-language resolvers live in `shared/` and the language
leaves (`golang/`, `java/`, `jvm/`, `kotlin/`, `groovy/`, `javascript/`,
`typescript/`, `python/`, `dart/`, `elixir/`, `haskell/`, `perl/`, `rust/`,
`swift/`); each leaf has its own scoped `AGENTS.md`.

## Invariants

- Never import the parent reducer package. If a change here needs root
  logic, the logic is either shared (hoist it to the shared tier) or the
  caller belongs in root (keep it there and call down into this package).
  The seven code_call_projection_* runner files are the worked example: they
  need the root lease and shared-projection machinery, so they stay in root
  and import this package directly.
- Behavior-preserving by default: the same rows, the same partition keys,
  the same evidence_source strings. `PartitionKeyVersion`,
  `RepoRefreshEvidenceSource`, `EvidenceSource`, and
  `PythonMetaclassEvidenceSource` are persisted in shared-intent payloads
  and partition keys, and the root runner reads the evidence sources back;
  changing one is a data migration, not a refactor.
- `shared.EntityIndex` is built once per pass and read-only afterward. Do not
  add mutation methods; the symbol-runtime builders in root share the same
  instance. Its language-specific fields stay unexported — read them through
  the accessor methods on `shared.EntityIndex`, never by adding new exported
  fields.
- No leaf (`golang/`, `java/`, ...) imports this package (`call`), and
  `shared/` imports no leaf. `languages.go` is the only file that imports
  every leaf; it wires each leaf's exported `Resolvers` list into
  `codeCallLanguageResolvers` under the language key(s) parser output uses.
  There is no `init()`-time registration left in this family — adding a
  language means adding its leaf plus one entry in `languages.go`'s map, not
  a blank import.
- Test doubles stay package-local. Other packages that need a helper from
  here keep their own twin (see `reducerTestRelativePath` in
  `code/call/materialization/routes_java_test.go`).
- Prove changes with `go test ./internal/reducer/... -count=1` (recursive),
  plus the resolution goldens here (`resolution_goldens_*_test.go`) and the
  B-7 golden corpus when row shapes could change.
