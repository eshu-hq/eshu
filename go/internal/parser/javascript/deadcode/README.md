# JavaScript/TypeScript Dead-Code Roots

## Purpose

`deadcode` decides which JavaScript and TypeScript declarations are reachable
entry points rather than dead code. It produces the evidence the parser payload
carries as `dead_code_root_kinds` (per declaration) and
`dead_code_file_root_kinds` (per file).

A declaration with no in-repository caller is not automatically dead. It may be
a package entry point declared in `package.json`, a framework route handler, a
CommonJS or ES module export, a Hapi handler, plugin or proxy callback, a
Next.js pages/app route export, a NestJS controller method, or part of a
TypeScript package's declared public surface. Every one of those is a root, and
every one is proven from local syntax plus repository layout.

## Ownership boundary

This package owns the *root* question: is this declaration an entry point, and
on what evidence. It does not own route detection — deciding that
`app.get("/x", h)` registers a route is the parent `javascript` package's job,
and this package consumes the answer through `FrameworkEvidence`. It does not
own the parse lifecycle, the tree-sitter parser pool or the sibling-file cache;
it consumes those through `SiblingSource`.

It may import `javascript/syntax` and `javascript/project`. It must never
import the parent `javascript` package.

## The two inverted seams

Before issue #6771 these files could not be a package: they sat in a 17-file
mutually recursive component with the route detectors and the semantics files,
because route registration decides roots and root detection needs to know what
registers routes. A `go/types` census measured 24 symbol edges pointing outward.
Most were AST primitives that moved to `javascript/syntax` on their own merits.
The remaining ones became two interfaces, declared here and implemented by the
parent:

- `SiblingSource` — one method, `RootForFile`, for reading another file in the
  repository (a Hapi handler directory's index, a TypeScript barrel's re-export
  target, a package's declared surface).
- `FrameworkEvidence` — `RegisteredRootKinds`, `IsControllerMethod` and
  `ExpressSemantics`, the framework-route judgements the parent owns.

Declaring the interface in the consumer is what makes the compiled dependency
run one way (`javascript` imports `deadcode`) while the call graph still runs
both ways. Critically, no part of the parse seam moved: #6062 requires
`ParserFactory`/`ParserReturner` stay at the root pending the `LanguageProvider`
decision, and they did.

## Exported surface

- `Evidence`, `RootEvidence` — per-file root evidence, gathered once per parse
- `RootKinds` — the root kinds for one declaration
- `RegisteredDeadCodeRootKinds`, `MergeRegisteredRootKinds` — registration-derived
  root kinds and the merge over that map shape
- `ExpressServerSymbols` — Express server symbols from route semantics
- `HapiRouteHandlerReferenceCall` — Hapi route-config handler reference
- `CommonJSExportName`, `CollectCommonJSModuleExportAlias`,
  `RewriteCommonJSModuleExportAliasFullName`, `ExportAssignmentNameNode` —
  CommonJS export shapes the parent also reads
- `SiblingSource`, `FrameworkEvidence` — the two seams above
- `SetPackageSurfaceComputeHookForTest` — test-only cache-compute hook

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/parser/javascript/syntax` — AST primitives (names, parent
  lookup, member expressions, import/export rows)
- `go/internal/parser/javascript/project` — nearest `package.json`/`tsconfig.json`
  resolution and repo-relative path handling
- `go/internal/parser/shared` — node/payload helpers every parser shares
- `github.com/tree-sitter/go-tree-sitter`

## Telemetry

None. Every function here is a pure read over an already-parsed tree plus, via
`project`, cached filesystem lookups. Operator-facing signals for the parse
path live in the parent `javascript` package, which owns the parse lifecycle.

## Gotchas / invariants

- **A nil `SiblingSource` means absence of evidence, and you must go through
  `rootForFile` to get that.** This is subtler than it looks and it bit this
  refactor. The parameter used to be a concrete `*javaScriptSiblingParser`
  whose method guarded `p == nil`, so a nil parser answered ok false. A nil
  typed pointer and a nil *interface* are not alike: the first dispatches to
  the method, the second panics. Production always supplies a real parser, but
  tests pass nil and the walk-count tests only survived because they also pass
  `repoRoot ""` and return before the seam. `rootForFile` restores the
  pre-#6771 behaviour; `sibling_test.go` pins it with a control that fails
  loudly if an earlier guard starts short-circuiting the probe.
- **Never import the parent.** Go would reject the cycle, and the census behind
  #6771 shows how readily this directory's call graph produces one. New
  framework knowledge belongs in the parent, reached through
  `FrameworkEvidence`.
- **`Evidence.Parents` is exported for a reason.** The parent threads the same
  `syntax.ParentLookup` through its own helpers. Building a second lookup per
  file would reintroduce the per-declaration cgo cost that #3586 removed.
- **The walk-count contract is measured through `shared.SetWalkNamedHookForTest`,
  not by swapping the parent's `walkNamed` alias.** The alias only sees calls
  made from the parent package, so after this split it counted zero of this
  package's walks and the assertion silently compared equal numbers. The shared
  hook fires on every `WalkNamed` in the process.

## Related docs

- `go/internal/parser/javascript/README.md` — the parent package and how it
  composes this one
- `go/internal/parser/javascript/syntax/README.md` — the AST primitives used here
- `docs/internal/parser-audit/javascript.md` — the dead-code evidence inventory
