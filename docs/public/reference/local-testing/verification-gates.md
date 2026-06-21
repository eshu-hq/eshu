# Verification Gates

Use these after selecting the smallest proof from
[Local Testing](../local-testing.md). Run long Compose gates one at a time;
many allocate local ports and reuse Compose project state.

## Go Runtime Package Gate

Use this broad package gate for runtime and collector wiring:

```bash
cd go
go test ./internal/parser ./internal/collector/discovery ./internal/content/shape \
  ./internal/collector ./cmd/collector-git ./cmd/collector-terraform-state \
  ./cmd/collector-aws-cloud \
  ./cmd/ingester ./cmd/bootstrap-index \
  ./internal/runtime ./internal/app ./internal/telemetry \
  ./internal/storage/cypher ./internal/storage/neo4j ./internal/storage/postgres \
  ./internal/projector ./internal/reducer ./cmd/reducer -count=1
```

## Replatforming API/MCP Parity Proof

Run this gate when changing any replatforming serving surface: the plan
(`POST /api/v0/replatforming/plans` / `compose_replatforming_plan`),
ownership-packet
(`POST /api/v0/replatforming/ownership-packets` / `find_unmanaged_resource_owners`),
or rollup
(`POST /api/v0/replatforming/rollups` / `get_replatforming_rollups`) route or
tool, or their shared source-state, safety-gate, or readiness logic.

```bash
cd go
go test ./internal/mcp -run TestReplatforming -count=1
```

This is an in-process, fixture-backed proof. It mounts one `query.IaCHandler`
over a deterministic IaC-management fixture store and drives each request twice:
once straight through the HTTP route and once through the real MCP dispatch path
(`dispatchTool` → `resolveRoute` → the same mounted handler →
`parseCanonicalEnvelope`). It then asserts:

- **API/MCP parity** — for one scope the HTTP route and the MCP tool return the
  identical canonical envelope `Data` block plus identical truth label (level,
  basis, capability, freshness). Full-`Data` equality pins bounded results,
  source-state totals, readiness counts, refusal reasons, and stories to one
  contract, so the two surfaces cannot diverge silently.
- **Refusal safety** — safety-gated findings (`security_review_required`,
  ambiguous, stale, unknown) resolve to the `rejected` source state and a
  refused import candidate with reasons; they are never silently omitted nor
  counted as import-ready. Only a safety-approved `cloud_only` finding with a
  supported import mapping is import-ready.
- **Profile/truth bounds** — an unsupported runtime profile returns
  `unsupported_capability` on both surfaces instead of a downgraded answer, and
  neither surface leaks a confident truth level on the refusal path.
- **Negative leakage** — a credential-shaped raw tag value never appears in
  either surface's serialized payload.

The fixture proof is the deterministic CI gate. The operator-facing complement
is the remote all-collector Compose proof in
[Remote collector E2E](remote-collector-e2e.md): once a representative or
full-corpus stack has drained AWS runtime drift, drive the same three routes and
tools against the live API and MCP server and compare the bounded payloads,
truth labels, source-state counts, readiness counts, and refusal summary for the
same scope. The Compose proof records fact counts, queue and dead-letter state,
and the safety/refusal summary; this in-process gate proves the surfaces agree
before that run.

No-Observability-Change: this gate exercises the existing replatforming query
spans and truth envelopes; it adds no metric, span, or log. Runtime diagnosis of
the same surfaces still uses the `query` handler spans named in
`go/internal/telemetry/contract.go` and the canonical response envelope.

## Collector Gates

Use focused checks when changing collector families or source providers.
Tracked evidence must name input size, fact count, wall time, API budget, and
telemetry.

```bash
cd go

go test ./internal/collector/terraformstate -count=1 -run TestParseStream_PeakMemoryGate

go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceRecordsEmissionCounters|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v

go test ./internal/collector/ociregistry/ociruntime \
  -run 'TestSourceNextEmitsCollectedGenerationForRegistryTarget|TestClaimedSourceNextClaimedScansMatchingTargetWithClaimGeneration' \
  -count=1 -v

go test ./internal/collector/packageregistry/packageruntime \
  -run 'TestClaimedSourceParsesMetadataIntoPackageRegistryFacts|TestClaimedSourceTruncatesMetadataOverVersionLimit|TestClaimedSourceSanitizesSourceURIBeforeFactEmission' \
  -count=1 -v

go test ./internal/collector/confluence \
  -run 'TestSourceRecordsBoundedConfluenceMetrics|TestHTTPClientRecordsBoundedRequestMetrics' \
  -count=1 -v
```

