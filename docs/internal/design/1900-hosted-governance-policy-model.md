# Hosted Governance Policy Model (#1900)

Status: **PROPOSED - SECURITY REVIEW REQUIRED BEFORE RUNTIME ENFORCEMENT.**

Refs #1900. Refs #1774, #1781, #1852, #1899, #1902, #1903, #1905,
#1906, #1907, #1908, #1909, #1910, #2036. See also
[Hosted Governance Posture](../../public/operate/hosted-governance.md),
[Hosted Project Onboarding](../../public/deployment/hosted-onboarding.md),
[Semantic Enrichment Posture](../../public/reference/semantic-enrichment-posture.md),
[Hosted Redaction Registry](../../public/reference/hosted-redaction-registry.md),
[Hosted Retention And Deletion Policy](../../public/reference/hosted-retention-deletion-policy.md),
[Plugin Trust Model](../../public/reference/plugin-trust-model.md),
[Semantic Extraction Policy](1754-semantic-extraction-policy.md), and
[Hosted Extension Operator Policy](1826-hosted-extension-operator-policy.md).

This is a maintainer-only design gate. It defines the shared model and
ownership boundaries that later implementation issues must preserve. It does
not add runtime enforcement, new auth modes, storage schema, API routes, MCP
tools, Helm values, or collector behavior.

## Decision

Hosted governance is a core-owned policy layer over the existing facts-first
runtime. It must not replace source facts, reducer-owned graph truth, or bounded
API/MCP reads. It coordinates these decisions:

- request identity and authorization scope;
- tenant, workspace, repository, and service-principal boundaries;
- semantic provider egress and source allowlists;
- collector and extension enablement;
- redaction, retention, and deletion posture;
- audit event eligibility and redacted operator status;
- fail-closed behavior for missing, invalid, stale, or partial policy.

The v1 policy model has three deployment modes:

| Mode | Policy source | Runtime posture |
| --- | --- | --- |
| `local_no_policy` | No governance policy object. Local development may use no token or a shared token. | Deterministic local reads remain available. Provider egress, hosted extension claims, and scoped-token claims stay disabled unless their narrower policies already allow them. |
| `hosted_single_tenant` | Operator-managed config bundle loaded from environment, Kubernetes Secret or ConfigMap, Helm-rendered private values, or another private deployment source. | One tenant/workspace boundary is explicit, shared-token reads remain a known limitation until #1852, and optional provider or extension work fails closed unless policy allows it. |
| `hosted_multi_tenant` | Durable Postgres policy revisions owned by a future governance service or API, with migration and tenant/workspace schema from #1902. | Every auth, claim, egress, extension, query, retention, audit, and status decision names the policy revision and tenant/workspace boundary before doing work. |

The authoritative runtime state is a validated policy revision, not raw config
text. Runtime components may read their specific subset, but status and audit
must name only a safe policy id, revision hash, mode, and low-cardinality
reason codes.

## Schema Contract

Implementation issues should model `governance_policy.v1` as a normalized
document or equivalent typed model with these top-level sections:

| Section | Required fields | Notes |
| --- | --- | --- |
| `metadata` | `policy_id`, `revision`, `mode`, `source_kind`, `issued_at`, optional `expires_at` | `revision` must be stable enough for status, audit, and claim idempotency. Status should expose a hash or opaque revision, not private source paths. |
| `identity` | auth mode, accepted issuer classes, subject classes, token scope mode | Current shared bearer-token mode is `shared_token`. Future scoped tokens are #1852. |
| `tenancy` | tenant mode, workspace mode, repository-scope mode, default boundary | Future durable tenant/workspace persistence is #1902. Empty tenant state in hosted enforcing mode is a deny reason. |
| `authorization` | read scopes, admin scopes, claim scopes, default decision | API/MCP reads enforce here after identity is attached. Collectors and reducers do not infer authorization from repository names. |
| `egress` | provider classes, collector egress classes, extension egress classes, default decision | Semantic egress intersects this section with `internal/semanticpolicy`; hosted network-policy proof is #1909. |
| `semantic` | provider policy refs, source class refs, redaction ref, retention ref | The existing semantic profile and extraction policy remain the source-specific evaluator. Governance does not duplicate provider credentials. |
| `extensions` | trust mode refs, activation refs, claim-capable refs, revocation refs | The existing component trust policy and hosted extension policy remain the package and claim evaluators. |
| `redaction` | redaction registry revision, status exposure rules, negative canary class | Central registry and proof are #1906. |
| `retention` | fact, content, semantic evidence, audit, and deletion postures | Detailed deletion and tombstone semantics are #1905. |
| `audit` | enabled decision classes, sink class, retention ref, failure policy | Audit events are #1903. Missing audit sink in enforcing hosted mode denies admin and policy-changing actions. |
| `status` | exposed state fields, redaction classes, reason code allowlist | API/MCP/admin readbacks are #1908. |

