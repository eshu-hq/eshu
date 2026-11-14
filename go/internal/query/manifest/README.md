# manifest query package

Serves `GET /api/v0/repository/manifest-dependencies`: the bounded,
repository-scoped inventory of declared manifest dependencies (package name,
declared version or range, manifest file, ecosystem, dev-or-runtime signal).

## Layout

- `doc.go` — route contract.
- `capability.go` — capability id `manifest.dependencies.list` and its
  derived-truth support row (single declaration; production registers it from
  `contract/capability_matrix_ext.go`).
- `store.go` — the `ManifestDependencyStore` query-local port, the
  `PostgresManifestDependencyStore` adapter over
  `storage/postgres` active-generation reads, and the envelope-to-row
  mapping (including the package-manager-to-ecosystem normalization).
- `handler.go` — the bounded read: capability gate, limit 1..200 (default
  50), `repository_id` anchor, `(after_observed_at, after_fact_id)` keyset
  cursor, grant short-circuit, truncation probe, freshness, truth envelope.
- `handler_tracing.go` — span helper and fixed failure messages.

## Telemetry

Span `query.manifest_dependencies` with repo/count/truncation attributes;
`eshu_dp_manifest_dependency_list_duration_seconds` and
`eshu_dp_manifest_dependency_list_errors_total`; the store path records the
`read=repository` manifest read metrics and delta-union outcomes. Refused
delta bases answer 503 and never serve delta-only rows as a complete page.
