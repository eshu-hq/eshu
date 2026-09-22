# AGENTS.md - internal/parser/javascript/deadcode guidance

## Read first

- `doc.go` for the godoc contract and the two inverted seams.
- `README.md` for the ownership boundary and why this package exists.
- The parent `go/internal/parser/javascript/README.md` before assuming a
  behaviour belongs here. Route detection, framework semantics and the parse
  lifecycle are all the parent's.
- `docs/internal/parser-audit/javascript.md` for the dead-code evidence
  inventory this package is measured against.

## Invariants this package enforces

- **Never import the parent `javascript` package.** Go rejects the cycle, and
  the `go/types` census behind #6771 measured 24 symbol edges pointing that way
  before the split. Anything you need from the parent goes through
  `SiblingSource` or `FrameworkEvidence`, declared here and implemented there.
- **Do not move the parse seam here.** `ParserFactory`, `ParserReturner` and
  the sibling-file cache stay in the parent; #6062 requires that pending the
  `LanguageProvider` decision. `SiblingSource` exists precisely so this package
  does not need them.
- **Both seams tolerate nil.** A nil `SiblingSource` must answer ok false, not
  panic, and callers must treat that as "no sibling evidence available" rather
  than as an error. Tests construct evidence with nil seams; keep that working.
- **Root evidence is gathered once per file.** `RootEvidence` runs one pass and
  `RootKinds` answers per declaration from it. Do not add a per-declaration
  filesystem read or a per-declaration AST walk; that is the cost #4925 and
  #3586 removed.
- **Reuse `Evidence.Parents`.** The parent builds one `syntax.ParentLookup` per
  parse and threads it in. Building another per file reintroduces the cgo cost
  #3586 profiled at ~48% of parse CPU.
- **No stutter.** The path already says `deadcode`, so files are `roots.go` and
  `hapi.go`, not `dead_code_roots.go`; exported identifiers are `RootKinds`,
  not `DeadCodeRootKinds`.

## Common changes and how to scope them

- **Adding a root kind.** Decide first whether the evidence is local syntax
  (belongs here), a framework registration (parent, reached through
  `FrameworkEvidence`), or repository layout (`javascript/project`). Then add
  the kind string, a case in `RootKinds`, and a fixture test. Root-kind strings
  are graph truth: `docs/internal/parser-audit/javascript.md` lists them and
  the B-7 golden corpus gate compares them byte-for-byte.
- **Adding a method to a seam.** Both interfaces are consumer-declared, so
  adding a method means the parent must implement it. Update
  `javascript/framework_evidence.go` in the same commit; its
  `var _ deadcode.FrameworkEvidence = frameworkEvidence{}` assertion is there to
  fail the build if you forget.
- **Touching the public-surface cache.** `public_surface_cache.go` is keyed and
  invalidated through `javascript/project`'s stat-keyed `ScopeCache`. Its
  compute-once behaviour is pinned by
  `TestEngineParsePathComputesPackageSurfaceClosureOnceForSharedBarrel` and its
  race safety by `TestEngineParsePathConcurrentPackageSurfaceCacheIsRaceSafe`,
  both in the parent package.

## Failure modes and how to debug

- **An import cycle on build** means something reached back at the parent. Find
  it with `go list -f '{{join .Imports "\n"}}' ./internal/parser/javascript/deadcode`
  and route it through a seam instead.
- **A walk-count test comparing equal numbers** means the instrument is blind,
  not that the optimization vanished. Count through
  `shared.SetWalkNamedHookForTest`, which fires on every `WalkNamed` in the
  process; the parent's `walkNamed` alias only sees the parent's own calls and
  will report zero for this package.
- **A dead-code root that disappears after a refactor** is graph truth changing.
  The B-7 golden corpus gate will catch it in CI, but the fast local check is
  the fixture tests in this directory plus the parent's
  `dead_code_*_test.go` engine suites.
- **A bulk identifier rename that "just" changes a field name** has bitten this
  refactor three times: check the receiver's type, not only the member name.
  `parents` is a local in most functions and a field only on `Evidence`.

## Do not change without review

- The `SiblingSource` and `FrameworkEvidence` contracts, including their nil
  tolerance. They are what keeps this package compilable.
- The once-per-file gathering shape of `RootEvidence`.
- Any root-kind string. They are consumed as graph truth downstream and pinned
  by the golden corpus.
