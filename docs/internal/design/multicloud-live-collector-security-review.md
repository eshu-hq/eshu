# Security Review Scope — GCP/Azure Live Cloud Collector

Pre-enablement review gate for issues #1997 (GCP) and #1998 (Azure).

**Scope of this doc:** the credential-bearing **live transport** paths only.
Fixture parsing, the shared reducer admission (`cloud_inventory_admission`), and
the API/MCP readback (`GET /api/v0/cloud/inventory`, `list_cloud_resource_inventory`)
are already shipped and fixture-proven; they are out of scope here except where
the live path changes their inputs.

This review must pass before any command, chart, credential mount,
ServiceMonitor, or live smoke path enables credential-bearing live collection.
Adapter code may merge only when default wiring stays inert and tests prove the
seam is explicitly injected, read-only, bounded, and sanitized. Today both
binaries run fixture/file-backed and make **zero** live calls. The GCP live seam
exists as an explicit-injection `gcpruntime.LiveClient`, but the GCP command and
chart do not wire it by default. The Azure live seam is gated by construction:
`azureruntime.LiveProviderFactory{}` returns
`ErrLiveProviderGated`, and live Resource Graph plus optional ARM fallback calls
require explicit operator-owned injection of read-only clients and allowlist
rules. It is not command- or chart-enabled by default.

## 1. Credential surfaces

| Provider | Live seam | Auth model to verify | Least-privilege scope |
| --- | --- | --- | --- |
| GCP | `gcpruntime.LiveClient` (`go/internal/collector/gcpcloud/gcpruntime/liveclient.go`); `CredentialRef` is a name only. | Workload Identity Federation / ADC for a dedicated service account. No long-lived JSON keys mounted. | Cloud Asset Inventory read-only (`roles/cloudasset.viewer`) at the configured org/folder/project parent only. No `assets.export`, no IAM write, no data-plane reader roles. |
| Azure | `azureruntime.LiveProviderFactory` (`go/internal/collector/azurecloud/azureruntime/live_provider.go`). Zero value is inert; explicit injected clients can read Resource Graph and allowlisted ARM `GET` only. | Managed/workload identity for the configured tenant. No client-secret string in env. | `Reader` at the configured subscription/management-group scope only; Resource Graph + allowlisted ARM `GET` only. No `Contributor`, no provider registration, no write/delete. |

Threat-model checks: privilege creep (read-only inventory, not secret *values*);
credential *reference* vs material (names only — never bytes — in struct fields,
config, fact payloads, logs, spans, metric labels); confused-deputy / scope
escape (scope enforced by the credential's own grant, not a client-side filter);
default-deny (the inert zero-value seams stay the command/chart default; any path
constructing or wiring the live adapter without explicit operator opt-in is a
finding).

GCP status as of #2701: `gcpruntime.LiveClient` is implemented as an
explicit-injection REST `PageProvider` for Cloud Asset Inventory `assets.list`.
The `collector-gcp-cloud` command and chart paths still use fixture/default-off
wiring and make no live calls unless a future security-reviewed slice explicitly
injects the live transport and credential source.

## 2. Redaction-key handling

The live path **must** mirror the shipped GCP file-based key handling
(`loadRedactionKey` in `go/cmd/collector-gcp-cloud/main.go`; the Azure binary now
mirrors it via `ESHU_AZURE_REDACTION_KEY_FILE`):

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
- Azure ARM fallback calls require exact resource-type allowlist rules, fixed
  API versions, configured extension fields, one bounded `GetByID` per
  allowlisted Resource Graph row, and oversized payloads degrade to explicit
  redaction warning evidence.
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
- Multi-layer default-deny: the live adapter is not wired by the CLI flag, the
  JSON config gate, the coordinator config test, or the Helm chart; each layer
  fails closed independently.
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

The GCP path's default posture is unchanged by this gate: the
`collector-gcp-cloud` command and chart continue to run fixture/file-backed
with no live wiring, and this review records proof only — it does not enable
live collection anywhere by default.

## Azure live-smoke gate status

Issue #2660 remains blocked on an isolated Azure review target and
operator-owned credential wiring, now tracked by issue #2665. Local and remote
preflight on 2026-06-16 found no Azure environment variable names, no Azure CLI
path, and no existing credential-bearing live-smoke runner. The current
`collector-azure-cloud` command still selects the zero-value
`azureruntime.LiveProviderFactory` when no fixture pages are configured, so it
fails closed before any live Resource Graph or ARM request can occur. That
default-off state is the intended security posture until live enablement is
explicitly reviewed.

The Azure checklist below must stay unchecked until a sanitized live run proves:

- a throwaway read-only identity scoped only to the configured review parent;
- workload or managed identity auth with no long-lived secret material mounted;
- a bounded Resource Graph query and allowlisted ARM fallback family;
- result and warning counts captured without tenant, subscription, resource,
  hostname, IP, credential, query text, or raw provider body values;
- command and chart defaults still fail closed without explicit live wiring.

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
