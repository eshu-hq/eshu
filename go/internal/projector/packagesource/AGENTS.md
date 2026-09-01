# AGENTS.md — package-source-correlation projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants, including
   the rule that source hints are provenance only in the projector.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs first.
5. `go/internal/reducer/package_source_correlation_handler.go` for what the
   reducer does with the intent this package enqueues.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildPackageSourceCorrelationReducerIntent` probes
  `package_registry.source_hint` first and `package_registry.package` second,
  each with `FirstOfKind`, and anchors to the earliest fact of the first kind
  that is present. Do not merge the two probes into one cross-kind scan, swap
  their order, or add a third trigger kind; each changes `FactID` or `Reason`
  for generations that carry both kinds, and the root fan-out parity fixture
  pins the identity branch.
- Keep the two `Reason` strings and the `package_source_correlation:<scope>`
  entity key byte-identical. The reducer claims one intent per scope
  generation and reloads the facts itself.
- Do not decode a payload here. This builder reads only `envelope.FactKind`;
  `sdk/go/factschema/packageregistry/v1/README.md` records it as a
  routing-only consumer of `SourceHint`, and adding a decode predicate would
  silently turn a malformed hint from "anchors the intent" into "skipped".
- Do not classify hints, decide ownership or publication, admit consumption
  edges, or write to the graph here. The reducer's
  `DomainPackageSourceCorrelation` handler owns all of that.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree, and
the golden-corpus gates selected by the changed paths.