Terraform-state parser trend and large-state proof:

```bash
cd go
go test -bench=BenchmarkParseStream_LargeState -benchmem -run=^$ \
  ./internal/collector/terraformstate

ESHU_TFSTATE_100MIB_PROOF=true \
  go test ./internal/collector/terraformstate -count=1 \
  -run TestParseStreamLargeState100MiBStreamingProof -timeout 300s
```

## Local-Authoritative Gates

Before a run that executes local Eshu binaries:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Use these for local-host startup, graph-backed query compatibility, or
NornicDB routing:

```bash
ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeStartupEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeCallChainSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeTransitiveCallersSyntheticEnvelope -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/eshu-bare-install-smoke/bin/nornicdb-headless \
ESHU_LOCAL_AUTHORITATIVE_PERF=true \
  go test ./cmd/eshu -run TestLocalAuthoritativeDeadCodeSyntheticEnvelope -count=1 -v
```

Manual MCP smokes should end with:

```bash
eshu graph stop --workspace-root "$PWD"
eshu graph status --workspace-root "$PWD"
```

The status output should report no active owner for that workspace.

## Compose Gates

Run only the gate that matches the touched behavior. These scripts own fixture
setup and teardown; use them instead of hand-run Compose when you need
acceptance evidence.

```bash
./scripts/verify_collector_git_runtime_compose.sh
./scripts/verify_projector_runtime_compose.sh
./scripts/verify_reducer_runtime_compose.sh
./scripts/verify_incremental_refresh_compose.sh
./scripts/verify_webhook_refresh_compose.sh
./scripts/verify_relationship_platform_compose.sh
./scripts/verify_admin_refinalize_compose.sh
./scripts/verify_graph_analysis_compose.sh
./scripts/verify_correlation_dsl_compose.sh
```

For a no-credential proof of additional lanes against public, unauthenticated
endpoints (CISA KEV, FIRST EPSS, OSV, public npm), use the public-collector
gate. It claim-drives the workflow-coordinator and asserts fact commit, reducer
drain to zero, and API/MCP readback with aggregate-only, public-safe output. See
[Public Collector Proof](public-collector-proof.md).

```bash
./scripts/verify_local_public_collector_proof.sh --check   # no Docker, no network
./scripts/verify_local_public_collector_proof.sh           # live public proof
```

Use `./scripts/verify_product_truth_fixtures.sh` when changing product truth
across graph, evidence, API, MCP, CLI, or cleanup workflows.

## Targeted Graph And Terraform Gates

NornicDB grouped-write probes:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteSafetyProbe -count=1 -v

ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless-eshu-rollback \
ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance -count=1 -v
```

Normal laptop runs should leave `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`
unset.

Terraform provider-schema and relationship extraction:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1
```

Terraform config-vs-state drift:

```bash
bash scripts/verify_tfstate_drift_compose.sh

ESHU_TFSTATE_DRIFT_PROOF_OUT=/tmp/eshu-tfstate-drift-compose-$(date +%Y-%m-%d).md \
  bash scripts/verify_tfstate_drift_compose.sh

bash scripts/verify_tfstate_drift_compose_tier2.sh

bash scripts/verify_tfstate_drift_compose_tier2_v25.sh
```

The Terraform-state scripts use distinct `COMPOSE_PROJECT_NAME` values and
dynamic host ports, so tier 1, tier 2, and v2.5 proofs can run side by side.

Webhook refresh:

```bash
bash scripts/verify_webhook_refresh_compose.sh
```

Expected observability: `webhook_refresh_triggers.status`, existing webhook
decision/store metrics, bounded listener request logs, and ingester Git sync
lifecycle logs.

## Runtime Tree Hygiene

The deployable runtime tree is Go-only:

```bash
rg --files . -g '*.py' | rg -v '^(\\./)?tests/fixtures/'
```

Fixture data under `tests/fixtures/` and explicitly offline-only tooling can
still carry Python source when they are not part of the deployable runtime.
