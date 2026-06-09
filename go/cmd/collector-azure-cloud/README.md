# cmd/collector-azure-cloud

Fixture-driven runtime binary for the Azure cloud collector. It wires the
`azureruntime.Source` into the shared `collector.Service` and commits Azure
source facts through the Postgres ingestion store. This is the runtime
scaffolding slice of issue #1998.

## Configuration (declarative, credentials by name only)

| Env var | Required | Meaning |
| --- | --- | --- |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | yes | Collector instance that owns target policy and the credential environment. |
| `ESHU_AZURE_TARGETS_JSON` | yes | JSON array of bounded Azure scope shards. |
| `ESHU_AZURE_POLL_INTERVAL` | no | Sweep cadence (default 5m). |
| `ESHU_AZURE_FIXTURE_PAGES_JSON` | no | File-backed offline page provider for local proof/smoke. Unset selects the gated live seam. |

Each target object:

```json
{
  "tenant_id": "tenant-abc",
  "scope_kind": "subscription",
  "provider_scope_id": "11111111-1111-1111-1111-111111111111",
  "resource_type_family": "microsoft.compute",
  "location_bucket": "eastus",
  "credential_ref": "azure-read-only-spn",
  "fencing_token": 7
}
```

`credential_ref` is a **name**, never a secret value. `scope_kind` is one of
`subscription`, `management_group`, or `tenant`.

## Live-call safety

With `ESHU_AZURE_FIXTURE_PAGES_JSON` unset, the binary selects
`azureruntime.LiveProviderFactory`, which is inert and returns
`ErrLiveProviderGated`. No default code path and no test issues a live Azure
request. A real read-only Resource Graph/ARM adapter is a separate gated PR.

## Deferred (NOT in this slice)

Reducer admission of `azure_cloud_resource`, the remaining Azure fact families
(`azure_tag_observation`, identity/policy, relationships, DNS, image refs),
API/MCP readback truth labels, claim-driven workflow scheduling, and
Helm/env/chart wiring. Those follow the Azure cloud collector contract gate.

## Verify

```bash
cd go && go build ./...
cd go && go test ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/... -count=1
cd go && golangci-lint run ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/...
```

## Evidence

No-Regression Evidence: New runtime scaffolding binary; it adds no new graph or
Postgres hot path. Baseline: no Azure collector binary existed before this PR
(zero Azure generations committed). After: the binary commits one bounded
generation per configured scope target through the same `collector.Service` +
`postgres.IngestionStore` commit seam already used by `collector-aws-cloud` and
`collector-oci-registry`; the commit path is unchanged. Backend/version: shared
ingestion store, no new DDL, no new Cypher. Input shape: bounded fixture pages
(2 pages / 3 rows) for the smoke test; production input is bounded per-scope
shards within the lease and Resource Graph quota budget. Terminal counts: smoke
run yields 1 generation with 3 resource facts and 0 warnings; the page loop is
bounded by `azurecloud.maxResourceGraphPages` (1000). Telemetry: per-target
`collector.azure.scope_scan` span and the parent package's bounded-label
`eshu_dp_azure_*` metrics; the binary adds no goroutine fan-out, lock, or queue.
Why safe: single-pass over a fixed target slice with the fixture/gated provider;
the live seam is inert.

Observability Evidence: The binary boots shared telemetry (tracer, meter,
logger, pprof, Prometheus handler, status server) identically to the AWS and OCI
collector binaries. Per-target scans emit the bounded `collector.azure.scope_scan`
span and a structured `azure scope scan completed` log (scope_id, generation_id,
scope_kind, source_lane, bounded counts, partial/truncated flags, duration).
Azure fact and partial-scope counters reuse the parent package's bounded-label
instruments. No span attribute, metric label, or log key carries an ARM ID,
subscription/tenant ID, resource group/resource name, location, tag, KQL text,
URL, or credential name. No shared-registry telemetry series is added in this
slice.
