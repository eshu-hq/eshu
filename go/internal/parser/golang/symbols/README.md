# Go Symbol Resolution

## Purpose

`symbols` resolves tree-sitter Go syntax nodes into the receiver types,
variable types, struct field types, interface targets, import aliases, and
lexical-scope bindings that the rest of `internal/parser/golang` builds fact
payloads, dead-code roots, and control-flow lowering on top of. It carries no
knowledge of any of those higher-level concerns; it only answers "what does
this tree-sitter node mean" questions.

## Ownership boundary

This package owns Go-specific tree-sitter node interpretation: receiver and
variable type inference (local and imported), struct field types, interface
target classification, import-alias bookkeeping, and the amortized
`ParentLookup` / `VariableTypeIndex` / `ImportedVariableTypeIndex` traversal
helpers. It does NOT own fact payload shape, dead-code root classification,
control-flow lowering, or package pre-scan entrypoints — those live in the
`golang` root package and its `cfg`, `deadcode`, and `prescan` siblings, which
depend on `symbols` rather than the reverse. `symbols` MUST NOT import any of
them; doing so would reopen the import cycle the split exists to break (see
`doc.go` and issue #6774).

## Exported surface

See `doc.go` for the full godoc contract. By area:

- **Traversal** — `ParentLookup`/`BuildParentLookup` (amortized ancestor
  walks), `VariableTypeIndex`/`BuildVariableTypeIndex` and
  `ImportedVariableTypeIndex`/`BuildImportedVariableTypeIndex` (amortized
  scoped variable-type lookups), `WalkScopeBindings`, `WalkDirectNamed`,
  `FirstNamedDescendant`.
- **Receiver/variable type inference** — `LocalReceiverBindings`,
  `InferredReceiverType`, `ConcreteInferredReceiverType`,
  `TypeNameFromNode`, `NormalizeTypeName`, `StructFieldConcreteTypes`,
  `ConcreteTypeFromExpression`, `ConcreteTypeFromTypeNode`,
  `CompositeLiteralTypeName`, `EnclosingMethodReceiver`,
  `MethodReceiverBinding`, `ReceiverContext`.
- **Interface targets** — `InterfaceTarget`/`InterfaceTargetFromTypeNode`,
  `FunctionParamInterfaceTargets`, `FunctionParamImportedInterfaceMethods`,
  `InterfaceMethodNames`, `KnownImportedInterfaceMethods`,
  `ReferencedLocalInterfaces`, `TypeParameterConstraintCandidates`.
- **Import aliases** — `ImportAlias`, `ImportAliasIndex`, `CollectImportAlias`,
  `AliasesForImportPath`, `ImportPathForAlias`.
- **Local-name / scope bindings** — `LocalNameBinding`,
  `LocalNameBindingsFromParameters`, `LocalNameBindingsFromNames`,
  `NameIsLocallyBound`, `FunctionLiteralIsCompositeElement`, `InsideFunction`,
  `NearestLexicalScope`.
- **Node text/shape helpers** — `IdentifierNames`, `IdentifierNodes`,
  `AssignableIdentifierNodes`, `ExpressionNodes`, `SelectorBaseAndField`,
  `UnwrapSingleExpression`, `Docstring`, `VariableNames`,
  `ShortVariableNames`, `IdentifierIsExported`, `CollectConstructorReturnType`.
- **Call-site resolution** — `CallArgumentNodes`, `CallIsFmtFormatting`,
  `FmtStringerFirstValueArgIndex`, `QualifiedCallFunctionName`,
  `ImportedDirectMethodCallKey`, `ImportedFmtStringerCallKeys`,
  `LocalInterfaceImportedMethodReturns`.
- **Slice utilities** — `AppendUniqueImportAlias`, `AppendUniqueMethods`.

## Dependencies

- `internal/parser/shared` — `NodeText`/`NodeLine`/`NodeEndLine`/`CloneNode`,
  `WalkNamed`, `Options`, and the `GoImportedInterfaceParamMethods` /
  `GoDirectMethodCallRoots` payload types this package's interface-target
  helpers produce and consume.
- `github.com/tree-sitter/go-tree-sitter` — the parsed syntax tree this
  package reads.
- No dependency on any sibling `internal/parser/golang/*` package; see
  Ownership boundary.

## Telemetry

None. Every function here is a pure, in-process resolution over an
already-parsed tree; the caller (`golang.Parse` and the package pre-scan
entrypoints) owns any parse-stage telemetry.

## Gotchas / invariants

- Layering is one-directional: `symbols` imports only `parser/shared` and
  tree-sitter. Never add an import from `symbols` back to `golang`,
  `golang/dataflow`, `golang/deadcode`, or `golang/prescan`.
- `ParentLookup`, `VariableTypeIndex`, and `ImportedVariableTypeIndex` are
  read-only after construction and scoped to one parsed tree; do not share
  one across files or mutate its backing maps from outside this package.
- `LocalReceiverBinding.Variable` is exported because a caller outside this
  package (the AWS SDK receiver-binding path) tests it directly to detect the
  zero-value binding `NewLocalReceiverBinding` returns when a node has no
  enclosing lexical scope; every other field on `LocalReceiverBinding` and
  `LocalNameBinding` stays unexported because nothing outside this package
  reads them.
- `InterfaceTarget`'s fields are exported because callers outside this
  package (the dead-code semantic-roots and package-prescan paths) construct
  and mutate `InterfaceTarget` values directly while merging imported-method
  evidence gathered from other files in the same Go package.
- Scoped queries (`VariableTypeIndex.ForNode`, `ImportedVariableTypeIndex.ForCall`)
  replay only the bindings whose `startByte` precedes the query node, in
  source order, so a later same-name rebinding wins — do not reorder or
  bypass this when extending either index.

## Related docs

- `docs/public/languages/go.md` — the Go collector's fact-emission contract
  this package's callers feed.
- `docs/internal/design/reducer-target-tree.md` — the dirgate-ledger restack
  rule referenced when another `internal/parser/golang` leaf is split next.
