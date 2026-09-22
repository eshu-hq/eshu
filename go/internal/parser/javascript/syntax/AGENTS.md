# AGENTS.md - internal/parser/javascript/syntax guidance

## Read first

- `doc.go` for the godoc contract and the exported surface.
- `README.md` for why this package exists and where its boundary runs.
- `docs/internal/naming.md` — this package was created by the #6771 rename
  and split pass; its rules bind anything added here.
- The parent `go/internal/parser/javascript/README.md` before assuming where
  a behaviour belongs. Most JavaScript questions belong there, not here.

## Invariants this package enforces

- **Leaf, permanently.** Never import the parent `javascript` package. Go
  would reject the cycle, and the symbol census behind #6771 showed this
  directory's call graph produces cycles readily — the tree originally
  proposed for the split had 22 of them.
- **Syntax, not semantics.** These functions read a node and report what the
  grammar says. Framework recognition (Express, Fastify, Hapi, NestJS,
  Next.js, CommonJS export shapes) belongs to the parent. The Express chain
  unwrap in `member_expression.go` is a documented exception the #6771 move
  carried over unchanged; do not treat it as licence to add a second one.
- **No per-declaration `Node.Parent()` in a hot path.** Build a `ParentLookup`
  once per parse and read it. Tree-sitter's `Node.Parent()` re-walks from the
  root and crosses cgo per call; the pattern this package replaced made
  `runtime.cgocall` about 48% of parse CPU on a full-corpus profile (#3586).
- **The residual regex stays characterized.** `staticComputedMemberNameRe` in
  `names.go` is a permanent within-string-content exception (#3590). It runs
  only against AST-isolated text. Any change to it must keep
  `names_test.go`'s accepted/rejected tables meaningful — that file exists to
  fail on behaviour drift, so a change that only edits the test to match new
  behaviour has defeated its purpose.
- **No stutter.** The package name already says `syntax` and the path already
  says `javascript`. Exported identifiers are `FunctionName`, not
  `SyntaxJavaScriptFunctionName`; files are `names.go`, not `syntax_names.go`.

## Common changes and how to scope them

- **Adding an extraction primitive.** Put it in the file that owns that node
  family (`names.go`, `metadata.go`, `type_references.go`,
  `member_expression.go`, …). Export it only if the parent package actually
  calls it; an unexported helper with one in-package caller stays unexported.
- **Adding a file.** This directory is well under the 40-file `dirgate` cap,
  but it was created to relieve that cap in the parent. If a change would add
  several files here, ask whether they are one nested responsibility
  (`syntax/<thing>/`) rather than more flat files.
- **Changing an exported signature.** The parent package is the only consumer
  today. Grep it, change both sides in one commit, and rerun the parent's
  tests — the parent's suite is where nearly all the behavioural coverage of
  this package lives.

## Failure modes and how to debug

- `undefined: syntax` in the parent means a caller uses `syntax.X` without the
  import line. A bulk edit that introduces `syntax.` call sites will not add
  the import for you; add it per file and let the compiler find the misses.
- A method renamed here and blind-renamed at call sites can land on a
  same-named method of an unrelated local type. That happened during #6771:
  `lookup.parent(...)` on a benchmark-local `benchParentLookup` was rewritten
  to `.Parent(...)`. The compiler catches it; read the receiver type before
  accepting a bulk method rename.
- A test that compiles but registers nothing is the silent failure mode of a
  move. Check with `go test -list '.*' ./internal/parser/javascript/...` and
  compare the name set, not just the count.

## Do not change without review

- `ParentLookup`'s one-pass construction and `Id()`-keyed map. It is a
  measured fix for a profiled hot path (#3586), not a style choice.
- `staticComputedMemberNameRe` and the `computedPropertyName` fallback order
  in `names.go`. The precedence between the static resolver and the regex is
  what `names_test.go`'s dotted-member-chain case pins.
- The leaf constraint in the first invariant above.
