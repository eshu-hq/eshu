# Security review — GCP/Azure live cloud collector

Pre-operator-activation and pre-promotion review for GCP issue #1997 and Azure
issue #3066. Azure's claimed-live runtime (#3050) and default-off Helm wiring
(#3059) are shipped. They let an operator request a credential-bearing run;
they do not supply the sanitized live evidence needed to promote Azure.

**Scope of this doc:** the credential-bearing **live transport** paths and the
security evidence for them. Fixture parsing, shared reducer admission
(`cloud_inventory_admission`), and API/MCP readback
(`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`) are shipped
and fixture-proven. This review does not replace the remaining #3066 live and
security evidence described in the Azure live-smoke status below, and it does
not replace the separate partition-filtered handler proof required for
promotion.

This review must pass before an operator enables any credential mount,
ServiceMonitor, or live smoke path. Shipped command and chart defaults remain
off until that opt-in.
Adapter code may merge only when default wiring stays inert and tests prove the
seam is explicitly injected, read-only, bounded, and sanitized. Today both
binaries use fixture/file-backed defaults that make **zero** live calls. The GCP
live seam exists as an explicit-injection `gcpruntime.LiveClient`. The Azure
zero-value `azureruntime.LiveProviderFactory{}` returns
`ErrLiveProviderGated`. Azure also has a separate opt-in
`collector-azure-cloud -mode claimed-live` path: it needs an enabled,
claim-enabled `azure` instance with `live_collection_enabled=true`, a workflow
claim, and an operator-provided credential. Azure RBAC is an operator
prerequisite, not a runtime-enforced check: the live smoke must prove that the
credential is read-only. The Helm chart exposes that path only when
`azureCloudCollector.enabled=true`; the chart default is off.

## 1. Credential surfaces

| Provider | Live seam | Auth model to verify | Least-privilege scope |
| --- | --- | --- | --- |
| GCP | `gcpruntime.LiveClient` (`go/internal/collector/gcpcloud/gcpruntime/liveclient.go`); `CredentialRef` is a name only. | Workload Identity Federation / ADC for a dedicated service account. No long-lived JSON keys mounted. | Cloud Asset Inventory read-only (`roles/cloudasset.viewer`) at the configured org/folder/project parent only. No `assets.export`, no IAM write, no data-plane reader roles. |
| Azure | `azureruntime.LiveProviderFactory` (`go/internal/collector/azurecloud/azureruntime/live_provider.go`). Its zero value is inert for fixture/default mode. The opt-in `collector-azure-cloud -mode claimed-live` path resolves the ambient credential and injects a Resource Graph client. ARM fallback is an unwired injectable seam, outside claimed-live and #3066 smoke scope. | Operator prerequisite: managed/workload identity with read-only Azure RBAC at the configured scope. The runtime does not verify the grant; the smoke must prove it. No client-secret string in env. | Expected operator grant: `Reader` at the configured subscription/management-group scope. Claimed-live serves Resource Graph reads only; it has no `Contributor`, provider-registration, write, or delete path. |

Threat-model checks: privilege creep (read-only inventory, not secret *values*);
credential *reference* vs material (names only — never bytes — in struct fields,
config, fact payloads, logs, spans, metric labels); confused-deputy / scope
escape (scope enforced by the credential's own grant, not a client-side filter);
default-deny (the inert zero-value seams stay the command/chart default; any path
constructing or wiring the live adapter without explicit operator opt-in is a
finding).

GCP security status: issues #1997 and #2644 are closed after the sanitized
live security smoke recorded below. `gcpruntime.LiveClient` is the shipped
explicit-injection REST `PageProvider` for Cloud Asset Inventory `assets.list`.
Fixture remains the `collector-gcp-cloud` CLI default, while the explicit
`-mode claimed-live` command path wires the live transport and credential
source. The chart keeps `gcpCloudCollector.enabled=false` by default and starts
claimed-live only after operator opt-in. The sanitized GCP smoke and security
evidence are recorded below. That security result does not promote the canonical
GCP claimed-live/full-promotion lane, which remains `gated` for its broader
readiness proof.

## 2. Redaction-key handling

The GCP live path uses shipped file-based key handling
(`loadRedactionKey` in `go/cmd/collector-gcp-cloud/main.go`). Azure
fixture/default mode may use `ESHU_AZURE_REDACTION_KEY_FILE`; claimed-live
requires `-redaction-key-file`, which the Helm deployment passes from the
read-only redaction mount. Both Azure inputs follow the same rules:

- Key material loaded from a `filepath.Clean`-ed **file**, never an env literal
  or config-JSON field; passed to `redact.NewKey`.
- A blank/whitespace key file is **rejected** so facts are never emitted with an
  unkeyed marker; a configured-but-unreadable file is a hard error.
- Key material never logged; never in spans, metrics, status, or payloads. Only
  the keyed *marker* (`redact.String(...).Marker`) is persisted.
- Every emitted fact stamps `redaction_policy_version`; the live path must emit
  **through** the redacting envelope builders that stamp it.
- Key-file mount is read-only with restrictive perms. Rotation semantics are
  defined for GCP: rotating the redaction key material MUST be accompanied by
  bumping the collector's `RedactionPolicyVersion` constant
  (`gcpcloud.RedactionPolicyVersion`,
  `go/internal/collector/gcpcloud/redaction.go`), because every emitted fact
  stamps `redaction_policy_version`. The version stamp is what distinguishes
  the pre- and post-rotation keyspaces, so fingerprints produced under
  different keys are never treated as the same identity, and the bump signals
  that pre-rotation facts must be re-fingerprinted (re-collected) to join the
  new keyspace. A key rotation without a version bump is unsupported: it would
  silently produce mismatched fingerprints for the same underlying identity
  across the rotation boundary.

## 3. Bounded-fetch / timeout / size invariants

- Pagination bounded and resume-only (GCP page token; Azure `$skipToken`); an
  expired/missing token degrades to a durable **partial warning**, never a
  silent truncation or infinite re-page loop.
- Per-call timeouts + context cancellation; the collector lease bounds the scan;
  no unbounded retries.
- Quota/throttle/backoff bounded and capped, surfaced as retryable warning
  evidence rather than hammering the provider.
- Response page size and per-resource payload size bounded; extension objects
  carry only safe bounded metadata.
- Azure ARM fallback is an unwired injectable seam. It is outside claimed-live
  and the #3066 operator smoke. Any future activation must use exact
  resource-type allowlist rules, fixed API versions, configured extension
  fields, one bounded `GetByID` per allowlisted Resource Graph row, and explicit
  redaction warning evidence for oversized payloads.
- **No write operations** reachable by construction (no export/register/deploy/
  delete/mutate).

## 4. What data leaves the boundary

- **Preserved (source evidence):** raw provider locators (CAI full resource name,
  ARM resource ID), normalized identity, asset/resource type, source timestamps,
  bounded labels/tags within retention, redaction policy version, versioned
  extension object.
- **Fingerprinted (keyed marker only):** sensitive label/tag values, IAM/identity
  principals, policy condition presence, etag.
- **Never persisted:** raw IAM policy JSON, secret/Key Vault values, object
  contents, startup scripts, env-var values, connection strings, access keys,
  tokens, DB contents, request/response bodies, public/private IPs, private
  endpoint hostnames, ARM deployment templates, KQL/query text.
- **Metric/label/status boundary:** labels are bounded enums only. Grep the live
  path for any resource ID, name, project/subscription ID, URL, tag value, IAM
  member, or credential name leaking into a label, span attribute, log field, or
  status key.

## 5. Partial-access evidence states

The live path must **produce, not swallow**, explicit incompleteness:
`gcp_collection_warning` / `azure_collection_warning` for partial permission,
hidden resources, quota/backoff, throttle, truncation, token expiry, unsupported
tier, and redaction outcomes; `ScopeAccess` reports partial scope. Per-path truth
labels (`partial`/`stale`/`unavailable`/`unsupported`) must not be converted into
silent fallback truth at read time. Delete/change records stay conservative (a
delete does not fabricate a tombstone without inventory confirmation).

## GCP live-smoke gate status

Issue #2644 records the gate outcome below. The live smoke ran against a
throwaway, dedicated read-only identity: a purpose-created service account
granted only `roles/cloudasset.viewer`, authenticated via keyless service
account impersonation (no long-lived JSON key), scoped to an organization
parent.

Sanitized result: a bounded org-scope `assets.list` smoke, page-capped at 5
pages through a `PageProvider` wrapper (so the scan cost stays bounded
regardless of org size), drained 782 facts in approximately 2.4 seconds — 500
`gcp_cloud_resource`, 278 `gcp_cloud_relationship`, 4 `gcp_tag_observation`; 0
collection warnings this pass; `redaction_policy_version` present on 100% of
`gcp_cloud_resource` facts. Transport was GET-only (read-only by construction).
The evidence captured no tenant, account, resource identifier, hostname, IP
address, or credential material. The redaction key for this run was fresh
random material generated with `crypto/rand`, written to a temporary
read-only-mounted file, and loaded back through the same file-load path the
collector command uses, so the run also exercises the redaction-key-file gate
item.

The smoke is reproducible via the gated Go test
(`go/internal/collector/gcpcloud/gcpruntime/liveclient_smoke_test.go`), which
requires `ESHU_GCP_LIVE_SMOKE=1` plus an operator-provided throwaway target
(`ESHU_GCP_SMOKE_SA`, `ESHU_GCP_SMOKE_ORG`, `ESHU_GCP_SMOKE_QUOTA_PROJECT`). The
test is default-off and CI never runs it — the environment gate skips it
cleanly in every CI run.

This run satisfies the section 6 reviewer-allowlist items for the GCP path:

- Read-only, GET-only transport with no mutate/register/export/delete
  reachable.
- ADC / workload identity federation via keyless service-account
  impersonation; no long-lived key material mounted.
- Credential carried as a reference/name only (`CredentialRef` is a config
  string, never material).
- Multi-layer default-deny: fixture is the CLI default. For Helm-managed
  launches, the chart is default-off. Direct CLI claimed-live does not read
  Helm state; it requires explicit `-mode claimed-live`, enabled claim-capable
  collector configuration, a workflow claim, and `-redaction-key-file`. The
  Helm path supplies the same command, configuration, claim, and key gates.
  Omitting a relevant gate fails closed.
- Every emitted fact passed through the redacting envelope builders and
  stamped `redaction_policy_version` (verified at 100% coverage in the smoke
  run).
- Bounded pagination, timeout, and retry behavior; the smoke run bounded the
  scan at 5 `assets.list` pages at the provider seam (not just a fact count
  after an unbounded drain) and completed within the per-call timeout budget.
- Warning facts and truth labels are produced rather than swallowed; this pass
  observed 0 warnings, which is a valid (not silent) outcome given the
  bounded run.
- Telemetry stayed to bounded-enum-only labels; no resource id, name, project
  id, URL, tag value, IAM member, or credential name appeared in the captured
  evidence.
- The redaction key was mounted read-only from a file, per the loader in
  `go/cmd/collector-gcp-cloud/main.go`.

The GCP path's default posture is unchanged by this gate: fixture remains the
CLI default and `gcpCloudCollector.enabled` remains false. The explicit
`collector-gcp-cloud -mode claimed-live` command and conditional chart wiring
are shipped, and the sanitized smoke above records the completed #1997/#2644
security evidence. This review does not enable live collection by default or
promote the canonical GCP lane beyond `gated`.

## Azure live-smoke gate status

The claimed-live runtime shipped in #3050 and its default-off Helm exposure
shipped in #3059. `collector-azure-cloud -mode claimed-live` requires an
enabled, claim-enabled Azure instance, `live_collection_enabled=true`, a
workflow claim, and `-redaction-key-file` before it calls Azure. Its Azure RBAC
grant is an operator prerequisite that the smoke must prove; the runtime does
not inspect the grant. The Helm chart creates that deployment only when
`azureCloudCollector.enabled=true`; its render checks require enabled
collector-side and coordinator-side Azure instances, enabled scopes, and the
redaction Secret mount. They do not compare instance IDs. The operator must
pair the same instance on both sides; claimed-work and live-smoke evidence must
verify that pairing before promotion.

The zero-value factory still protects fixture/default mode. No sanitized
operator run has supplied the evidence needed to promote Azure, so the Azure
readiness lane remains `gated` and every checklist item below stays unchecked.
This checklist is only the security subset of #3066. The
[Azure readiness entry](../../public/reference/collector-reducer-readiness.md)
also requires sanitized evidence of fact counts, zero reducer backlog, API/MCP
status-readback agreement, and claim lease/heartbeat behavior before promotion.

An operator-run live smoke must prove:

- an operator-granted read-only identity scoped only to the configured review
  parent; the runtime does not enforce that RBAC grant;
- workload or managed identity auth with no long-lived secret material mounted;
- a bounded Resource Graph query. Claimed-live does not wire ARM fallback; that
  injectable seam is out of scope for this smoke;
- result and warning counts captured without tenant, subscription, resource,
  hostname, IP, credential, query text, or raw provider body values;
- fixture and chart defaults fail closed without explicit live opt-in.

## 6. Reviewer allowlist (all must be checked before enablement)

Each item is tracked per provider. GCP is checked on the strength of the
sanitized live-smoke evidence recorded in "GCP live-smoke gate status" above.
Azure stays unchecked until its own isolated live run lands (see "Azure
live-smoke gate status"). Checking an item here records review evidence only;
it does not flip any command, chart, or config default to live.

- [x] (GCP) / [ ] (Azure) Live credential is a dedicated, read-only identity
      scoped to the configured parent only; no write/delete; no data-plane
      secret access.
- [x] (GCP) / [ ] (Azure) Auth uses workload/federated identity; no
      long-lived keys/secrets mounted.
- [x] (GCP) / [ ] (Azure) Credential carried as reference/name only — no
      material in code, config, logs, spans, metrics, or facts.
- [x] (GCP) / [ ] (Azure) Inert stubs remain the default; the live adapter is
      reachable only via explicit operator opt-in; accidental wiring fails
      loudly.
- [x] (GCP) / [ ] (Azure) Redaction key from a `filepath.Clean`-ed read-only
      file, blank rejected, never logged; mount perms restrictive; rotation
      defined (GCP rotation semantics: see section 2 and
      `gcpcloud.RedactionPolicyVersion`).
- [x] (GCP) / [ ] (Azure) Every emitted fact passes through the redacting
      envelope builders and stamps `redaction_policy_version`.
- [x] (GCP) / [ ] (Azure) All live calls read-only; no
      mutate/register/export/delete reachable.
- [x] (GCP) / [ ] (Azure) Pagination/timeout/concurrency/quota/backoff/
      response-size bounded; token expiry degrades to durable partial warning.
- [x] (GCP) / [ ] (Azure) Partial/permission-hidden/throttle/unsupported
      outcomes emit warning facts and correct truth labels; no silent
      fallback to empty success.
- [x] (GCP) / [ ] (Azure) No resource IDs, names, URLs, tag/label
      values, IAM/identity principals, query text, or credential names in
      metric labels, span attributes, log fields, or status keys.
- [x] (GCP) / [ ] (Azure) Live smoke tests run in an isolated review
      environment with a throwaway least-privilege identity;
      fixtures/recordings carry no real tenant/account IDs, hostnames,
      secrets, or proprietary identifiers (placeholders only).
- [x] (GCP) / [ ] (Azure) Helm/chart wiring exposes credential + redaction-key
      mounts as read-only secrets, with the live transport off by default.
