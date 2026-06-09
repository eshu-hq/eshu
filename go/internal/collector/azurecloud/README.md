# internal/collector/azurecloud

First fixture-testable slice of the Azure cloud collector. It turns Azure
Resource Graph `Resources` API response pages into provider-specific
`azure_cloud_resource` source facts and explicit `azure_collection_warning`
evidence, for one bounded subscription, management-group, or tenant shard.

This package is the Azure sibling of `internal/collector/awscloud`. It follows
the [Azure Cloud Collector Contract](../../../../docs/public/reference/azure-cloud-collector-contract.md)
and the [Multi-Cloud Runtime Collector Contract](../../../../docs/public/reference/multi-cloud-collector-contract.md).

## What this slice does

- Parses Resource Graph pages (`ParseResourceGraphPage`): `totalRecords`,
  `count`, `resultTruncated`, `$skipToken`, and resource rows.
- Normalizes raw ARM resource IDs into identity fields (`ParseARMIdentity`):
  subscription, resource group, provider namespace, fully-qualified resource
  type (including nested types), resource name, and a lowercased normalized ID
  used for stable keys.
- Redacts the provider extension object (`RedactExtension`): keys matching the
  secret or data-plane token policy are dropped (not masked) and recorded in
  `redacted_keys`. The redaction policy version is persisted on every fact.
- Emits facts (`NewResourceEnvelope`, `NewWarningEnvelope`) with the scope and
  generation contract fields: `collector_kind=azure`, collector instance,
  tenant, scope kind, provider scope, source lane, generation, fencing token,
  a deterministic stable fact key, source ref, `source_confidence=reported`,
  and provider/observed timestamps.
- Drives a bounded scan (`Collector.Collect`): walks `$skipToken` pages,
  emits one resource fact per row, emits truncation and partial-scope warnings
  as explicit evidence, and records bounded telemetry.

## What this slice does NOT do (documented follow-ups)

- No live Azure Resource Graph or ARM calls. The `PageProvider` seam is fed by
  fixtures under `testdata/`. A live ARM client adapter is a follow-up.
- No `collector.Source`/`ClaimedService` runtime wiring, no `cmd/` binary, no
  Helm values, environment variables, or runtime profiles. The contract forbids
  claiming Azure runtime before it is implemented and fixture-proven.
- No reducer admission, no `cloud_resource_uid` resolution, no graph projection,
  no API/MCP readback. Those are reducer-owned follow-ups.
- Only `azure_cloud_resource` and `azure_collection_warning` are emitted. The
  remaining contract fact kinds (`azure_cloud_relationship`,
  `azure_tag_observation`, `azure_identity_observation`,
  `azure_resource_change`, `azure_dns_record`, `azure_image_reference`) have
  registered constants and schema versions in `internal/facts/azure.go` but no
  envelope builders yet.

## Invariants

- Raw ARM resource IDs are preserved; normalized fields are additive.
- Extension payloads never carry deployment templates, secrets, Key Vault
  values, connection strings, access keys, tokens, IP addresses, private
  endpoint hostnames, or provider response bodies.
- Stable fact keys derive from normalized identity, resource type, source lane,
  and tenant only, so extension or tag churn does not split idempotent
  re-emission of a generation.
- Partial scope, permission-hidden subscriptions, and truncation are explicit
  warning evidence, never silent success.
- Telemetry labels are bounded enums only.

## Fixture matrix covered

`collector_test.go`, `envelope_test.go`, `redaction_test.go`,
`resourcegraph_test.go`, `armid_test.go`, and `metrics_test.go` cover:
skip-token pagination resume, idempotent re-emission of the same generation,
stale/invalid generation rejection, truncation warning, partial-scope and
permission-hidden warning accounting, malformed-row unsupported warning,
extension redaction (including nested maps), provider-read-error propagation,
and bounded telemetry-label safety.

## Tests

```bash
cd go && go test ./internal/collector/azurecloud/... ./internal/facts/ -count=1
```
