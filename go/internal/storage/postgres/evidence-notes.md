# storage/postgres Evidence Notes

Keep this file for scoped evidence that is too detailed for the package
orientation README.

## Tenant Workspace Grants (#2047)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'Test(BootstrapDefinitionsIncludeTenantWorkspaceGrants|TenantWorkspaceGrantStore)' -count=1` failed before `TenantWorkspaceGrantStore` and `tenant_workspace_grants` bootstrap DDL existed, then passed after adding idempotent tenant/workspace/scope/repository grant upserts, active bounded grant reads, tombstone/expiry/effective-time predicates, and the privacy guard that prevents raw display names or credential-shaped columns in the schema. The tables are additive and do not change existing fact, queue, graph, API, MCP, collector, or workflow behavior.

No-Observability-Change: #2047 adds no route, worker, queue domain, graph write,
metric name, metric label, runtime default, or API/MCP response field. Operators
diagnose store calls through the existing Postgres instrumentation wrapper
(`postgres.exec`, `postgres.query`, and
`eshu_dp_postgres_query_duration_seconds{store=...,operation=...}`) once callers
wrap the store's `ExecQueryer`; runtime enforcement and status/audit surfacing
are left to follow-up enforcement issues.

## Scoped API Tokens (#1852)

No-Regression Evidence: `go test ./internal/storage/postgres -run 'Test(BootstrapDefinitionsIncludeScopedAPITokens|ScopedAPITokenStore)' -count=1` failed before `ScopedAPITokenStore` and `scoped_api_tokens` bootstrap DDL existed, then passed after adding hash-only token upserts, active tenant/workspace bounded lookup, expiry/revocation predicates, and validation that rejects blank token hashes. The table is additive and does not store raw bearer tokens, tenant names, workspace names, provider credentials, API/MCP response fields, or graph truth.

No-Observability-Change: #1852 scoped-token registry plumbing adds no route,
worker, queue domain, graph write, metric name, metric label, runtime default,
or API/MCP response field. Runtime wiring and per-request enforcement are
follow-up work; when callers opt into the store through an instrumented
Postgres adapter, existing query/exec spans and
`eshu_dp_postgres_query_duration_seconds` cover the SQL.

## Cloud Inventory Evidence Loader (issues #1997, #1998)

`PostgresCloudInventoryEvidenceLoader` is the concrete
`reducer.CloudInventoryEvidenceLoader` that backs `DomainCloudInventoryAdmission`.
It reads the three provider inventory source fact kinds (`aws_resource`,
`gcp_cloud_resource`, `azure_cloud_resource`) for one scope generation and maps
each provider payload into the shared admission record. The admission handler
owns identity resolution, evidence folding, and the canonical
`reducer_cloud_resource_identity` upsert; the loader stays read-only so a stale
generation it happens to read is still superseded before any canonical write.

No-Regression Evidence: `go test ./internal/storage/postgres -run 'TestPostgresCloudInventoryEvidenceLoader|TestCloudInventoryAdmissionEndToEnd' -count=1` proves the loader reads exactly the three inventory source fact kinds for one `(scope_id, generation_id)`, drops blank-identity and undecodable rows, rejects a blank scope or generation, and that the end-to-end loader -> admission -> writer path is idempotent under retries and concurrent workers.

No-Observability-Change: the loader adds no table, route, worker, queue domain,
graph write, metric name, or metric label. The admission handler already emits
bounded cloud-inventory admission counters and the canonical
`reducer_cloud_resource_identity` read-model payload; the Postgres
instrumentation wrapper still emits `eshu_dp_postgres_query_duration_seconds`
for the load.
