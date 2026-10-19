# Evidence: slice-B direct-family writers add no hot-path cost (#6228)

Records the performance and observability evidence for the slice-B offline
coverage commit (security_group_rule_endpoint, security_group_sg_rule,
s3_external_principal_grant, uses_service_account, plus verify-only
workload_cloud_relationship), so the hot-path evidence gate
(`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record
specific to this change rather than relying on PR prose.

## Change shape

Additive only: new Cypher edge writers under
`go/internal/storage/cypher/cloud/aws/...`, one shared-read-model addition in
`go/internal/reducer/secretsiam/secrets_iam_trust_chain_writer.go`, new
registry rows in `single_type_families.go`, plus Odu/guard/fixture test code.
No existing writer, query, batch size, worker knob, lease, retry, or queue
path is modified. New writers emit bounded exact sets per fixture (5-edge,
4-edge, 2-edge); the shared security-group Odu reuses one reachability read
model for two families instead of duplicating it.

## Performance Evidence:

- **Baseline / after** (same host, AMD EPYC 9R14, `-benchtime 100x -count=1`,
  `go/internal/reducer/secretsiam`, hermetic Go test, no backend):
  - `BenchmarkBuildSecretsIAMTrustChainReadModels`: base 48,331,078 ns/op,
    34,703,297 B/op, 332,334 allocs/op → branch 47,619,061 ns/op,
    34,702,979 B/op, 332,332 allocs/op.
  - `BenchmarkSecretsIAMGCPGrantObservations`: base 11,696,425 ns/op,
    10,135,945 B/op, 116,050 allocs/op → branch 11,734,685 ns/op,
    10,135,598 B/op, 116,048 allocs/op.
- Deltas are noise (±1.5% time, identical allocs). The touched writer file's
  benchmarks show no regression.

## No-Regression Evidence:

The change adds new write paths; no existing hot path changed shape, so no
existing budget holder can regress. Backend/version: no Cypher text of an
existing writer altered, no batch size or worker knob changed. Input shape:
fixture-bounded edge sets listed above. Live proof (Neo4j projection of the
new writers at corpus scale) belongs to the CI live matrices at landing;
waivers stand with `LiveProven` false.

## No-Observability-Change:

No metric, span, log key, or status surface added or altered (verified: no
otel/prometheus/telemetry/log/span additions in the diff). Nothing for an
operator to watch beyond existing writer telemetry.
