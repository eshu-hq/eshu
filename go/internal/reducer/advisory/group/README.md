# internal/reducer/advisory/group

## Purpose

Owns the shared advisory-group projection contract for #10411: the grouped
advisory membership the reducer computes and the graph writer reconciles as
`Repository-[:AFFECTED_BY]->AdvisoryGroup`.

## Ownership boundary

**Owns:** the group and edge shapes (`Edge`, `Group`, `WinnerRow`,
`WriteStats`), the entity-key prefix, and `GroupIDFromEntityKey`.

**Does not own:** grouping logic and the projection handler (a sibling lane
adds `handler.go` here later), the Cypher
(`internal/storage/cypher/edge/writer/advisory_group.go`), the schema
(`internal/graph`), or the read surface.

## Exported surface

- `Edge` — one (repository, package) membership with ecosystem and PURL
- `Group` — one reconciled group: CVE/advisory ids plus full edge membership
- `WriteStats` — driver summary counters summed over one reconcile call
- `WinnerRow` — one grouped advisory row the handler reconciles
- `EntityKeyPrefix`, `GroupIDFromEntityKey` — entity-key to group-id mapping

See `doc.go` for the full godoc contract.

## Dependencies

- Standard library only (`strings`). The package must stay import-free of
  other Eshu packages so the reducer, storage, and (later) handler lanes can
  all import it without a cycle.

## Telemetry

No metrics, spans, or logs: this package holds types only.

## Gotchas / invariants

- A `Group` carries no evidence stamps by design. Do not add
  evidence_source, scope, or generation fields here.
- The (RepositoryID, PackageID) pair is the edge identity. Do not widen the
  `Edge` identity without updating the writer's `AFFECTED_BY` MERGE key.
- `WinnerRow.Members` includes the winner itself.

## Related docs

- Design rationale: `docs/internal/evidence/9209-3-2-advisory-group-cost.md`
