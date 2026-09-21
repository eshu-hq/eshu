# AGENTS.md - internal/parser/golang/symbols guidance

## Read first

1. `README.md` — ownership boundary, exported surface by area, dependencies
2. `doc.go` — godoc contract: the layering rule and the amortized-traversal
   contract
3. `parent_lookup.go` — `ParentLookup`, the O(1)-per-step ancestor index every
   other resolution helper in this package threads through
4. `variable_index.go` / `imported_variable_index.go` — the scoped
   variable-type indices; read `ForNode`/`ForCall` before changing binding
   replay order
5. The sibling this package was carved out of: `../language.go`,
   `../file_level_indexes.go`, and the `../dead_code_*.go` files that still
   call into this package for issue #6774 context

## Invariants this package enforces

- **Layering is one-directional.** This package imports only
  `internal/parser/shared` and `github.com/tree-sitter/go-tree-sitter`. It
  MUST NOT import `internal/parser/golang` or any of `golang/dataflow`,
  `golang/deadcode`, `golang/prescan` — those packages import `symbols`, and
  a back-import reopens the cycle the #6774 split exists to break.
- **Export only what crosses the package boundary.** A symbol used only
  inside `symbols/*.go` stays unexported (lower-case, `go`-prefix retained
  where that was the existing name). A symbol another `internal/parser/golang`
  leaf calls must be exported following `docs/internal/naming.md`: no
  package-name stutter, and drop the now-redundant `go` prefix
  (`goReceiverContext` → `ReceiverContext`). Every exported identifier needs a
  real doc comment, not a restatement of its name.
- **`ParentLookup`, `VariableTypeIndex`, `ImportedVariableTypeIndex` are
  read-only after construction** and scoped to one parsed tree. Do not share
  one across files, and do not mutate `packageVars`/`scopeBindings` from
  outside the type's own methods.
- **Scoped replay order matters.** `VariableTypeIndex.ForNode` and
  `ImportedVariableTypeIndex.ForCall` merge only the bindings whose
  `startByte` precedes the query node, in source order, so a later
  same-key binding wins. Changing that order changes correlation truth for
  every caller (dead-code root resolution, call-metadata annotation).
- **`LocalReceiverBinding.Variable` and `InterfaceTarget`'s fields are
  exported on purpose** — callers outside this package construct, mutate, or
  zero-check them directly (the AWS SDK receiver-binding path; the
  imported-interface-param-method merge in `dead_code_semantic_helpers.go`).
  Do not add more exported fields to either type without checking whether the
  new field actually needs cross-package access; keep the rest unexported.

## Common changes and how to scope them

- **Add a new receiver/variable-type resolution rule:** extend the relevant
  file (`receiver.go`, `receiver_concrete.go`, `variable_types.go`,
  `variable_scope.go`, `map_receiver.go`) and add a focused test in this
  package (`package symbols`) exercising the new tree-sitter shape directly,
  rather than only through `golang.Parse`'s equivalence-dump harness.
- **Add a new interface-target classification:** extend `interfaces.go`
  (`InterfaceTargetFromTypeNode`, `KnownImportedInterfaceMethods`) and check
  whether the new target kind needs a new exported `InterfaceTarget` field —
  prefer a method over a field when the value can be derived from existing
  fields.
- **Widen what `ParentLookup`/`VariableTypeIndex` amortizes:** these exist
  because per-node re-walks saturated CPU on repo-scale inputs (#161). Any
  change here needs before/after evidence on a large fixture, not just a
  fixture-scale timing.

## Failure modes and how to debug

- **Import cycle on build:** almost always means a new call from `symbols`
  into `golang`, `golang/dataflow`, `golang/deadcode`, or `golang/prescan` slipped
  in. Move the caller's logic to the calling package instead of pulling the
  callee down into `symbols`.
- **A moved/renamed identifier silently stops being called from root:** the
  root `golang` package still uses the mechanical forwarders in
  `../helpers.go` (`nodeText`, `nodeLine`, `nodeEndLine`, `walkNamed`) and the
  type aliases in `../types.go` — a helper the root package calls must resolve
  through those forwarders or a direct `symbols.X` call, never a bare
  unqualified name left over from before the #6774 split.
- **A scoped lookup returns a stale or wrong-scope type:** check whether the
  binding that should have shadowed the query node has a `startByte` at or
  after the query node's `StartByte()` — `ForNode`/`ForCall` intentionally
  stop replaying bindings once they pass the query node.

## Do not change without review

- The layering rule (this package importing only `shared` + tree-sitter).
- The `startByte`-ordered replay semantics of `VariableTypeIndex.ForNode` and
  `ImportedVariableTypeIndex.ForCall`.
- The exported-field decisions on `LocalReceiverBinding` and `InterfaceTarget`
  documented above; widening them without a real cross-package need adds
  surface the merge/zero-check callers do not require.
