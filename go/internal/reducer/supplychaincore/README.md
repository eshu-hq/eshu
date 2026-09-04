# Supply-chain core

## Purpose

`supplychaincore` holds the shared value types of the reducer supply-chain
impact family: the impact finding and the provenance, priority, reachability,
detection-profile, remediation, and suppression types it composes. It exists
to break a real import cycle (issue #6061) so the impact-finding and
suppression halves of the family can later split into sibling packages.

## Ownership boundary

This package owns only data types and their string-enum constants. Every
function that reads, decides, or builds these values -- suppression
evaluation, advisory provenance selection, priority scoring, reachability
enrichment, detection-profile classification, remediation, and the writer --
stays in the parent `internal/reducer` package, which aliases these types so
its existing spelling is unchanged.

## Exported surface

- `SupplyChainImpactFinding` -- the reducer-owned vulnerability impact finding.
- `SupplyChainImpactStatus` and its five status constants.
- `AlternateSeverity`, `FixedVersionBranch`, `AdvisorySourceObservation` --
  advisory provenance rows.
- `SupplyChainImpactPriorityContribution` -- one priority-score input.
- `SupplyChainReachabilityState` (six constants) and `SupplyChainReachability`.
- `SupplyChainServiceWorkloadPair` -- one co-occurring (service, workload) pair.
- `DetectionProfile` and its two tier constants.
- `SupplyChainImpactRemediation` -- the advisory-only safe-upgrade block.
- `SupplyChainSuppressionState` (eight constants) and
  `SupplyChainSuppressionDecision`.

See [doc.go](doc.go) for the package contract.

## Dependencies

The package imports only the standard library (`time`, for the suppression
decision's timestamps). It does not import the parent reducer package or any
sibling, storage, queue, graph, telemetry, or domain-family implementation, so
it can never re-form the cycle it was created to break.

## Telemetry

This package emits no telemetry. It declares data types only. The parent
reducer runtime records the spans, counters, and logs around the functions
that read and write these types.

## Gotchas / invariants

- Keep this package a leaf below `internal/reducer`; never import the parent.
- The parent reducer aliases every type here, so changing a field, a constant
  value, or a string enum literal changes the existing `reducer` API too, and
  the query and storage decoders that expect those wire values.
- `SupplyChainImpactFinding.Suppression` embeds `SupplyChainSuppressionDecision`
  and `SupplyChainImpactFinding.Remediation` embeds `SupplyChainImpactRemediation`;
  the finding's field-type closure is exactly what this leaf holds. A new field
  whose type is a reducer-root type must either hoist here too or the finding
  cannot stay in this leaf.

## Related docs

- [Reducer package](../README.md)
- [Reducer contract](../contract/README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
