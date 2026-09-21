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

One wart is documented rather than hidden: `member_expression.go` carries
Express awareness (`IsExpressRouteChain` and `ExpressHandlerNames` unwrap the
`app.route(path).get(handler)` chain). That is framework knowledge sitting in
a syntax package. It moved as-is because splitting it would have meant
inventing a new seam during a rename pass; see the invariants below before
adding a second one.

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
