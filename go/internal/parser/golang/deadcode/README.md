# Go Dead-Code Root Evidence

## Purpose

`deadcode` derives the "dead_code_root_kinds" evidence the golang parser
attaches to every function, method, interface, and struct declaration:
concrete signals that a declaration is a reachability root even though
nothing in the file calls it directly (an HTTP handler registered by name, a
cobra command's `Run`/`RunE`, an interface implementation satisfied only
through a variable of the interface type, and similar framework or
type-system wiring). Extracted from `internal/parser/golang` (issue #6774) so
the root package stops importing everything it needs from one flat,
cycle-prone file set.

## Ownership boundary

This package owns dead-code root evidence for Go: explicit framework
registrations (`registrations.go`), signature-shape and direct-method-call
roots (`roots.go`), and composing both with the semantic evidence from
`internal/parser/golang/deadcode/semantic`. It does NOT own reachability
analysis or graph traversal itself — that happens downstream of the parser,
over the fact this package emits. It does not own symbol/parent-lookup/
receiver-context helpers shared across the `golang` parser (those live in
`internal/parser/golang/symbols`). It never imports `internal/parser/golang`
— that back-edge is exactly the cycle #6774 removes.

## Exported surface

See `doc.go` for the full godoc contract. The surface is:

- `Evidence(root, source, importAliases, importedParamMethods,
  directMethodCallRoots, packageImportPath, localNameBindings,
  constructorReturns, lookup) EvidenceSet` — the single entry point:
  registration evidence, same-package direct-method-call roots, and semantic
  evidence (delegated to `deadcode/semantic.CollectRoots`), composed into one
  `EvidenceSet`.
- `EvidenceSet` — `FunctionRootKinds`, `InterfaceRootKinds`, and
  `StructRootKinds`, each a lower-cased-identity-keyed map of root-kind
  strings. The golang parser (`language.go`) reads these fields directly when
  rendering each declaration's payload.
- `RootKinds(node, source, importAliases, registeredRootKinds) []string` —
  the root kinds for one function/method declaration node: registration
  matches plus signature-shape matches (HTTP handler, cobra `RunE`,
  controller-runtime `Reconcile`).
- `HTTPServeMuxVars(root, source, importAliases) map[string]struct{}` and
  `HTTPHandlerWrapperTarget(node, source, importAliases) string` — also called
  by `internal/parser/golang/framework_routes.go` to recognize the same
  `http.ServeMux`/`http.HandlerFunc` shapes for route-semantics extraction,
  so the two features agree on what counts as a registered mux variable and a
  wrapped handler target.

## Dependencies

- `internal/parser/golang/deadcode/semantic` (the semantic evidence pass;
  parent importing child), `internal/parser/golang/symbols` (import-alias
  resolution, identifier extraction, parent lookup), `internal/parser/shared`
  (node text/line helpers, the `GoImportedInterfaceParamMethods` /
  `GoDirectMethodCallRoots` collector-supplied types), and
  `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. Evidence gathering is a pure function; a reducer that drives the ingest
pipeline owns telemetry for the stage that calls it.

No-Regression Evidence: this is a pure package-move (issue #6774 stage 2,
`golang/dead_code_registrations.go` -> `golang/deadcode/registrations.go`,
`golang/dead_code_roots.go` -> `golang/deadcode/roots.go`, `package golang` ->
`package deadcode`). No function body changed beyond swapping the root's
`nodeText`/`walkNamed` forwarders for the `internal/parser/shared` calls they
forwarded to, swapping the root's `GoImportedInterfaceParamMethods`/
`GoDirectMethodCallRoots` type aliases for their `internal/parser/shared`
originals, and renaming the symbols the root and `framework_routes.go` still
call to their new exported names (`goDeadCodeEvidence` -> `Evidence`,
`goDeadCodeEvidenceSet` -> `EvidenceSet` with exported fields,
`goDeadCodeRootKinds` -> `RootKinds`, `goHTTPServeMuxVars` ->
`HTTPServeMuxVars`, `goHTTPHandlerWrapperTarget` -> `HTTPHandlerWrapperTarget`).
Verified by `go test ./internal/parser/golang/... -count=1` and the accuracy
golden gate (`scripts/verify_accuracy_golden_gate.sh`), both green on the
moved code.

No-Observability-Change: the move adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query.

## Gotchas / invariants

- **Registration matching is deliberately conservative and lower-cased**:
  `registrations.go` matches a compacted, lower-cased source rendering
  (`goCompactSource`) against known import aliases, so a registration through
  an unrecognized alias or a dynamically built handler name is a safe false
  negative, never a false positive.
- **Signature-shape roots require a resolved import alias**
  (`goSignatureMatchesHTTPHandler`, `goSignatureMatchesCobraRun`,
  `goSignatureMatchesControllerRuntimeReconcile` in `roots.go`): a same-shaped
  function from an unimported or differently-aliased package never matches.
- **`Evidence` composes in a fixed order**: registration roots, then
  same-package direct-method-call roots, then semantic roots — all mutate the
  same `FunctionRootKinds` map, so a root-kind's position in the emitted
  slice reflects that order (locked in by
  `TestDeadCodeCrossKindSameKeyOrdering` in root's
  `dead_code_gather_resolve_cross_kind_test.go`).
- **`EvidenceSet` fields are exported, not accessor methods**: the golang
  parser reads `FunctionRootKinds`/`InterfaceRootKinds`/`StructRootKinds`
  directly, matching the move discipline of exporting a struct field over
  inventing a getter for a boundary the field alone already crosses cleanly.

## Related docs

- Issue #6774 (parser/golang leaf split).
- `internal/parser/golang/deadcode/semantic/README.md` — the semantic
  evidence pass this package composes with registration and signature roots.
- `internal/parser/golang/README.md` — the parser this package's evidence
  feeds, and `framework_routes.go`, the other caller of
  `HTTPServeMuxVars`/`HTTPHandlerWrapperTarget`.
