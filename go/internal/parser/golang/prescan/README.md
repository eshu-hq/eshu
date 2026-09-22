# Go Pre-Parse Evidence

## Purpose

`prescan` provides the cheap, pre-parse Go evidence the collector's
package-level prescan pass needs before the real per-file parse runs:
deterministic symbol names for the import map (`PreScan`), and the per-file
interface/method-call evidence a package-level pass aggregates across every
file in a package before feeding it back into per-file parse options
(`PreScanFileEvidence` and its per-evidence functions). Extracted from
`internal/parser/golang` (issue #6774) so the root package stops importing
everything it needs from one flat, cycle-prone file set.

## Ownership boundary

This package owns pre-parse (before-the-real-parse) Go evidence collection:
cheap symbol-name prescan for the import map, and per-file interface/
method-call evidence for the package-level semantic-roots prescan. It does
NOT own the real per-file parse (`internal/parser/golang.Parse`) or dead-code
root evidence (`internal/parser/golang/deadcode`), which run only after a
package's prescan evidence is available. It does not own the symbol/parent-
lookup/type-index helpers shared across the `golang` parser (those live in
`internal/parser/golang/symbols`). It never imports `internal/parser/golang`
— that back-edge is exactly the cycle #6774 removes.

## Exported surface

See `doc.go` for the full godoc contract. The surface is:

- `PreScan(parser, path) ([]string, error)` — sorted function, struct, and
  interface names for one file's import-map prescan.
- `PreScanFileEvidence(parser, path) (*PrescanFileEvidence, error)` — the
  single-parse aggregate of every per-file evidence type below, for
  `internal/parser`'s package-level prescan loop.
- `PrescanFileEvidence` — the struct `PreScanFileEvidence` populates:
  `ImportedInterfaceParamMethods`, `ExportedInterfaceParamMethods`,
  `ImportedDirectMethodCallRoots`, `LocalInterfaceImportedMethodReturns`,
  `LocalInterfaceMethods`, `GenericConstraintInterfaceNames`,
  `MethodDeclarationKeys`.
- `ImportedInterfaceParamMethods`, `ExportedInterfaceParamMethods`,
  `ImportedDirectMethodCallRoots`,
  `ImportedDirectMethodCallRootsWithInterfaceReturns`,
  `LocalInterfaceImportedMethodReturns`, `LocalInterfaceMethods`,
  `GenericConstraintInterfaceNames`, `MethodDeclarationKeys` — the
  single-evidence functions `PreScanFileEvidence`'s one-parse walk mirrors.
  `ImportedDirectMethodCallRootsWithInterfaceReturns` is called directly by
  `internal/parser/go_package_interface_prescan.go`'s pass 5 (chained
  receiver-call roots using package-level interface returns, which needs a
  second per-file parse because its input is only known after the parent
  aggregates every file's returns).

## Dependencies

- `internal/parser/golang/symbols` (import-alias resolution, interface
  method extraction, the imported-variable-type index, parent lookup),
  `internal/parser/shared` (source reading, node text/line helpers, the
  `GoImportedInterfaceParamMethods`/`GoDirectMethodCallRoots` types), and
  `github.com/tree-sitter/go-tree-sitter`.
- `internal/parser/golang`'s `compat_prescan.go` depends on this package (not
  the reverse) to keep `internal/parser/go_language.go` and
  `internal/parser/go_package_interface_prescan.go` compiling against
  `golang.PreScan`, `golang.PreScanFileEvidence`, and
  `golang.ImportedDirectMethodCallRootsWithInterfaceReturns` unchanged.

## Telemetry

None. Every function here is a pure per-file read+parse+walk; a collector
stage that drives the prescan pass owns telemetry.

No-Regression Evidence: this is a pure package-move (issue #6774 stage 2,
`golang/prescan.go` -> `golang/prescan/prescan.go`,
`golang/package_interface_prescan.go` -> `golang/prescan/package_interface.go`,
`golang/package_prescan_evidence.go` -> `golang/prescan/package_evidence.go`,
`package golang` -> `package prescan`). No function body changed beyond
swapping the root's `nodeText`/`walkNamed` forwarders for the
`internal/parser/shared` calls they forwarded to — every exported symbol here
already used `shared.GoImportedInterfaceParamMethods`/
`shared.GoDirectMethodCallRoots` directly before the move, so no type-alias
substitution was needed. `internal/parser/golang/compat_prescan.go` keeps
`golang.PreScan`, `golang.PreScanFileEvidence`, `golang.PrescanFileEvidence`,
and `golang.ImportedDirectMethodCallRootsWithInterfaceReturns` compiling
unchanged for their external callers in `internal/parser/*.go`. Verified by
`go build ./internal/parser/...` (covers the compat-forwarder callers),
`go test ./internal/parser/golang/... -count=1`, and the accuracy golden gate
(`scripts/verify_accuracy_golden_gate.sh`), all green on the moved code.

No-Observability-Change: the move adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query.

## Gotchas / invariants

- **`PreScan` must stay cheap** (`prescan.go`): it collects only function,
  struct, and interface names directly from the tree — never dead-code root
  evidence or other `Parse`-only work, which doubled per-file cost on
  repo-scale dogfood inputs before this constraint was established (#161).
- **`PreScanFileEvidence` mirrors, not calls, the single-evidence functions**
  (`package_evidence.go`'s `extract*` helpers vs. `package_interface.go`'s
  exported functions): the two are kept behaviorally identical by design, but
  `PreScanFileEvidence` never re-reads or re-parses the file to call the
  exported functions — read `extractExportedInterfaceParamMethods`'s doc
  comment before assuming a shortcut exists.
- **Pass 5 is deliberately a second parse**
  (`ImportedDirectMethodCallRootsWithInterfaceReturns`): chained receiver-call
  roots need package-level interface-return metadata that is only known after
  every file in the package has been aggregated once, so this one evidence
  type cannot fold into `PreScanFileEvidence`'s single pass.
- **The compat forwarders in `internal/parser/golang/compat_prescan.go` are
  load-bearing**: `internal/parser/go_language.go` and
  `internal/parser/go_package_interface_prescan.go` call `golang.PreScan`,
  `golang.PreScanFileEvidence`, and
  `golang.ImportedDirectMethodCallRootsWithInterfaceReturns` by those exact
  names. Renaming a symbol here without updating the matching forwarder
  breaks that external, out-of-this-package-tree build.

## Related docs

- Issue #6774 (parser/golang leaf split).
- `internal/parser/golang/README.md` and `compat_prescan.go` — the root
  package this one was extracted from and its compatibility surface.
- `internal/parser/go_package_interface_prescan.go` — the package-level
  prescan loop (`PreScanGoPackageSemanticRoots`) that is this package's
  primary caller.
