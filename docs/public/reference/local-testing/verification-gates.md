# Verification Gates

Use these gates after selecting the smallest proof from the matrix in
[Local Testing](../local-testing.md). Run long Compose gates one at a time; many
allocate local ports and reuse Compose project state.

## Go Runtime Package Gate

Use this gate when validating current runtime and collector wiring:

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

## Collector Performance Gates

Use these focused checks when changing collector families or expanding a source
provider. The tracked evidence note must name input size, fact count, wall
time, remote/API budget, and telemetry that proves the source stage is
diagnosable.

```bash
cd go

# Terraform-state parser memory and throughput.
go test ./internal/collector/terraformstate -count=1 -run TestParseStream_PeakMemoryGate
go test -bench=BenchmarkParseStream_LargeState -benchmem -run=^$ \
  ./internal/collector/terraformstate

# AWS claim scan counters, emitted fact counts, API stats, and budget status.
go test ./internal/collector/awscloud/awsruntime \
  -run 'TestClaimedSourceRecordsEmissionCounters|TestClaimedSourceRecordsScanStatusWithAPICallStats' \
  -count=1 -v

# OCI registry target scan, manifest/referrer fact shape, and bounded labels.
go test ./internal/collector/ociregistry/ociruntime \
  -run 'TestSourceNextEmitsCollectedGenerationForRegistryTarget|TestClaimedSourceNextClaimedScansMatchingTargetWithClaimGeneration' \
  -count=1 -v

# Package-registry metadata fetch, parser bounds, fact emission, and URL scrubbing.
go test ./internal/collector/packageregistry/packageruntime \
  -run 'TestClaimedSourceParsesMetadataIntoPackageRegistryFacts|TestClaimedSourceTruncatesMetadataOverVersionLimit|TestClaimedSourceSanitizesSourceURIBeforeFactEmission' \
  -count=1 -v

# Confluence source-stage metrics and high-cardinality label guard.
go test ./internal/collector/confluence \
  -run 'TestSourceRecordsBoundedConfluenceMetrics|TestHTTPClientRecordsBoundedRequestMetrics' \
  -count=1 -v
```

## Local-Authoritative Gates

Before a local-authoritative run that executes local Eshu binaries, rebuild the
owner and child binaries and put the install directory on `PATH`:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"
```

Use these focused gates when touching local-host startup, graph-backed query
compatibility, or NornicDB routing:

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

Manual MCP local-authoritative smokes should end with:

```bash
eshu graph stop --workspace-root "$PWD"
eshu graph status --workspace-root "$PWD"
```

The status output should report no active owner for that workspace.

## Compose Verification Gates

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

Use `./scripts/verify_product_truth_fixtures.sh` when changing a feature Eshu
claims as product truth across graph, evidence, API, MCP, CLI, or cleanup
workflows.

## NornicDB Grouped-Write Safety

Use this opt-in gate when touching grouped canonical writes or
`ESHU_NORNICDB_CANONICAL_GROUPED_WRITES`:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteSafetyProbe -count=1 -v
```

The stricter promotion gate is:

```bash
ESHU_NORNICDB_BINARY=/tmp/nornicdb-headless-eshu-rollback \
ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK=true \
  go test ./cmd/eshu -run TestNornicDBGroupedWriteRollbackConformance -count=1 -v
```

Normal laptop runs should leave `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` unset.

## Terraform Provider-Schema Gate

Use this gate when touching Terraform provider schemas or schema-driven
relationship extraction:

```bash
cd go
go test ./internal/terraformschema ./internal/relationships ./internal/storage/postgres -count=1
```

The canonical packaged schemas live under
`go/internal/terraformschema/schemas/*.json.gz`.

## Terraform-State Parser Memory Gate

Use these checks when touching the Terraform-state parser or any code on the
`ParseStream` path. `TestParseStream_PeakMemoryGate` is the hard CI gate. The
benchmark and 100 MiB proof are for trend tracking and periodic large-scale
validation.

```bash
cd go

go test ./internal/collector/terraformstate -count=1 -run TestParseStream_PeakMemoryGate

go test -bench=BenchmarkParseStream_LargeState -benchmem -run=^$ \
  ./internal/collector/terraformstate

ESHU_TFSTATE_100MIB_PROOF=true \
  go test ./internal/collector/terraformstate -count=1 \
  -run TestParseStreamLargeState100MiBStreamingProof -timeout 300s
```

## Terraform Config-vs-State Drift Compose Proofs

Use these gates when touching `DomainConfigStateDrift`, the Phase 3.5 drift
enqueue path, `terraformBackendCandidate`, related canonical-side reads, or the
`collector-terraform-state` binary.

Tier 1 is the seeded-fact handler proof:

```bash
bash scripts/verify_tfstate_drift_compose.sh

ESHU_TFSTATE_DRIFT_PROOF_OUT=/tmp/eshu-tfstate-drift-compose-$(date +%Y-%m-%d).md \
  bash scripts/verify_tfstate_drift_compose.sh
```

Tier 2 runs the real collector chain through minio and active workflow
coordination:

```bash
bash scripts/verify_tfstate_drift_compose_tier2.sh
```

Both verifiers use distinct `COMPOSE_PROJECT_NAME` values and dynamic host
ports, so they can run side-by-side:

```bash
bash scripts/verify_tfstate_drift_compose.sh &
bash scripts/verify_tfstate_drift_compose_tier2.sh &
wait
```

## Webhook Refresh Compose Proof

Use this gate when touching `go/cmd/webhook-listener`, `go/internal/webhook`,
`WebhookTriggerStore`, or the ingester webhook-trigger handoff path:

```bash
bash scripts/verify_webhook_refresh_compose.sh
```

The verifier creates a local bare Git remote, seeds the managed workspace
checkout, indexes the first generation, advances the remote default branch,
sends a signed GitHub `push` webhook to `eshu-webhook-listener`, and verifies
the queued trigger is handed off through the ingester to a new generation whose
content is visible through the HTTP API.

Expected observability: `webhook_refresh_triggers.status`, existing webhook
decision/store metrics, bounded listener request logs, and ingester Git sync
lifecycle logs.

## Runtime Tree Hygiene

The deployable runtime tree is Go-only. Use this check when confirming that
runtime implementation has not drifted into Python:

```bash
rg --files . -g '*.py' | rg -v '^(\\./)?tests/fixtures/'
```

Fixture data under `tests/fixtures/` and explicitly offline-only tooling can
still carry Python source when they are not part of the deployable runtime.