Policy values must contain credential handles or source classes only. Raw
tokens, provider keys, cloud credentials, local paths, private hostnames, source
payloads, prompts, provider responses, and credential-bearing URLs are invalid
policy content.

## Existing Policy Mapping

The governance layer composes existing policy contracts instead of replacing
them:

| Existing surface | Current owner | Governance mapping |
| --- | --- | --- |
| Shared bearer-token auth in `go/internal/query/auth.go` | API/MCP query surface | `identity.auth_mode=shared_token`; status must say this is not tenant isolation. |
| Semantic provider profiles | `internal/semanticprofile` and status projection | Provider inventory only. Governance may reference provider profile classes but never treats a profile as egress permission. |
| Semantic extraction policy | `internal/semanticpolicy` | Source-specific allowlist, ACL, budget, redaction, and retention evaluator. Governance egress must intersect with it. |
| Component trust policy | `internal/component` | Package install trust gate for disabled, allowlist, and strict modes. Governance references the trust result for hosted activation. |
| Hosted extension operator policy | `docs/internal/design/1826-hosted-extension-operator-policy.md` and future runtime owners | Claim-capable gate. Governance owns tenant/workspace and audit linkage but does not make installed components runnable by itself. |
| Hosted onboarding repository rules | CLI hosted onboarding | Ingestion preflight only. Repository rules are not authorization after data is indexed. |
| Helm and Kubernetes network policy | Deployment owners | Deployment enforcement and proof for egress posture. Governance status reports only safe mode, state, and reason classes. |

## Ownership Boundaries

| Owner | Governance responsibility | Must not do |
| --- | --- | --- |
| API and MCP | Authenticate, attach request identity, enforce read/admin scope, return `permission_denied` for denied scopes, expose redacted status. | Must not treat repository onboarding rules or shared token possession as tenant isolation. |
| Workflow coordinator | Create claims only for identities, tenant/workspace scope, policy revision, source scope, and collector or extension mode allowed by policy. | Must not issue work for stale, partial, revoked, or tenantless hosted policy. |
| Collectors and ingesters | Observe bounded source scopes, resolve credential handles through runtime identity, emit source facts with policy/audit-safe metadata. | Must not write graph truth, persist raw secrets, or infer authorization from source names. |
| Semantic extraction | Run provider work only when governance egress, provider profile, semantic source policy, ACL, redaction, retention, and budget all allow. | Must not fall back to provider work when deterministic evidence is missing or policy is absent. |
| Collector egress | Filter active-mode claim-capable collector instances before scheduled or freshness work can create workflow rows. | Must not treat network reachability or a configured collector instance as policy permission to claim external work. |
| Component manager and extension host | Verify install trust, evaluate activation, re-check policy before launch and fact commit, surface safe diagnostics. | Must not let installed or enabled components become claim-capable without hosted policy. |
| Reducer and projection | Preserve facts-first truth, tenant/workspace anchors from facts once #1902 lands, and graph/read-model correctness. | Must not invent tenant boundaries, hide unauthorized data with query-only filters, or change graph truth based on caller identity. |
| Graph and content stores | Persist facts, content, queues, status, and graph truth under explicit schema contracts. | Must not become the authorization engine through hidden backend-specific filters. |
| Operator docs and examples | Describe current limitations, safe status, proof gates, and private-storage requirements. | Must not publish policy bodies or examples containing private identifiers or credential values. |

## Update And Reload

Policy loading must be atomic:

1. Parse and validate the complete policy document.
2. Normalize referenced sub-policies and ensure required refs exist.
3. Compute the policy revision hash.
4. Publish the revision to runtime readers only after validation succeeds.
5. Keep the prior valid revision only until its `expires_at` or configured TTL.

Hosted enforcing mode fails closed after the last valid revision becomes stale.
A bad reload must not partially update egress, extension, read, or retention
state. Local no-policy mode may keep deterministic dev reads available, but it
must report `local_no_policy` or `shared_token_mode` instead of implying hosted
governance.

Future multi-tenant updates must use a durable Postgres policy revision table
and an idempotent activation transaction. That table belongs to #1902 or a
child issue of #1902, not this design gate.

## Fail-Closed Matrix

