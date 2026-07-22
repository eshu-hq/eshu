# Evidence: S3 external-principal grant posture read (issue #5643)

Issue #5643 wires the previously write-only
`(:CloudResource)-[:GRANTS_ACCESS_TO]->(:ExternalPrincipal)` graph truth into
its spec-declared read surface, `GET /api/v0/secrets-iam/posture-summary`, as
a bounded grant-posture section (`secrets_iam_grant_posture.go`).

No-Regression Evidence: This change adds five new read-only aggregate Cypher
statements and modifies no existing query, writer, or hot-path statement. Each
statement anchors on the `GRANTS_ACCESS_TO` relationship type — a population
written only by the `s3_external_principal_grant_materialization` reducer
domain and bounded by the corpus's S3 bucket-policy external-principal
statements (expected O(10^2..10^3) edges) — filters on `rel.scope_id =
$scope_id`, and returns aggregate rows only (grouped `count(*)` over the
closed `grant_outcome`/`resolution_mode` vocabularies, or a single filtered
`count(*)` total), so output cardinality is a handful of rows per statement.
The grouped reads deliberately use the single-grouping-key
`RETURN <bucket expr>, count(*)` shape already validated by the
package-registry aggregate store (`package_registry_aggregates.go`) instead of
one multi-key grouping statement, because multi-key grouped aggregation is not
a validated hot-path template on the pinned NornicDB binary. All five
statements share one 10s deadline (`secretsIAMGrantPostureReadTimeout`,
mirroring `codeownersOwnershipReadTimeout`) and run only on posture-summary
requests (a scope-anchored dashboard read, not a per-fact or per-projection
path). Statement-shape and folding behavior are locked by
`secrets_iam_grant_posture_test.go` (scoped/bounded-aggregate shape test,
deadline assertion, allow-list injection guard) and
`secrets_iam_summary_test.go` (handler integration, omission when unwired,
error propagation).

Observability Evidence: The read executes inside the existing
`telemetry.SpanQuerySecretsIAMPostureSummary` handler span
(`startQueryHandlerSpan`, `secrets_iam_summary.go`), so operators see the
grant read's latency and failures on the existing
`GET /api/v0/secrets-iam/posture-summary` span and the shared per-endpoint
HTTP request metrics; a graph failure surfaces as the route's 500 with the
wrapped `summarize s3 external-principal grant posture` error. No new metric
is introduced: the route was already instrumented and the new read adds no
background or per-fact stage.
