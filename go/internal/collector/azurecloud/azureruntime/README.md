# internal/collector/azurecloud/azureruntime

Fixture-driven runtime wiring for the Azure cloud collector. This package turns
the parent `azurecloud` fact engine into a `collector.Source` that the shared
`collector.Service` can poll and commit.

## What this package owns

- `Config` / `TargetConfig`: declarative collector instance and bounded Azure
  scope shards (tenant, subscription, or management group), with credentials
  referenced by **name only** (`CredentialRef`).
- `Source`: implements `collector.Source.Next`, yielding one
  `collector.CollectedGeneration` per configured target. It builds the
  `azurecloud.Boundary`, derives durable scope and generation identity, reads
  Resource Graph pages through the `PageProvider` seam (with `$skipToken`
  resume), and delegates emission to `azurecloud.Collector`. The default source
  lane is `resource_graph`; `resource_changes` is an explicit fixture-only lane
  that emits provenance-only `azure_resource_change` facts.
- `PageProviderFactory`: the single seam that keeps the live Resource Graph/ARM
  client out of the runtime and tests.
- `FixturePageProvider`: an in-memory and file-backed provider used by tests and
  offline tooling for Resource Graph inventory and fixture `resourcechanges`
  pages. It issues no network calls.
- `LiveProviderFactory`: an inert documented production seam that returns
  `ErrLiveProviderGated`. It is never the default and never calls Azure.

## What this package does NOT own

Pagination, normalization, ARM identity, redaction, fact-envelope construction,
and bounded telemetry live in the parent `azurecloud` package. Durable commit
lives in `collector.Service` + the ingestion store. This package also does not
own reducer admission, graph writes, API/MCP readback, claim-driven workflow
scheduling, Helm/chart wiring, or live Azure transport activation. Existing
reducer slices already admit Azure resource, tag, image-reference, and
managed-relationship evidence outside this package; identity, change, DNS, live
source lanes, readback expansion, and chart wiring remain gated follow-ups for
issue #1998.

## Scope and generation

Each target maps to a stable scope ID
`azure:<tenant>:<scope_kind>:<provider_scope>:<resource_family>:<location>:<source_lane>`
(empty narrowing buckets collapse to `all`). Generation identity is a
deterministic fingerprint of the scope ID and observation time, so a replayed
sweep at the same instant converges to the same fact IDs (idempotent
re-emission). Partial subscription or management-group access is surfaced as an
explicit `azure_collection_warning` fact, never silent success.

## Live-call safety

No code path in this package or its tests calls Azure. The live client is a
documented seam (`LiveProviderFactory` returns `ErrLiveProviderGated`); a real
read-only adapter is a separate gated PR with its own credential, quota,
throttle, and fixture proof.

## Verify

```bash
cd go && go build ./...
cd go && go test ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/... ./internal/facts/... -count=1
cd go && golangci-lint run ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/...
```

## Evidence

Collector Performance Evidence: This is fixture-only scaffolding that adds a new
`collector.Source` and runtime binary; it changes no existing hot path.
Baseline: before this PR there was no Azure runtime path, so the prior Azure
ingestion throughput is zero generations/sec. After: the Azure `Source` yields
one bounded generation per configured scope target by streaming already-parsed
Resource Graph pages through the existing `azurecloud.Collector`, which is
unchanged. Backend/version: no graph or Postgres write path is added by this
package; commit goes through the existing `collector.Service` +
`postgres.IngestionStore` seam exercised by the AWS and OCI collectors. Input
shape: bounded fixture pages (2 pages, 3 resource rows) keyed by `$skipToken`;
production input is bounded per-scope shards within the collector lease and
Resource Graph quota budget. Terminal counts: fixture sweep commits 1 generation
with 2-3 resource facts plus 0-1 warning facts; the page loop is bounded by
`azurecloud.maxResourceGraphPages` (1000) so a malformed continuation cannot
loop forever. Telemetry: per-target `collector.azure.scope_scan` span plus the
parent package's bounded-label Azure metrics
(`eshu_dp_azure_*`); no per-target goroutine fan-out, no new lock, no new queue.
The resource-change lane reuses the same page bound and emits only source facts
from fixture pages with a configured redaction key. Why safe: the runtime is
single-pass over a fixed target slice, holds no shared mutable state across
targets, and adds no concurrency primitive; the only provider is fixture-backed
under test and gated in production.

Collector Observability Evidence: The runtime emits a bounded per-target span
`collector.azure.scope_scan` (labels: collector kind, scope kind, source lane —
all bounded enums) and a structured `azure scope scan completed` log carrying
scope_id, generation_id, scope_kind, source_lane, resource/change/warning/page
and resume counts, partial-scope and truncation flags, and duration. Fact emission and
partial-scope counters reuse the parent `azurecloud` bounded-label instruments
(`eshu_dp_azure_api_calls_total`, `eshu_dp_azure_skip_token_resumes_total`,
`eshu_dp_azure_partial_scope_total`, `eshu_dp_azure_facts_emitted_total`). No
metric label, span attribute, or log key carries an ARM ID, subscription or
tenant ID, resource group or resource name, location, tag, KQL text, URL, or
credential name. No shared-registry telemetry series is added in this slice.

No-Observability-Change: no telemetry contract file changes are needed. The
runtime reuses the existing `collector.azure.scope_scan` span, structured scan
completion log, and parent `eshu_dp_azure_*` metric family while adding only
bounded enum values for the resource-change lane. The command-level offline
fixture factory chooses either Resource Graph inventory parsing or
`resourcechanges` parsing from the declared target source lane and never changes
the default gated live provider.

Collector Deployment Evidence: no Docker Compose service, Helm Deployment,
Service, ServiceMonitor, port, chart value, runtime profile, or live Azure
credential path changes in this slice. `LiveProviderFactory` remains inert and
fixture providers are the only resource-change source.
