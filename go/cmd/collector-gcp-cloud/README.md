# cmd/collector-gcp-cloud

Fixture-driven runtime scaffolding for the GCP Cloud Asset Inventory collector.
The binary wires the `gcpruntime.Source` into the shared hosted collector service
and commits collected generations through the Postgres ingestion store.

## What this binary does

1. Parses `-config` (declarative JSON) and `-redaction-key-file` (read-only key
   material) flags. Both are file paths; this slice mints no `ESHU_*` runtime
   contract.
2. Builds a `gcpruntime.Source` backed by an offline `FixturePageProvider` that
   serves Cloud Asset Inventory pages from the local fixture files named in the
   config.
3. Commits each generation through `postgres.NewIngestionStore`, wrapped by
   `gcpStatusCommitter`, which records the bounded GCP claim metric on commit.
4. Serves shared health, status, pprof, and Prometheus endpoints like the other
   hosted collectors.

## Why the command is fixture-driven only

The GCP cloud collector contract forbids claiming runtime readiness before the
reducer and chart paths exist. This binary is the runtime SCAFFOLDING slice: it
proves the source, committer, and config wiring with fixtures and performs **no
live Google Cloud call**. The live Cloud Asset Inventory transport
(`gcpruntime.LiveClient`) is implemented as an explicit-injection `PageProvider`,
but this command does not wire it by default or resolve live credentials.

## Declarative config

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

## Deferred

Helm values, environment-variable contracts, command wiring for live Cloud Asset
Inventory transport, credential resolution, claim-enabled scheduler activation,
direct/effective tag APIs, and live smoke proof are deferred slices. Shared GCP
reducer admission, explicit-injection live transport, and API/MCP readback are
implemented outside this command and do not make this binary a live provider
collector. See
`docs/public/reference/gcp-cloud-collector-contract.md`.

## Verification and evidence

Run `go test ./cmd/collector-gcp-cloud/... ./internal/collector/gcpcloud/...`.
The collector runtime no-regression and observability evidence is recorded in
`docs/public/reference/gcp-cloud-collector-contract.md` under "Runtime
Scaffolding Evidence".
