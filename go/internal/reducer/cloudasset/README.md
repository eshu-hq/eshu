# cloudasset

## Purpose

Resolves canonical cloud asset identity across sources and writes one durable
`reducer_cloud_asset_resolution` fact per reconciliation intent. This package
moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the cloud asset resolution handler
(`CloudAssetResolutionHandler.Handle`) and the durable fact writer
(`PostgresCloudAssetResolutionWriter`).

It does **not** own the `DomainCloudAssetResolution` constant or its
`DomainDefinition` entry, which stay in the reducer root's core catalog
(`registry.go`'s `DefaultDomainDefinitions`) alongside `DomainWorkloadIdentity` --
both are always-registered domains, not additive ones, so their catalog entry
belongs with the root's other core definitions. Only the `Handler` field is
wired from this package (`defaults_domain_catalog.go`).

## Exported surface

- `CloudAssetResolutionHandler` -- `defaults_domain_catalog.go`
- `CloudAssetResolutionWriter` -- `defaults.go`; `DefaultHandlers` declares
  `CloudAssetResolutionWriter` with this type directly -- no root compat file
- `PostgresCloudAssetResolutionWriter` -- `cmd/reducer`, and
  `internal/replay/costcounting`'s cost-budget tests
- `CloudAssetResolutionWrite`, `CloudAssetResolutionWriteResult` -- the
  handler/writer request and result shapes

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/gpphase`, `reducer/factwrite`,
`reducer/payloadcore`, `internal/facts`. Never `internal/reducer`.

## Telemetry

No metric instrument. The domain publishes a graph-readiness phase
(`gpphase.PublishIntentGraphPhase`, keyspace `cloud_resource_uid`, phase
`canonical_nodes_committed`) on a successful write; that phase publication is
observed through the existing `graph_projection_phase_state` durable rows and
`GraphProjectionPhaseRepairQueue`, not a dedicated counter.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root supplied as one-line forwarders or aliases (`Intent`/`Result`/
`ResultStatusSucceeded`/`DomainCloudAssetResolution`,
`GraphProjectionPhasePublisher`/`GraphProjectionKeyspaceCloudResourceUID`/
`GraphProjectionPhaseCanonicalNodesCommitted`/`publishIntentGraphPhase`,
`uniqueSortedStrings`, `workloadIdentityExecer`/`canonicalReducerFactInsertQuery`/
`reducerWriterNow`/`reducerFactCollectorKind`) are now called directly against
the leaf package that already owned them (`reducer/contract`, `reducer/gpphase`,
`reducer/payloadcore`, `reducer/factwrite`). Measured on this branch:
`go build ./...` exits 0, `go vet ./...` exits 0,
`go test ./internal/reducer/cloudasset -count=1` passes, and
`go test ./internal/reducer/... ./cmd/reducer ./internal/storage/postgres
./internal/replay/costcounting -count=1` passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph
query shape, metric instrument, or metric label. The graph-phase publication
is the same before and after the move.

## Gotchas / invariants

- `DomainCloudAssetResolution` and its `DomainDefinition` entry stay in the
  reducer root -- do not move them here even though the handler lives here.
- An intent with no entity keys, or no related scope id after including its
  own `ScopeID`, MUST be rejected before the writer is called
  (`cloudAssetResolutionWriteFromIntent`); do not fabricate an empty write.
- Entity keys and related scope ids MUST be deduplicated and sorted
  (`payloadcore.UniqueSortedStrings`) so the stable fact key and canonical id
  stay identical across retries and replays.
- The graph phase publish happens AFTER the durable write succeeds, never
  before -- a canonical-nodes-committed phase must not be published for a
  reconciliation that never landed.
- `reducer_cloud_asset_resolution` currently has no production consumer
  (documented exemption); if one is added, promote the fact kind to full
  governance in the same PR (see
  `docs/internal/design/4784-reducer-derived-fact-governance.md`).
- Test helper (`recordingGraphProjectionPhasePublisher`) is duplicated from
  the reducer root by design; Go test files cannot share unexported symbols
  across a package boundary.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/gpphase/README.md`
- `go/internal/reducer/factwrite/README.md`
- `docs/internal/design/4784-reducer-derived-fact-governance.md`
- `docs/internal/design/4786-contract-integration-matrix.md`
