# Secrets/IAM Graph Projection And ASSUMES_IAM_ROLE Edge Promotion Evidence (#1347, #1379, #1381)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Secrets/IAM graph projection evidence (#1347)

No-Regression Evidence: the `secrets_iam_graph_projection` domain is pure
in-memory extraction (`ExtractSecretsIAMGraphRows`, a single linear pass bounded
by read-model output) plus an orchestration handler that calls the
backend-neutral `cypher.SecretsIAMGraphWriter`. The domain is wired into the
additive registry but stays OFF by default: `DomainSecretsIAMGraphProjection`
registers only when `cmd/reducer` constructs a live writer, and that writer is
nil unless `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` is set truthy. So
no graph write executes in production from this PR — the registry gate sees a nil
writer and skips the domain, and the handler runs only against a recording writer
in tests. The handler writes all node families before all edge families (so each
edge `MATCH` resolves an already-committed node), retracts-before-reproject
(skipped only on a first-generation first attempt), and counts skipped rows.
Covered by `go test ./internal/reducer -run 'Extract|GraphProjection|AppendAdditiveDomains.*SecretsIAM'`,
`go test ./internal/storage/cypher -run SecretsIAMGraph`, and
`go test ./cmd/reducer -run SecretsIAMGraphProjectionWriter` (flag default-off,
enabled, and malformed-value cases). Cross-scope readiness gating, retry
liveness handling, the §11/§12 repo-local proofs, and ADR #1314 §14
principal/security sign-off are present. Activating the flag remains gated by
the target-bound activation record in #2430, including deployment binding and
flag-on proof.

Observability Evidence: the domain emits the `reducer.secrets_iam_graph_projection`
span and three bounded-enum counters — `eshu_dp_secrets_iam_graph_nodes_written_total`
`{node_type}`, `eshu_dp_secrets_iam_graph_edges_written_total{edge_type}`, and
`eshu_dp_secrets_iam_graph_skipped_total{skip_reason}` — plus a per-phase-duration
completion log. All labels are static extractor constants (no path/ARN/namespace),
and `node_type`/`edge_type`/`skip_reason` plus the span are in the frozen
telemetry contracts asserted by `go test ./internal/telemetry`.

## Secrets/IAM ASSUMES_IAM_ROLE edge promotion (#1379)

The fifth trust-chain edge, `SECRETS_IAM_ASSUMES_IAM_ROLE`, now promotes. The
trust-chain read-model row (`SecretsIAMIdentityTrustChain`) additionally carries
an optional `IAMRoleCloudResourceUID` — the redaction-safe IAM-role
`CloudResource` node uid — and a bounded `IAMRoleAssumeMode`
(`web_identity` / `pod_identity`). The build recomputes the uid at the existing
site (`secretsIAMRoleCloudResourceUID`) as
`cloudResourceUID(account_id, region, "aws_iam_role", role_arn)` from the
`aws_iam_principal` fact the chain already requires (the AWS resource collector
sets `resource_id = role_arn`), so no new collector, source field, or
cross-source join is introduced. The raw ARN is never stored; it is only hashed
into the one-way uid, identical to the AWS resource projection and the
`iam_can_assume` edge slice. When the principal fact omits `account_id`/`region`
the uid stays blank and the extractor keeps the prior skip+count behavior
(`iam_role_endpoint_unresolved_pending_read_model`). The Cypher writer adds a
static-token `MATCH (SecretsIAMServiceAccount)` / `MATCH (CloudResource)` /
`MERGE ...SECRETS_IAM_ASSUMES_IAM_ROLE...` template; a missing `CloudResource`
endpoint is a no-op (never a fabricated node), and the edge's reducer-owned START
node is removed by the existing `DETACH DELETE` retract. See ADR #1314 §5.1/§5.3.

No-Regression Evidence: the read-model build stays a single pass over facts
already loaded (one map lookup + one `cloudResourceUID` hash per exact chain — no
new fact load). The extractor adds one endpoint-pair-deduped edge family to the
same linear `ExtractSecretsIAMGraphRows` pass; the writer adds one no-op-safe
edge template and no new retract statement. The change is exact-only and
endpoint-no-op-safe, so absence of the uid preserves prior behavior and there is
no hot-path regression. Proven by `go test ./internal/reducer ./internal/projector ./internal/storage/cypher ./internal/telemetry ./internal/graph -count=1`,
`go test -race ./internal/reducer -count=1`, and
`golangci-lint run ./internal/reducer/... ./internal/storage/cypher/...` (0 issues)
on Go 1.26.x: build resolves and matches `cloudResourceUID`, blank uid without
account/region, web-identity vs pod-identity assume mode, extractor emits when
resolvable and skips+counts when only the fingerprint is present, duplicate
dedupe, and the projection handler writes/omits the edge end-to-end.

