# cmd/collector-gcp-cloud

Runtime command for the GCP Cloud Asset Inventory collector. The default mode is
fixture-backed and inert. The opt-in claimed-live mode consumes workflow claims,
wires `gcpruntime.LiveClient`, and commits claimed generations through the
Postgres ingestion store.

## What this binary does

1. Parses `-mode` and `-redaction-key-file`. `-mode fixture` is the default and
   requires `-config`; `-mode claimed-live` reads workflow collector instance
   configuration from environment.
2. In fixture mode, builds a `gcpruntime.Source` backed by an offline
   `FixturePageProvider` that serves Cloud Asset Inventory pages from local
   fixture files named in the config.
3. In claimed-live mode, selects one enabled claim-capable `gcp` collector
   instance, requires `live_collection_enabled=true`, constructs the explicit
   `gcpruntime.LiveClient` with ADC, and lets `collector.ClaimedService` own
   claim acquire, heartbeat, fenced commit, retry, and terminal failure
   handling.
4. Commits each generation through `postgres.NewIngestionStore`, wrapped by
   `gcpStatusCommitter`, which records the bounded GCP claim metric on commit.
5. Serves shared health, status, pprof, and Prometheus endpoints like the other
   hosted collectors.

## Default-off live mode

The command performs no live Google Cloud call unless the operator explicitly
starts `-mode claimed-live`, provides a read-only redaction key file, and
configures a claim-enabled GCP collector instance with
`live_collection_enabled=true`. Credential references are names only. The command
uses ADC for token acquisition and never accepts credential material in flags,
collector instance JSON, logs, spans, metrics, facts, or status rows.

## Declarative config

Fixture mode uses the file config below:

```json
{
  "collector_instance_id": "gcp-instance-1",
  "poll_interval": "30m",
  "scopes": [
    {
      "parent_scope_kind": "project",
      "parent_scope_id": "my-project",
      "asset_type_family": "mixed",
      "content_family": "resource",
      "location_bucket": "global",
      "fencing_token": 7,
      "credential_ref": "gcp-readonly-sa",
      "page_files": ["testdata/assets_list_page1.json"]
    }
  ]
}
```

`credential_ref` is a credential **name** only; no secret material is stored in
the config. The redaction key is read from the `-redaction-key-file` path and is
never logged.

Claimed-live mode uses:

| Environment variable | Purpose |
| --- | --- |
| `ESHU_COLLECTOR_INSTANCES_JSON` | Desired collector instances with one enabled, claim-capable `gcp` instance. |
| `ESHU_GCP_COLLECTOR_INSTANCE_ID` | Optional selector when more than one GCP instance is present. |
| `ESHU_GCP_COLLECTOR_OWNER_ID` | Optional stable owner id; defaults to `HOSTNAME` or the command name. |
| `ESHU_GCP_COLLECTOR_POLL_INTERVAL` | Idle poll interval for claim acquisition. |
| `ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL` | Workflow claim lease duration. |
| `ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL` | Claim heartbeat cadence; must be shorter than the lease TTL. |

The collector instance `configuration` must include
`live_collection_enabled=true` and at least one enabled scope. Scope entries
carry parent scope kind/id, asset/content/location shard fields, and
`credential_ref`. Set `direct_tags_enabled` or `effective_tags_enabled` per
scope to opt into Resource Manager tag evidence. Generation id and fencing token
come from the workflow work item, not static config.

## Deferred

Sanitized live smoke proof is deferred. Shared GCP reducer admission and API/MCP
readback are implemented outside this command. See
`docs/public/reference/gcp-cloud-collector-contract.md`.

## Verification and evidence

Run `go test ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/...`.
The collector runtime no-regression and observability evidence is recorded in
`docs/public/reference/gcp-cloud-collector-contract.md` under "Runtime
Scaffolding Evidence".
