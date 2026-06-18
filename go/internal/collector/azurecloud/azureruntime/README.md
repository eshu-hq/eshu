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
- `PageProviderFactory`: the single seam that keeps live Resource Graph and ARM
  fallback access injectable and non-default.
- `FixturePageProvider`: an in-memory and file-backed provider used by tests and
  offline tooling for Resource Graph inventory and fixture `resourcechanges`
  pages. It issues no network calls.
- `LiveProviderFactory`: a gated-by-default production seam. Its zero value
  returns `ErrLiveProviderGated`; only explicitly injected read-only Resource
  Graph and allowlisted ARM fallback clients can make live calls. The command
  and chart paths still do not activate it.

## What this package does NOT own

Pagination, normalization, ARM identity, redaction, fact-envelope construction,
and bounded telemetry live in the parent `azurecloud` package. Durable commit
lives in `collector.Service`/`collector.ClaimedService` + the ingestion store.
This package implements the `collector.ClaimedSource` (`NextClaimed`) that
resolves an already-claimed work item to its authorized scope target, but it
does not own coordinator-side claim scheduling, reducer admission, graph writes,
API/MCP readback, Helm/chart wiring, or live-smoke proof. Existing reducer slices
already admit Azure resource, tag, image-reference, and managed-relationship
evidence outside this package; identity, change, DNS, live source lanes, readback
expansion, Helm chart activation, and live-smoke proof remain gated follow-ups
(issue #3024 tracks the live-transport promotion).

## Scope and generation

Each target maps to a stable scope ID
`azure:<tenant>:<scope_kind>:<provider_scope>:<resource_family>:<location>:<source_lane>`
(empty narrowing buckets collapse to `all`). Generation identity is a
deterministic fingerprint of the scope ID and observation time, so a replayed
sweep at the same instant converges to the same fact IDs (idempotent
re-emission). Partial subscription or management-group access is surfaced as an
explicit `azure_collection_warning` fact, never silent success.

## Live-call safety

No default command, fixture, or test path calls Azure. The zero-value live seam
(`LiveProviderFactory{}`) still returns `ErrLiveProviderGated`; live transport is
reachable only when operator-owned wiring explicitly injects a read-only
`LiveResourceGraphClient` or the SDK-backed `AzureSDKResourceGraphClient`.
Optional ARM fallback enrichment requires a separate injected
`LiveARMFallbackClient`, at least one exact resource-type `LiveARMFallbackRule`,
a fixed API version, and an extension-field allowlist. The SDK fallback wrapper
exposes only `armresources.Client.GetByID`; it does not expose provider
registration, create, update, or delete operations.

The adapter bounds Resource Graph `Resources` calls with explicit KQL, `$top`,
`$skipToken`, per-call timeout, retry cap, backoff cap, and a per-row live
payload size cap. The owned default query avoids the full ARM `properties` bag;
overridden queries still fail closed on oversized rows. ARM fallback payloads
are selected by field allowlist, wrapped with their own schema version,
byte-bounded before attachment, and then redacted by the parent
`azure_cloud_resource` envelope builder before persistence. Throttling,
permission-hidden scopes, unsupported families, fallback skips, oversized
fallback payloads, and expired auth or continuation tokens surface through
`ScopeAccess` so the parent collector emits `azure_collection_warning` evidence
instead of silent empty success. Credential references stay names only and are
not copied into requests, facts, logs, spans, or metric labels.

## Verify

```bash
cd go && go build ./...
cd go && go test ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/... ./internal/facts/... -count=1
cd go && golangci-lint run ./internal/collector/azurecloud/... ./cmd/collector-azure-cloud/...
```

## Evidence

Collector Performance Evidence: The runtime source keeps the command default
gated and adds injected live Resource Graph and allowlisted ARM fallback
providers behind the existing `PageProviderFactory`; it changes no existing
graph, reducer, queue, or Postgres hot path.
Baseline: before this PR there was no Azure runtime path, so the prior Azure
ingestion throughput is zero live generations/sec. After: the Azure `Source`
yields one bounded generation per configured scope target by streaming fixture or
explicitly injected Resource Graph pages through the existing
`azurecloud.Collector`, which still owns fact emission. ARM fallback enrichment
does not introduce a separate commit path; it only augments Resource Graph row
extension data before the existing envelope redaction. Backend/version: no graph
or Postgres write path is added by this package; commit goes through the
existing `collector.Service` +
`postgres.IngestionStore` seam exercised by the AWS and OCI collectors. Input
shape: bounded fixture pages (2 pages, 3 resource rows) keyed by `$skipToken`;
production input is bounded per-scope shards within the collector lease and
Resource Graph quota budget with live page size capped at 1000, retry count
capped, per-call timeout enforced, and optional ARM fallback calls limited to
one read-only GET per allowlisted Resource Graph row. Terminal counts: fixture
sweep commits 1
generation with 2-3 resource facts plus 0-1 warning facts; the page loop is
bounded by `azurecloud.maxResourceGraphPages` (1000) so a malformed continuation
cannot loop forever. Telemetry: per-target `collector.azure.scope_scan` span plus
the parent package's bounded-label Azure metrics
(`eshu_dp_azure_*`); no per-target goroutine fan-out, no new lock, no new queue.
The resource-change lane reuses the same page bound and emits only source facts
from fixture pages with a configured redaction key. Why safe: the runtime is
single-pass over a fixed target slice, holds only per-provider warning state, and
adds no concurrency primitive; live calls require explicit injected client
wiring and are not command- or chart-enabled.

No-Regression Evidence: warning-priority merging for ARM fallback stays inside
the existing in-memory `ScopeAccess` aggregation. Baseline: a mixed fixture page
with one skipped, non-allowlisted resource row followed by one allowlisted row
whose fallback failed emitted 1 resource warning with
`warning_kind=fallback_skipped`, hiding the actionable fallback failure. After:
the same 2-row fixture page emits 1 resource warning with throttled, stale,
permission-hidden, or redaction reason when that fallback failure occurs; a
successful allowlisted fallback still leaves skip-only evidence visible. Backend
and input shape: no graph, Postgres, queue, worker, lease, or live-provider
activation changes; the proof uses the injected mock Resource Graph and ARM
fallback clients already used by the package. Terminal row counts stay 2
resource facts plus 1 warning fact, and the fallback client is called only for
the allowlisted row.

Collector Observability Evidence: The runtime emits a bounded per-target span
`collector.azure.scope_scan` (labels: collector kind, scope kind, source lane —
all bounded enums) and a structured `azure scope scan completed` log carrying
scope_kind, source_lane, resource/change/warning/page and resume counts,
partial-scope and truncation flags, and duration. Fact emission and
partial-scope counters reuse the parent `azurecloud` bounded-label instruments
(`eshu_dp_azure_api_calls_total`, `eshu_dp_azure_skip_token_resumes_total`,
`eshu_dp_azure_partial_scope_total`, `eshu_dp_azure_facts_emitted_total`). No
metric label, span attribute, or log key carries an ARM ID, subscription or
tenant ID, resource group or resource name, location, tag, KQL text, URL, or
credential name. No shared-registry telemetry series is added in this slice.

No-Observability-Change: no telemetry contract file changes are needed. The
runtime reuses the existing `collector.azure.scope_scan` span, structured scan
completion log, and parent `eshu_dp_azure_*` metric family. Live Resource Graph
and ARM fallback warning conditions are emitted as existing
`azure_collection_warning` facts via `ScopeAccess`; no new metric labels, span
attributes, status fields, or shared-registry telemetry series are added. The
command-level offline fixture
factory chooses either Resource Graph inventory parsing or `resourcechanges`
parsing from the declared target source lane and never changes the default gated
live provider. No-Observability-Change: the warning-priority fix changes only
which existing sanitized `azure_collection_warning` reason wins when mixed ARM
fallback outcomes occur in one scan; it adds no metric label, span attribute,
status field, log field, or provider identifier.

Collector Deployment Evidence: no Docker Compose service, Helm Deployment,
Service, ServiceMonitor, port, chart value, runtime profile, or live Azure
credential path changes in this slice. `LiveProviderFactory{}` remains inert;
SDK-backed live transport is available only by explicit in-process injection and
fixture providers remain the only resource-change source.
