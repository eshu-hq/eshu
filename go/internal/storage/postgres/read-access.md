# Postgres read adapters

`NewSQLReadStore(*sql.DB)` lets existing single-pool callers use the narrow
`db.ReadStore` contract: row, cursor, and read-only repeatable-read snapshot
operations. Its concrete SQL handle stays private, and the returned port has no
`Exec` or raw transaction method. The runtime reader pool implements the same
port with a per-connection freshness fence.

`InstrumentedQueryer` wraps only `db.Queryer`. It preserves the existing
Postgres read span, bounded query summary, and duration metric for stores that
need no write method. `InstrumentedDB` delegates its read method to this
wrapper, keeping the existing read telemetry contract.

`NewEshuSearchVectorMetadataReader(db.Queryer)` exposes only `ListActive` and
`Status`; `NewEshuSearchVectorValueReader(db.Queryer)` exposes only `ListActive`.
They use the existing active-generation SQL and bounded filters on the supplied
reader connection. Collector `NewEshuSearchVectorMetadataStore` and
`NewEshuSearchVectorValueStore` still accept `db.ExecQueryer` for fenced
upserts and batch writes. The reader types expose no write method or raw SQL
handle.

`NewTerraformConfigStateDriftFindingReader(db.Queryer)` binds both bounded
finding and count queries to a query-only port. The legacy constructor keeps
its `db.ExecQueryer` signature for existing callers. The query-layer drift
adapter wraps the reader with `InstrumentedQueryer`, preserving the existing
Postgres query signal.

## Additional query-only adapters

`NewMultiCloudRuntimeDriftFindingReader` exposes active drift-finding reads
through `db.Queryer`; the original constructor remains for write-capable
callers. Its list and count methods do not need an execution port.


`NewEshuSearchDocumentReader` supplies query-only access to active curated documents. The original store retains its write-capable vector-document methods for reducer callers.

AWS runtime drift and replatforming-scope reads expose `NewAWSCloudRuntimeDriftFindingReader(db.Queryer)`; the legacy store constructor remains available.
