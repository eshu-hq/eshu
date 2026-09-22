# AGENTS.md - internal/parser/golang/prescan guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: why pre-parse evidence exists and stays cheap
3. `prescan.go` - `PreScan`: the cheap import-map symbol collector
4. `package_evidence.go` - `PreScanFileEvidence`/`PrescanFileEvidence`: the
   single-parse evidence aggregate
5. `package_interface.go` - the individual evidence functions
   `PreScanFileEvidence`'s walk mirrors
6. `../compat_prescan.go` - the root package's forwarders that keep this
   move transparent to `internal/parser/*.go`
7. Callers: `internal/parser/go_language.go` and
   `internal/parser/go_package_interface_prescan.go` (outside this issue's
   edit scope — read them to understand the contract, never edit them here)

## Invariants this package enforces

- **Never import `internal/parser/golang`.** That back-edge is the cycle
  issue #6774 exists to remove. Anything this package needs from the old flat
  package lives in `internal/parser/golang/symbols` — import that instead.
- `PreScan` stays cheap: symbol names only, no dead-code root evidence, no
  other `Parse`-only payload work. It runs once per file before the real
  parse across a repo-scale corpus (#161); adding heavier work here
  reintroduces that regression.
- `PreScanFileEvidence`'s one-parse walk must stay behaviorally identical to
  calling the seven individual evidence functions separately — a change to
  one side without the matching change to the other is a silent divergence
  a caller cannot detect from the type system.
- Do not rewrite a function body while moving code into or out of this
  package. The accuracy golden gate and a parser equivalence dump compare
  emitted facts; a behavior change here is a defect, not a refactor.
- Keep `internal/parser/golang/compat_prescan.go`'s forwarders (`PreScan`,
  `PreScanFileEvidence`, `PrescanFileEvidence`,
  `ImportedDirectMethodCallRootsWithInterfaceReturns`) in sync with this
  package's exported signatures. Those four are the only symbols
  `internal/parser/*.go` calls; renaming or resignaturing one here without
  updating its forwarder breaks a build outside this package tree that this
  issue does not otherwise touch.

## Common changes and how to scope them

- Add a new per-file evidence type: add the single-evidence function to
  `package_interface.go`, an `extract*` mirror in `package_evidence.go`
  wired into `PreScanFileEvidence`, and a new `PrescanFileEvidence` field —
  keeping the two paths behaviorally identical (see
  `extractExportedInterfaceParamMethods`'s doc comment for the pattern).
- Change what `PreScan` collects: this is the highest-traffic function in the
  package (once per file, repo-scale); profile before adding a tree-sitter
  node kind to its switch.
- Add a new external caller in `internal/parser`: call this package directly
  (`prescan.X`), not through `golang.X` — the compat forwarders exist only to
  avoid touching `internal/parser/*.go` during the #6774 migration, not as a
  permanent indirection to add new callers through.

## Failure modes and how to debug

- `internal/parser` fails to build after a change here: check
  `../compat_prescan.go` first — a forwarder's signature likely drifted from
  this package's.
- `PreScanFileEvidence`'s aggregate result disagrees with calling the
  individual evidence functions: compare the `extract*` helper in
  `package_evidence.go` against its mirrored exported function in
  `package_interface.go` line by line; they must reproduce the same walk.
- `TestParseFullTreeWalkCount`-style walk-count regressions on a repo-scale
  corpus: check whether a change added a node kind or a nested walk to
  `PreScan` or a `PreScanFileEvidence` `extract*` helper — either runs once
  per file across the whole corpus.

## Do not change without review

- The compat-forwarder contract in `../compat_prescan.go` — its four
  forwarded symbols and their signatures are load-bearing for
  `internal/parser/*.go`, which this issue does not touch directly.
- `PreScan`'s cheap-collection-only contract (#161).
- `PreScanFileEvidence`'s single-parse-walk equivalence to the individual
  evidence functions.
