# querycontract/evidence

Evidence-citation contract and evidence-boundary disclosures for query
responses.

## What is here

| file | holds |
| --- | --- |
| `citation_handle.go` | `EvidenceCitationHandle`, `EvidenceCitationHandleKey` |
| `citation.go` | `EvidenceCitationResponse`, `EvidenceCitation`, `EvidenceCitationCoverage`, `EvidenceCitationProvenance` |
| `boundaries.go` | `EvidenceBoundariesFor`, `AttachEvidenceBoundaries`, `PostgresOnlyBoundary`, `BoundaryReasonPostgresOnly` |

## Dependencies

Inbound: 23 non-test and 10 test files import this package, including the
parent `querycontract` (its answer packets name `EvidenceCitationHandle`) and
`querycontract/visualization` (its nodes carry citation handles).

Outbound: the standard library only. This package must not import
`querycontract`, or the parent's import of it becomes a cycle.

## Telemetry

None.