| Condition | Hosted enforcing behavior | Local/no-policy behavior | Reason code |
| --- | --- | --- | --- |
| Policy missing | Deny scoped tokens, admin policy changes, provider egress, extension claims, and collector work that requires policy. | Deterministic local reads may continue; optional governed work remains disabled. | `policy_not_configured` |
| Policy invalid | Reject startup or keep last valid revision until TTL, then deny governed work. | Report invalid policy and leave governed work disabled. | `policy_invalid` |
| Policy stale | Deny new governed work after TTL; keep redacted status available. | Report stale policy only for configured governed work. | `policy_stale` |
| Policy partial | Deny the affected section and any dependent work. | Disable only the affected optional capability. | `policy_partial` |
| Tenant/workspace missing | Deny hosted claims, scoped reads, semantic egress, and extension activation that require tenant scope. | Report `local_no_policy` unless hosted mode is configured. | `tenant_scope_missing` |
| Subject scope missing | Return `permission_denied`; do not widen query scope. | Shared-token local mode reports the limitation. | `subject_scope_missing` |
| Egress policy missing | Deny provider, collector, or extension egress. | Optional egress remains off. | `egress_policy_missing` |
| Redaction policy missing | Deny semantic provider work and any status route that would expose unsafe fields. | Semantic provider work remains off. | `redaction_policy_missing` |
| Retention policy missing | Deny provider work and policy-changing admin actions that need retention proof. | Metadata-only defaults may be reported for no-provider mode. | `retention_policy_missing` |
| Audit sink unavailable | Deny admin and policy-changing actions; optionally deny claim creation when policy requires audit. | Report audit unavailable without affecting deterministic local reads. | `audit_sink_unavailable` |
| Extension policy untrusted | Deny install, enable, or claim-capable state according to the violated gate. | Optional components remain disabled. | `extension_policy_untrusted` |
| Semantic policy missing | Deny provider queue and provider egress even when profiles exist. | Deterministic reads continue. | `semantic_policy_missing` |

Allowed status reason codes are intentionally low cardinality. Private tenant
ids, source ids, policy file paths, credential handles, source paths, provider
endpoints, and raw errors belong only in private operator storage when policy
allows them.

## Redacted Status Contract

API, MCP, and admin status surfaces should expose:

- `mode`: `local_no_policy`, `hosted_single_tenant`, or `hosted_multi_tenant`;
- `state`: `disabled`, `partial`, `enforcing`, `stale`, or `invalid`;
- `source_kind`: `environment`, `kubernetes_secret`, `config_map`,
  `postgres_revision`, or `unknown`;
- `policy_revision_hash`;
- `loaded_at`, `age_seconds`, and optional `expires_in_seconds`;
- booleans for identity, tenant, egress, semantic, extension, redaction,
  retention, and audit policy readiness;
- aggregate counts such as tenant count, workspace count, policy section count,
  denied decision counts, and stale section counts;
- bounded reason-code arrays.

Status must not expose raw policy JSON, subject identifiers, tenant names,
workspace names, repository names, source identifiers, credential handles,
prompt text, provider responses, private endpoints, local paths, cloud resource
ids, or token-bearing URLs.

## Edge Cases And Proof Requirements

Implementation issues must cover:

- empty local state with no policy;
- hosted startup with missing, invalid, stale, and partial policy;
- duplicate tenant/workspace/repository names after #1902;
- stale claims after policy revision change or revocation;
- deleted tenants and offboarding retention;
- provider profile configured without semantic source policy;
- semantic policy allowed but governance egress denied;
- installed component not enabled;
- enabled component not claim-capable;
- revoked component with in-flight claim;
- audit sink outage during admin or policy-changing actions;
- redaction canaries in status, logs, metrics, API, MCP, and audit;
- API and MCP parity for `permission_denied` and governance status.

## Child Issue Mapping

No new child issue is required for this design gate because the open hosted
governance set already covers the implementation slices:

| Slice | Issue |
| --- | --- |
| Scoped tokens and shared-token replacement | #1852 |
| Tenant and workspace state | #1902 |
| Audit events | #1903 |
| Retention and deletion | #1905 |
| Redaction registry and leakage proof | #1906 |
| Provider, collector, and extension egress gates | #1907 |
| Hosted collector egress claim gate | #2036 |
| API, MCP, and admin status surfaces | #1908 |
| Helm network-policy hardening | #1909 |
| End-to-end hosted governance proof | #1910 |

If an implementation issue discovers a missing durable policy-revision table,
API-managed policy write surface, or migration verifier that is not covered by
#1902 or #1908, create a bounded child issue before adding runtime behavior.

## Evidence For This PR

No-Regression Evidence: `go test ./internal/coordinator -run 'Test(ParseCollectorEgressPolicyJSON|CollectorEgressPolicy|LoadConfigParsesCollectorEgressPolicy|ServiceRunActiveModeSkipsDeniedCollectorEgress|ServiceIncidentFreshnessSkipsDeniedCollectorEgress)' -count=1` proves collector egress policy parsing, restricted default-deny behavior, deny-over-allow precedence, broad-mode validation, config loading, scheduled work suppression, and incident freshness handoff suppression. The change filters only the scheduler input slice; it does not change claim lease timing, worker counts, queue ordering, reducer graph writes, fact emission, or provider API calls.

Observability Evidence: collector egress skips reuse coordinator reconcile metrics, workflow rows, claim status, and `/api/v0/index-status`; denied scheduled work creates no claimable row. The coordinator also emits a bounded structured log with only `collector_kind` and low-cardinality `reason` so operators can distinguish `egress_policy_missing` from `egress_provider_denied` without exposing provider URLs, token environment names, source IDs, account IDs, or webhook payloads.
