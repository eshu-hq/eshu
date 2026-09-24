# JavaScript/TypeScript Syntax Primitives

## Purpose

`syntax` turns a tree-sitter node into the structural facts a caller needs.
Hand it a node and the source bytes it was parsed from, and it answers
questions the grammar can settle on its own: what is this declaration called,
what docstring precedes it, is it a getter or an async method, what type
parameters and type references does it carry, which interfaces does it
implement, what is the base and property of this member expression, how many
parameters does this signature declare, and what is this node's parent.

It was split out of the `javascript` package's root as part of the
directory-size reduction in issue #6771 (`golangci-lint`'s 40-file `dirgate`
cap).

## Ownership boundary

This package answers "what does this node say". It never answers "what does
this framework mean". Recognizing an Express route, a NestJS controller, a
Hapi handler or a CommonJS export shape is the parent `javascript` package's
job, and it builds those judgements on the primitives here.

Two warts are documented rather than hidden. `member_expression.go` carries
Express awareness (`IsExpressRouteChain` and `ExpressHandlerNames` unwrap the
`app.route(path).get(handler)` chain), and `nodes.go` carries
`HasExpressImport`. Both are framework knowledge sitting in a syntax package.
They moved as-is because splitting them would have meant inventing a new seam
mid-move; see the invariants below before adding a third.

`syntax` is a leaf. It must not import the parent `javascript` package, and it
does not import its sibling `project` either — the two are independent.

## Exported surface

- `ParentLookup`, `BuildParentLookup` — one-pass parent-pointer index for a
  parsed tree, and `(*ParentLookup).Parent` to read it
- `FunctionName` — declared name for a function, method, class member or JSX
  identifier, resolving statically known computed properties
- `TrimQuotes` — strip matching quotes from a literal's text
- `Docstring`, `FunctionKind` — JSDoc text and getter/setter/async/generator
  classification
- `TypeParameterNames`, `AppendTypeReferenceCalls`, `TypeReferenceLeafName` —
  TypeScript generic parameters and type-reference metadata
- `ImplementedInterfaces` — TypeScript `implements` clause names
- `MemberBaseAndProperty`, `IsExpressRouteChain`, `ExpressHandlerNames`,
  `IdentifierName` — member-expression decomposition
- `ParameterCount` — declared parameter count for a signature
- `ImportEntries`, `NamespaceImportAlias`, `RequireImportEntries`,
  `RequireModuleSource` — import and `require` entry rows
- `ReExportEntries`, `ReExportSource`, `ReExportSpecifiers`, `IsStarReExport`,
  `ReExportSpecifier` — re-export rows and their specifier pairs
  (`ReExportSource` reads only the grammar's string-literal `source` field, and
  the specifier text fallback never reads a declaration export's body; there is
  no text scan for `from`, see #7056)
- `CollectNewExpressionVariableType`, `FunctionReturnTypes`,
  `CallInferredObjectType`, `NewExpressionConstructorName`,
  `TypedBindingName`, `DeclaredTypeName` — receiver typing from local syntax
- `IsFunctionValue`, `InsideFunction`, `Decorators`, `CallName`,
  `CallFullName`, `JSXComponentName`, `NodeContainsKind`, `NodeSameRange`,
  `StringLiteralValue`, `ObjectPairKey`, `HasExpressImport`, `TypeParameters` —
  node predicates and readers hoisted out of the parent for #6771

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/parser/shared` — `NodeText`, `WalkNamed`, `NodeLine`,
  `AppendBucket` and the other node/payload helpers every parser shares
- `github.com/tree-sitter/go-tree-sitter` — the node type these helpers read

Nothing else. In particular, no dependency on the `javascript` parent or on
the `project` sibling, which is what keeps the split cycle-free.

## Telemetry

None. Every function here is a pure read over an already-parsed tree: no I/O,
no goroutines, no metrics, spans or logs. Operator-facing signals for the
JavaScript parse path live in the parent `javascript` package, which owns the
parse lifecycle (`js_parse_bounded`) and the payload it emits.

## Performance Evidence

This package was created by moving eight files out of `internal/parser/javascript`
(issue #6771). No algorithm, data structure, cache, allocation site or
concurrency property changed in the move, so there is no before/after
measurement to report -- there is nothing whose cost could have moved. What the
move could plausibly have broken is the one measured performance contract these
files carry, so that contract is re-proven at the new location rather than
assumed.

No-Regression Evidence: the #3586 contract is that ancestor walks consult a Go
map instead of re-entering cgo via `ts_node_parent`, which took
`runtime.cgocall` from roughly 48% of parse CPU down off the profile. Its
mechanism gate `TestJavaScriptParentLookupEliminatesCgoCrossings` asserts zero
cgo `Parent()` crossings and identical is-exported results for every
declaration node. It lives in the parent package, now calls
`syntax.BuildParentLookup` across the package boundary, and passes at this
head:

```
go test ./internal/parser/javascript/ \
  -run 'TestJavaScriptParentLookupEliminatesCgoCrossings|TestWalkCount' \
  -count=1 -v                                              exit 0
  --- PASS: TestJavaScriptParentLookupEliminatesCgoCrossings (0.02s)
  --- PASS: TestWalkCount_FrameworkRouteEntries             (0.00s)
  --- PASS: TestWalkCount_DuringParse_FrameworkFile         (0.00s)
  --- PASS: TestWalkCount_AssertReduction                   (0.00s)
```

Baseline and after are the same corpus and the same gates, at `origin/main`
965631995 and at this branch head; the gates are exact assertions (zero
crossings, a fixed walk count), not timings, so they are not subject to the
contention that makes wall-clock comparisons on a shared machine meaningless.

Two second-order effects were considered and neither can regress:
`ParentLookup.Parent` is exported and tiny, so it stays inlinable across the
package boundary through export data; and the moved files now call
`shared.NodeText` directly instead of through the parent package's
`nodeText = shared.NodeText` function-value alias, which replaces an indirect
call with a direct one.

No-Observability-Change: this package emits no metric, span, structured log,
status field or pprof label, and the move added and removed none. Every
function here is a pure read over an already-parsed tree.

## Gotchas / invariants

- `ParentLookup` exists because tree-sitter's `Node.Parent()` re-walks from
  the tree root on every call and crosses cgo each time. Walking parents per
  declaration made `runtime.cgocall` about 48% of parse CPU on a full-corpus
  profile (#3586). Build the index once per parse and read it; never
  reintroduce a per-declaration `Node.Parent()` loop in a hot path. The
  mechanism gate is `TestJavaScriptParentLookupEliminatesCgoCrossings` and the
  before/after benchmark is `js_parent_lookup_bench_test.go`, both in the
  parent package.
- `staticComputedMemberNameRe` in `names.go` is one of three permanent
  within-string-content regex exceptions in the JavaScript family (#3590). It
  runs only against text the AST has already isolated. Its characterization
  tests are in `names_test.go` and are meant to fail on any behaviour change;
  the other two exceptions stayed with `semantics_ast.go` in the parent.
- Before adding framework knowledge here, put it in the parent package
  instead. The Express chain unwrap in `member_expression.go` is a documented
  exception carried over by the #6771 move, not a precedent.
- Keep this package a leaf. An import of the parent `javascript` package would
  be an import cycle, and the symbol census behind #6771 showed how quickly
  this directory's call graph produces those.

## Related docs

- `go/internal/parser/javascript/README.md` — the parent package and how it
  composes these primitives
- `docs/internal/design/regexp-audit.md` — entry 11 covers
  `syntax/names.go`'s residual regex
- `docs/internal/naming.md` — the naming rules this split was made under
