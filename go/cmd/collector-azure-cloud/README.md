# cmd/collector-azure-cloud

Runtime binary for the Azure cloud collector. It runs in two modes:

- **fixture** (default): wires the `azureruntime.Source` into the shared
  `collector.Service` and commits Azure source facts through the Postgres
  ingestion store. The live seam stays gated (issue #1998 scaffolding).
- **claimed-live** (`-mode claimed-live`): selects one enabled, claim-enabled
  Azure collector instance, wires the read-only live Resource Graph adapter, and
  runs through `collector.ClaimedService` so claim acquire, heartbeat, fenced
  commit, retry, and terminal failure follow the shared workflow lifecycle. This
  is the live-transport promotion path tracked by issue #3024 (the Azure
  equivalent of GCP #1997). It is opt-in and off by default.

## Configuration (declarative, credentials by name only)

| Env var | Required | Meaning |
| --- | --- | --- |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | yes | Collector instance that owns target policy and the credential environment. |
| `ESHU_AZURE_TARGETS_JSON` | yes | JSON array of bounded Azure scope shards. |
| `ESHU_AZURE_POLL_INTERVAL` | no | Sweep cadence (default 5m). |
| `ESHU_AZURE_FIXTURE_PAGES_JSON` | no | File-backed offline Resource Graph or `resourcechanges` page provider for local proof/smoke. A configured fixture list must match one `source_lane`; mixed-lane offline runs are rejected. Unset selects the gated live seam. |
| `ESHU_AZURE_REDACTION_KEY_FILE` | no | Read-only key-material file used to fingerprint tag values, managed identity GUIDs, and resource-change actors. Required by `source_lane=resource_changes`. |

Each target object:

```json
{
  "tenant_id": "tenant-abc",
  "scope_kind": "subscription",
  "provider_scope_id": "11111111-1111-1111-1111-111111111111",
  "resource_type_family": "microsoft.compute",
  "location_bucket": "eastus",
  "credential_ref": "azure-read-only-spn",
  "source_lane": "resource_graph",
  "fencing_token": 7
}
```

`credential_ref` is a **name**, never a secret value. `scope_kind` is one of
`subscription`, `management_group`, or `tenant`. `source_lane` defaults to
`resource_graph`; `resource_changes` is fixture-only in this slice and emits
provenance facts only. Because `ESHU_AZURE_FIXTURE_PAGES_JSON` carries one
ordered fixture page list, all targets in an offline fixture run must use the
same `source_lane`.

## Claimed-live mode (`-mode claimed-live`)

Claimed-live mode reads its instance from the reconciled
`ESHU_COLLECTOR_INSTANCES_JSON` document instead of `ESHU_AZURE_TARGETS_JSON`,
and requires a read-only redaction key file.

| Env var | Required | Meaning |
| --- | --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | yes | Reconciled desired collector instances. The runner selects the enabled, claim-enabled `azure` instance. |
| `ESHU_AZURE_COLLECTOR_INSTANCE_ID` | when >1 azure instance | Disambiguates which `azure` instance to run. |
| `ESHU_AZURE_POLL_INTERVAL` | no | Claim poll cadence (default 5m). |
| `ESHU_AZURE_COLLECTOR_CLAIM_LEASE_TTL` | no | Claim lease TTL (default from workflow). |
| `ESHU_AZURE_COLLECTOR_HEARTBEAT_INTERVAL` | no | Lease heartbeat interval; must be less than the lease TTL. |
| `ESHU_AZURE_COLLECTOR_OWNER_ID` | no | Durable claim owner id (defaults to `HOSTNAME`). |
| `-redaction-key-file` flag | yes | Read-only redaction key material file; a blank file is rejected. |

The instance `configuration` must set `live_collection_enabled: true` and carry
one or more enabled `scopes` that share a single `credential_ref`:

```json
{
  "live_collection_enabled": true,
  "scopes": [{
    "enabled": true,
    "tenant_id": "tenant-abc",
    "scope_kind": "subscription",
    "provider_scope_id": "11111111-1111-1111-1111-111111111111",
    "resource_type_family": "microsoft.compute",
    "location_bucket": "eastus",
    "credential_ref": "azure-read-only-spn"
  }]
}
```

The coordinator-assigned generation id and fencing token come from each claimed
work item; configured scopes carry no fencing token. The live credential is
resolved from the ambient Azure workload identity, never from configuration.
Claimed-live wires the live Resource Graph provider, which serves the
`resource_graph` lane only; a scope declaring `resource_changes` or
`arm_fallback` is rejected at startup.

## Live-call safety

In fixture mode with `ESHU_AZURE_FIXTURE_PAGES_JSON` unset, the binary selects
the zero-value `azureruntime.LiveProviderFactory`, which returns
`ErrLiveProviderGated`. No default code path and no test issues a live Azure
request. Live transport is reached only in `-mode claimed-live`, which is opt-in
and requires an explicit `live_collection_enabled=true` collector instance and a
granted workflow claim before any read. Helm chart activation of the claimed-live
deployment remains gated until the runtime and security gates pass.

## Ownership boundary

This command owns process startup, environment parsing, mode/provider-seam
selection, shared telemetry bootstrap, `collector.Service` (fixture) and
`collector.ClaimedService` (claimed-live) construction, and the claim-status
committer for the Azure collector source. It does not own fact normalization,
reducer admission, graph writes, API/MCP readback, Helm values, or live-smoke
proof.

Azure resource, tag, image-reference, and managed-relationship reducer slices
are implemented in their owning packages and stay outside this binary. Helm
chart activation, the hosted security posture gate, and live-smoke proof remain
issue #3024 follow-ups gated by the Azure cloud collector contract; this PR
delivers the claimed-live runtime and its fixture-proven claim handoff, and
keeps live transport off by default.

## Verify

```bash
cd go && go build ./...
cd go && go test ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/... ./internal/workflow/ -count=1
cd go && golangci-lint run ./cmd/collector-azure-cloud/... ./internal/collector/azurecloud/...
scripts/verify-package-docs.sh
scripts/verify-performance-evidence.sh
```

## Evidence

No-Regression Evidence (claimed-live): The claimed-live path reuses the shared
`collector.ClaimedService` runner already exercised by `collector-aws-cloud` and
`collector-gcp-cloud`; it adds no new graph or Postgres hot path. Conflict
domain: one durable workflow claim per `(azure instance, scope_id, generation)`;
fencing and lease handoff are owned by `collector.ClaimedService` and the
Postgres workflow control store, unchanged. The source emits one bounded
generation per claimed work item (page loop bounded by
`azurecloud.maxResourceGraphPages`). Fixture-proven claim handoff: matching work
item collects facts with the claim's generation id and fencing token;
unauthorized scope, mismatched instance, wrong collector kind, non-claimed
status, non-positive fencing token, and generation/run mismatch are each
rejected before any provider call (claimed_source_test.go). The bounded
`eshu_dp_azure_claims_total` counter records committed/failed claim outcomes.

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
the command uses the zero-value gated live seam.

Observability Evidence: The binary boots shared telemetry (tracer, meter,
logger, pprof, Prometheus handler, status server) identically to the AWS and OCI
collector binaries. Per-target scans emit the bounded `collector.azure.scope_scan`
span and a structured `azure scope scan completed` log (scope_kind, source_lane,
bounded counts, partial/truncated flags, duration).
Azure fact and partial-scope counters reuse the parent package's bounded-label
instruments. No span attribute, metric label, or log key carries an ARM ID,
subscription/tenant ID, resource group/resource name, location, tag, KQL text,
URL, or credential name. No shared-registry telemetry series is added in this
slice.
