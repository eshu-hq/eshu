# Canonical Terraform State

## Purpose

This package builds Terraform state graph rows and statement-free write parts,
including MATCHES_STATE resolution and Terraform resource attribute promotion.

## Ownership boundary

The leaf owns query templates, row shaping, resolver contracts, and ordering of
Terraform state parts. The parent `cypher` package owns executable `Statement`
wrapping, the writer API, and canonical phase dispatch.

## Exported surface

- `BuildTerraformStateStatements` — build the ordered parts for one materialization
- `Part` — statement-free Cypher plus parameters and execution flags
- `TerraformStateOwnershipResolver` and `TerraformStateConfigMatchResolver` — resolver ports

See `doc.go` for the package contract.

## Dependencies

- `internal/projector/canonical` — materialization and resolver data contracts
- `internal/storage/cypher/infra/search` — backend-neutral search-key query clause

## Telemetry

None. The parent writer executes parts through its existing instrumentation.

## Gotchas / invariants

- Preserve the order of migration, attribute removal, stale-content removal,
  upsert, resource retract, module/output upserts, then relationship retract
  before relationship upsert.
- MATCHES_STATE relationship retractions set `Drain` so the root executor can
  run them as standalone autocommit statements.
- Resolver ambiguity fails closed; transient ownership failures preserve edges.

## Performance Evidence

Benchmark Evidence: `BenchmarkBuildTerraformStateStatementsSyntheticCorpus`
(10,000 resources, five samples, `-benchmem`) measured 17.776–19.615 ms/op,
20,605,502–20,605,711 B/op, and 270,707–270,708 allocs/op before the move. The
final run measured 17.498–17.987 ms/op, 20,603,440–20,603,750 B/op, and
270,708–270,709 allocs/op. Both runs emitted 82 statements; latency was
comparable and bytes allocated were slightly lower, with one additional
allocation in some samples. The benchmark exercises in-process statement
building only. Backend execution shape is unchanged because all 11 Cypher
template sets are byte-identical to the pre-move root versions; the production
writer's delta-cycle MATCHES_STATE live regression passes both tests in its
file on the pinned Neo4j backend from `docker-compose.live-backend-neo4j.yml`
(fresh volume, package time 3.054s).

No-Observability-Change: the root writer retains phase execution,
instrumentation, metadata, and logging; this package adds no telemetry.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
