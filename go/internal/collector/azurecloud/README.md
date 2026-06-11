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
- `azure_cloud_resource` and `azure_collection_warning` are always emitted.
  `azure_tag_observation` is emitted **only when a redaction key is configured**:
  `Collector` takes an optional `WithRedactionKey`, and `emitPageResources`
  pairs each tagged resource with one tag-evidence fact whose values are keyed
  fingerprints (`FingerprintTagValues`); without a key, tag values are never
  fingerprinted or carried and no tag observation fact is emitted. The runtime
  source threads the key from `azureruntime.Source.RedactionKey`, loaded by the
  `collector-azure-cloud` binary from `ESHU_AZURE_REDACTION_KEY_FILE` (a blank
  or unreadable configured file fails closed). The remaining contract fact kinds
  (`azure_cloud_relationship`, `azure_identity_observation`,
  `azure_resource_change`, `azure_dns_record`, `azure_image_reference`) have
  registered constants and schema versions in `internal/facts/azure.go` but no
  envelope builders yet. Reducer admission of tag evidence is a further follow-up
  (#2192).

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

## Performance and Observability Evidence

No-Regression Evidence: this slice adds a new, isolated fixture-driven parsing
package and changes no existing hot path. Baseline: no Azure collector existed;
after: bounded in-memory normalization of Azure Resource Graph pages with no
Cypher, no graph or Postgres writes, no worker/lease/queue, and no claim-driven
runtime binary. Backend/version: none touched (NornicDB/Neo4j, Postgres, and the
reducer are unchanged; fact kinds are additive). Input shape: bounded Resource
Graph fixture pages resumed by skip-token; work is O(resources x pages)
single-pass, so terminal output is one bounded generation of
`azure_cloud_resource`/`azure_collection_warning` facts (row count equals deduped
fixture resources plus one warning per unsupported kind/scope). Why safe: no live
calls in tests, stale generations are rejected by fencing token, and re-emission
of the same generation is idempotent, all proven by fixture tests.

Observability Evidence: the package exports bounded-label data-plane metrics
`eshu_dp_azure_api_calls_total`, `_skip_token_resumes_total`,
`_partial_scope_total`, `_facts_emitted_total`, and `_freshness_lag_seconds`.
Labels are bounded enums only (collector kind, scope kind, source lane,
operation, status class, fact kind, outcome); a test asserts no ARM resource id,
subscription id, tenant id, resource name, tag, or URL appears in any label. An
operator reads partial-scope coverage, skip-token resumes, freshness lag, and
fact counts to answer whether a scan is complete, fresh, throttled, or partial.