Observability Evidence: no new telemetry surface. The new edge flows through the
existing `eshu_dp_secrets_iam_graph_edges_written_total{edge_type=
SECRETS_IAM_ASSUMES_IAM_ROLE}` counter, and the existing
`eshu_dp_secrets_iam_graph_skipped_total{skip_reason=
iam_role_endpoint_unresolved_pending_read_model}` counter still fires when the
uid is absent, so an operator can chart resolved-vs-skipped IAM-role edges per
generation. The frozen `edge_type`/`skip_reason` dimension keys are unchanged
(`go test ./internal/telemetry`).
### §11/§12 activation proofs (#1381)

The ADR #1314 §11 fixture-truth, §12 performance, repo-local backend
conformance proofs, and §14 principal/security sign-off now exist. Activation
still requires #2430's target deployment decision and flag-on proof before the
in-principle approval binds to one deployment. The flag stays OFF by default and
is unchanged; these are proof artifacts only.

Benchmark Evidence (§12): `BenchmarkSecretsIAMGraphWriter`
(`go/internal/storage/cypher/secrets_iam_graph_writer_bench_test.go`) writes all
four `SecretsIAM*` node families and all five resolvable `SECRETS_IAM_*` edge
families at 5,000 rows each through the no-op group executor, isolating
statement construction and `UNWIND` batching from graph round trips. Measured on
an Apple M4 Pro (`darwin/arm64`), `go test ./internal/storage/cypher -run '^$'
-bench BenchmarkSecretsIAMGraph -benchmem -benchtime=50x -count=3`:

| Writer | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkSecretsIAMGraphWriter` (9 surfaces × 5,000 rows) | ~20,819–38,670 | 53,728–53,729 | 765 |
| `BenchmarkCloudResourceNodeWriter` (1 node surface × 5,000) | ~2,867,595 | 6,327,775 | 25,068 |
| `BenchmarkKubernetesCorrelationEdgeWriter` (1 edge surface × 5,000) | ~1,095,590 | 2,164,620 | 25,097 |
| `BenchmarkObservabilityCoverageEdgeWriter` (1 edge surface × 5,000) | ~1,660,936 | 3,885,912 | 40,100 |
| `BenchmarkSecurityGroupReachabilityWriter` (3 surfaces × 5,000) | ~3,978,295 | 7,759,556 | 75,367 |

The secrets/IAM writer is faster than every shipped baseline because each of its
nine surfaces is one homogeneous static template streamed directly into `UNWIND`
batches (90 batch statements per op, 765 allocs total), with no per-row
token-grouping and no per-edge graph read. This is the §12 "build in-memory once
per scope, no N+1" contract proven: the write side is in the same shape class as
the proven node/edge writers and is far below the §12 ~10% regression stop
threshold against them.

No-Regression Evidence (§11): `TestGraphProjectionFixtureTruth*`
(`go/internal/reducer/secretsiam/secrets_iam_graph_projection_fixture_truth_test.go`) drives
the full load → extract → write orchestration through
`SecretsIAMGraphProjectionHandler` against the in-memory recording writer and
asserts the exact node/edge rows handed to all four node-family and all five
edge-family writer surfaces, plus the skip-counted cases (missing-workload,
missing-vault-hop, missing-secret-path, non-exact states, pod-identity
IAM-role-unresolved) and duplicate-delivery idempotency. The TRUE live-backend
conformance is the BACKEND-GATED
`TestSecretsIAMGraphWriterLiveConformance`
(`go/internal/storage/cypher/secrets_iam_graph_live_test.go`), which writes all
four node families and five edges, reads them back, and proves scoped retract
leaves the retained `KubernetesWorkload` and `CloudResource` endpoints intact — it SKIPs cleanly
unless `ESHU_SECRETS_IAM_GRAPH_LIVE=1` and Bolt env are configured, so the
default test run never fabricates a live proof. Run with
`go test ./internal/storage/cypher ./internal/reducer -run
'SecretsIAMGraph|GraphProjection' -count=1`.

The June 7 proof snapshot in
`docs/internal/design/1314-secrets-iam-graph-promotion-proof-2026-06-07.md`
records NornicDB and Neo4j live writer conformance, shared backend conformance,
schema readback, focused package gates, and the §12 benchmark rerun.

Activation remains blocked: enabling
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` for live execution still
requires #2430's target deployment decision and flag-on proof, which these
repo-local proofs do not grant.
