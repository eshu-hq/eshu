# Content read port

`NewContentReaderWithReadStore` accepts `db.ReadStore` for guarded Postgres
reads. Every content row, cursor, and hardcoded-secret snapshot uses that port.
The secrets side-table readiness check and follow-up side-table query share a
read-only repeatable-read snapshot when readiness is true. If readiness changes
before the snapshot, the reader rolls it back and uses the legacy scan.

`NewContentReader(*sql.DB)` keeps the existing constructor through
`postgres.NewSQLReadStore` for single-pool callers. The adapter changes
connection ownership without changing content SQL, ordering, or result shapes.
The API and MCP wiring must supply the guarded read store when a separate
reader endpoint is configured.

## Infrastructure aggregate and deployment evidence readers

`NewInfraResourceAggregateStoreWithReadStore` accepts a guarded `db.Queryer`
for the Postgres infrastructure read model. A failed read-model readiness
check returns its error and does not fall back to graph results. Graph-only
labels still use the graph without checking Postgres readiness.

`deployment.NewPostgresKubernetesPodTemplateStoreWithReadStore` accepts a
guarded `db.Queryer` for both tracking-id and declared-object live evidence,
including existence and bounded list reads. Existing SQL constructors remain
available for single-pool callers.

## Aggregate and drift readers

Guarded Postgres readers: the container-image, SBOM-attachment, and documentation-finding aggregate stores accept read-only stores through their `WithReadStore` constructors. Runtime drift accepts a guarded query-only reader and keeps its query telemetry.
