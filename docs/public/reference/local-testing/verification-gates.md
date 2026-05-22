# Verification Gates

Use these after selecting the smallest proof from
[Local Testing](../local-testing.md). Run long Compose gates one at a time;
many allocate local ports and reuse Compose project state.

## Go Runtime Package Gate

Use this broad package gate for current runtime and collector wiring:

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

## Collector Gates

Use these focused checks when changing collector families or source providers.
Tracked evidence must name input size, fact count, wall time, remote/API
budget, and the telemetry that makes the source stage diagnosable.

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
```

The tier 1 and tier 2 scripts use distinct `COMPOSE_PROJECT_NAME` values and
dynamic host ports, so they can run side-by-side.

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
