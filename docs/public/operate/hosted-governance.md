# Hosted Governance Posture

Use this page before onboarding a team to a hosted Eshu deployment or enabling
optional provider, collector, extension, API, or MCP access. It states what is
protected today, what is policy-gated, and what is still planned so operators do
not imply tenant isolation that does not exist yet.

## Current Boundaries

Hosted Eshu is a shared-service deployment of the same facts-first platform:
collectors and ingesters commit source facts, the resolution engine owns graph
truth, and API/MCP/CLI reads return bounded answers from graph, content, status,
and read-model stores.

Current shipped behavior:

| Area | Current behavior | Do not claim |
| --- | --- | --- |
| API/MCP authentication | A deployed service authenticates with a shared bearer token. `/readyz` proves the token is accepted. | Per-team or per-repository read isolation. |
| Repository scope | `eshu hosted-onboard` validates narrow repository rules and rejects accidental broad globbing unless `--confirm-broad` is set. | Repository onboarding rules as an authorization boundary after data is indexed. |
| Source ACLs | Deterministic reads use indexed facts and read models. Semantic and extension policy docs describe required source gates. | Complete source-ACL enforcement across every hosted read. |
| Semantic providers | No-provider mode is supported. Configured provider profiles are handles plus metadata and still require policy before source egress. | That a configured provider profile is permission to send content. |
| Collector egress | The workflow coordinator can filter enabled claim-capable collectors by `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON` before scheduled or freshness work is planned. | That Helm NetworkPolicy alone proves collector runtime policy or tenant isolation. |
| Extensions | Component trust can materialize verified activations, and the workflow coordinator requires `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` before planning component-extension claims. The full hosted extension policy file and charted extension runtime remain future work. | That installed or enabled components can collect in hosted mode without an explicit extension egress allow rule. |
| Network egress | Helm can render restricted NetworkPolicy egress classes for DNS, datastore, graph, internal service, collector providers, semantic providers, and extensions. | That `networkPolicy.egress.mode=broad` is least-privilege proof. |
| Redaction and retention | The hosted redaction registry defines forbidden raw classes, safe bounded field classes, and synthetic leakage canaries. The hosted retention policy defines design-only deletion, tombstone, and graph-rebuild semantics. Semantic posture docs still require redaction policy and metadata-oriented retention for optional provider work. | That runtime retention and deletion enforcement is implemented end to end. |
| Audit | The private Postgres audit sink persists validation-safe governance events and feeds aggregate-only API/MCP status counts. Detailed private queries require an operator-authorized storage call bounded by event type, actor class, scope class, decision, reason code, correlation id, and time window. | That public status, metrics, MCP readbacks, docs, or tickets may include raw event bodies, principals, source identifiers, prompts, provider responses, credential handles, private URLs, or token values. |

Before onboarding, run the [Hosted Security Posture Gate](hosted-security-posture.md)
against the operator values file. It proves API/MCP token references, Postgres
and graph credential references, pprof binding, and public-docs exposure posture
without printing credential values.

Tracked follow-up work covers per-team tokens, tenant/workspace isolation,
egress proof, redaction proof, runtime retention enforcement, and end-to-end
hosted governance proof. Until those land, use this page as an operator
checklist, not as a promise of multi-tenant isolation.

The shared governance policy model is a design gate, not runtime enforcement
yet. It keeps current local/no-policy behavior, hosted single-tenant shared
service behavior, and future multi-tenant policy revisions separate so later
implementation work does not blur source-fact truth, authorization, provider
egress, extension activation, retention, audit, and redacted status ownership.
Until the implementation issues land, any missing, invalid, stale, or partial
governance policy must disable optional governed work instead of widening
access or implying tenant isolation.

## Preflight Before Onboarding

Run governance preflight before handing a team an onboarding artifact.

1. Run the local hosted governance proof gate before remote Compose,
   Kubernetes, or GitOps promotion:

```bash
scripts/test-verify-hosted-governance-proof.sh
scripts/verify-hosted-governance-proof.sh
```

The local gate composes focused API/MCP scoped-token governance tests, redaction
and audit readback canaries, hosted security posture proof, and NetworkPolicy
egress proof. It is source-only and must not require live hosts, clusters,
private values, provider credentials, or tenant data.

2. Confirm the endpoint and token source are operator-managed:

```bash
export ESHU_SERVICE_URL=https://eshu.example.com
# Load ESHU_API_KEY from a secret manager or private shell first.
eshu hosted-setup --service-url "$ESHU_SERVICE_URL"
```

3. Check process health, dependency readiness, and completeness:

```bash
curl -fsS "$ESHU_SERVICE_URL/healthz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/readyz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/admin/status"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/api/v0/status/index"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/api/v0/status/governance"
```

`/api/v0/status/governance` reports only safe mode, state, policy-revision
hash, readiness booleans, aggregate counts, and reason codes. It must not show
raw policy bodies, tenant or workspace identifiers, source identifiers,
credential handles, provider endpoints, prompts, provider responses, or token
values.

4. Check optional semantic extraction status before enabling provider-backed
   answers:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/status/semantic-extraction"
```

No-provider mode should report unavailable or policy-disabled semantic
extraction without failing deterministic API, MCP, ingestion, reducer, or docs
verification paths.

5. If component packages are configured on the deployed runtime, inspect only
   the redacted inventory and diagnostics:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/component-extensions?limit=100"
```

MCP equivalents are `get_hosted_governance_status`,
`get_semantic_capability_status`, `list_component_extensions`, and
`get_component_extension_diagnostics`.

6. Generate the team artifact only after the above checks match the intended
   posture:

```bash
eshu hosted-onboard \
  --service-url "$ESHU_SERVICE_URL" \
  --team example-team \
  --repo example-org/example-service \
  --out onboarding-example-team.md
```

The artifact records the token source name and redacted endpoints. It must not
carry the bearer token value.

### Governance Status Readback

The governance status route reads only safe runtime metadata from these
environment keys:

| Key | Safe value shape |
| --- | --- |
| `ESHU_GOVERNANCE_MODE` | `local_no_policy`, `hosted_single_tenant`, or `hosted_multi_tenant` |
| `ESHU_GOVERNANCE_STATE` | `disabled`, `partial`, `enforcing`, `stale`, or `invalid` |
| `ESHU_GOVERNANCE_SOURCE_KIND` | `environment`, `kubernetes_secret`, `config_map`, `postgres_revision`, or `unknown` |
| `ESHU_GOVERNANCE_POLICY_REVISION_HASH` | Opaque `sha256:` revision hash only |
| `ESHU_GOVERNANCE_AUTH_MODE` | `none`, `shared_token`, or a future scoped-token class |
| `ESHU_GOVERNANCE_TENANT_MODE`, `ESHU_GOVERNANCE_WORKSPACE_MODE` | Mode names only, not tenant or workspace identifiers |
| `ESHU_GOVERNANCE_EGRESS_MODE` | `restricted`, `broad`, or `not_configured` |
| `ESHU_GOVERNANCE_REDACTION_STATE`, `ESHU_GOVERNANCE_RETENTION_MODE`, `ESHU_GOVERNANCE_AUDIT_STATE`, `ESHU_GOVERNANCE_EXTENSION_MODE` | Low-cardinality posture names |
| `ESHU_GOVERNANCE_DENIED_DECISION_COUNT`, `ESHU_GOVERNANCE_POLICY_SECTION_COUNT`, `ESHU_GOVERNANCE_STALE_SECTION_COUNT` | Non-negative aggregate counts |
| `ESHU_GOVERNANCE_REASONS` | Comma-separated reason codes from the hosted governance allowlist |

Do not put raw policy documents, tenant names, workspace names, repository
names, source identifiers, credential handles, private endpoints, prompts,
provider responses, local paths, or token values in these keys.

## Provider Modes

### No-Key Local Or Hosted Mode

Leave semantic provider profiles and policy unset. Deterministic answers remain
available; semantic observations and code hints are absent and status explains
`provider_not_configured` or policy-disabled state.

### Docker Compose Development Mode

Use provider profile JSON only when a developer intentionally tests a local
provider or gateway. Store credential values outside Compose files and docs.
Public examples should prove the mode without showing credential handles:

```bash
export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='{"profiles":[{"profile_id":"semantic-local-docs","provider_kind":"ollama","credential_source":{"kind":"cloud_workload_identity"},"model_id":"local-docs-model","source_classes":["documentation"],"source_policy_configured":true}]}'
```

Pair the profile with source policy, semantic-provider egress policy, limits,
redaction, and retention. Compose development proof is not hosted isolation
proof.

### Hosted Provider-Key Mode

Hosted deployments should use Kubernetes Secrets, external secret handles,
cloud workload identity, or an internal gateway. Do not ask end users to paste a
provider key into an assistant client, MCP config, issue body, docs page, or PR.
Operator examples should name only source classes and credential-source classes.
Pair any provider profile with a restricted semantic-provider egress rule in
`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` and restricted NetworkPolicy egress for
the `semanticProviders` class. If provider traffic routes through an internal
gateway, point the class at that gateway selector rather than enabling broad
pod egress.

### Internal Gateway Mode

Use `provider_kind=internal_gateway` when an organization routes provider calls
through a governed gateway. The gateway still needs source policy,
semantic-provider egress policy, tenant or workspace routing, redaction,
retention, budget, and audit controls. A gateway endpoint in docs should be a
generic service URL, not a private hostname. Hosted Helm values should express
that gateway through
`networkPolicy.egress.classes.semanticProviders.to` using public-safe label
selectors in shared examples and concrete selectors in private operator values.

## Safe Example Shapes

### Local

```bash
export ESHU_SERVICE_URL=http://localhost:8080
unset ESHU_SEMANTIC_PROVIDER_PROFILES_JSON
unset ESHU_SEMANTIC_EXTRACTION_POLICY_JSON
eshu hosted-setup --service-url "$ESHU_SERVICE_URL"
```

### Docker Compose

```bash
# Load ESHU_API_KEY and any provider credentials from a private env file first.
docker compose up --build eshu mcp-server
eshu hosted-setup --service-url "$ESHU_SERVICE_URL"
```

### Helm Or Kubernetes

Use private values or secret-management tooling for credentials. Public values
files may name provider kinds and source classes, but must leave concrete
credential references in private operator values:

```yaml
semanticProvider:
  profiles:
    - profileId: semantic-docs-default
      providerKind: internal_gateway
      credentialSourceKind: cloud_workload_identity
      sourceClasses:
        - documentation
```

Render and lint before applying:

```bash
helm template eshu ./deploy/helm/eshu -f values.hosted-governance.yaml
helm lint ./deploy/helm/eshu -f values.hosted-governance.yaml
```

## Operator Runbooks

### Denied Reads

1. Confirm the caller used the intended token source and endpoint.
2. Check `/readyz`; a 401 or 403 is an authentication or authorization failure,
   not an index problem.
3. Check the specific query envelope for `unsupported_capability`,
   `permission_denied`, `not_found`, or missing evidence.
4. Check `/api/v0/status/index` and repository coverage before reindexing.
5. Do not broaden repository scope or issue a shared token until the owner
   confirms the intended access boundary.

### Blocked Provider Egress

1. Check `/api/v0/status/semantic-extraction`.
2. Separate `credential_configured` from `source_policy_configured`.
3. Confirm the provider profile, source class, source allowlist, budget,
   redaction mode, and retention posture all match.
4. Treat policy-denied, unsafe, provider-unavailable, and budget-exhausted as
   different failure classes.
5. Current semantic policy and queue planning are pure evaluation paths. Use
   semantic status and policy-denied queue rows as the source of truth until a
   source-level semantic planner owns a private governance audit writer.
6. Keep raw prompts, provider responses, source IDs, credential handles, and
   token-bearing URLs out of tickets.

### Blocked Collector Egress

1. Check the workflow coordinator config source for
   `ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON`.
2. Confirm the policy mode is `restricted` or `broad`.
3. In restricted mode, confirm the collector kind has an explicit allow rule and
   no overlapping deny rule.
4. Check coordinator logs for `collector_kind` and reason codes such as
   `egress_policy_missing` or `egress_provider_denied`.
5. Keep provider URLs, token environment names, source IDs, account IDs, and
   webhook payloads out of tickets.

### Invalid Policy

1. Disable the policy or leave no-provider mode active until the policy parses.
2. Validate that every profile references only credential handles.
3. Confirm every source class has explicit scope, source selector, budget,
   redaction, and retention settings.
4. Re-run status checks and keep the failed policy body in private operator
   storage, not public docs or issue comments.

### Governance Audit Review

1. Check `/api/v0/status/governance` or `get_hosted_governance_status`.
2. Review aggregate audit event, denied decision, unavailable decision,
   event-type, actor-class, scope-class, reason, and ACL-state counts.
3. Use the private audit sink for detailed event fields only after confirming
   the operator is authorized for that scope.
4. Keep detailed audit searches bounded by actor class, scope class, decision,
   reason code, correlation id, and a narrow time window. Do not search by raw
   names, paths, URLs, document titles, prompts, or credential handles.
5. Retain detailed event fields only in the private audit sink for the hosted
   policy retention window. Status and MCP surfaces keep aggregate counts only.
6. Keep actor identifiers, tenant names, repository names, source identifiers,
   prompts, provider responses, credential handles, private URLs, and token
   values out of tickets.

### Denied Read Investigation

1. Check `/api/v0/status/governance` or `get_hosted_governance_status`.
2. Confirm `audit.denied_decision_count` increased and review
   `audit.actor_class_count`, `audit.scope_class_count`, and
   `audit.reason_count`.
3. Query the private audit sink by `event_type=read_authorization`,
   `decision=denied`, actor class, scope class, reason code, and time window.
4. If the reason is `subject_scope_missing`, verify the scoped token or service
   principal policy against the intended low-cardinality scope class.
5. Put only the event type, actor class, scope class, decision, reason code,
   correlation id, and timestamp in the ticket.

### Blocked Semantic Egress

1. Check `/api/v0/status/semantic-extraction`.
2. Confirm the redacted provider profile, source-policy, policy-denied,
   unsafe-payload, provider-unavailable, and budget-exhausted counts before
   enabling provider traffic.
3. Current semantic egress decisions intentionally do not append governance
   audit events because the shipped semantic policy and queue packages are pure
   parser/planner code with no provider work writer. Do not expect
   `event_type=semantic_policy_decision` rows until a source-level semantic
   planner owns an audit sink.
4. If the reason is `egress_policy_missing`, verify that the provider profile,
   source class, redaction posture, retention posture, and budget posture are
   configured before enabling egress.
5. Put only safe classes and reason codes in the ticket; keep prompts, provider
   responses, source identifiers, provider endpoints, and credential handles in
   private operator storage.

### Redaction Regression

1. Check the affected surface in
   [Hosted Redaction Registry](../reference/hosted-redaction-registry.md).
2. Add or update a focused test that calls
   `Registry.AssertNoForbiddenCanary(surface, payload)` after the owning
   surface applies its source-specific redaction.
3. Keep real credentials, private URLs, prompts, source identifiers, and direct
   personal identifiers out of fixtures and tickets.

### Extension Revocation

Hosted community extension execution is not enabled by the shipped chart or
Compose stack today. For policy review or future rollout:

1. Revoke by component ID, publisher, artifact digest, version range, or policy
   revision.
2. Stop new coordinator claims for the revoked identity.
3. Mark pending work ineligible with a bounded reason such as
   `revoked_policy`.
4. Confirm `ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON` no longer allows the
   component identity before expecting new workflow rows to stop.
5. Re-check policy before launching work and before committing facts.
6. Scale down compromised extension workloads when needed and keep status
   fields to component ID, instance ID, reason class, policy revision, and safe
   scope class.

### Tenant Offboarding

Until scoped tokens and hosted tenant isolation land, offboarding is an
operator-controlled shared-service procedure:

1. Remove or narrow repository sync rules for the team.
2. Rotate the shared bearer token if the team had access to it.
3. Disable provider policy rules and extension instances scoped to the team.
4. Rebuild or delete indexed state according to the retention policy that
   applies to the deployment.
5. Record aggregate evidence: which rule set changed, which token source was
   rotated, which policy revision applied, and which queues drained.

### Retention Or Deletion Progress

1. Check the affected data class in
   [Hosted Retention And Deletion Policy](../reference/hosted-retention-deletion-policy.md).
2. Confirm new governed work stopped for the affected source class.
3. Check deletion state, aggregate counts, policy revision hash, and bounded
   reason codes in governance status.
4. Wait for reducer repair and graph rebuild before treating reads as current.
5. Keep tenant names, repository names, source identifiers, prompts, provider
   responses, credential handles, private URLs, backup object locators, and raw
   policy bodies out of tickets.

### Redaction Proof Failure

1. Stop provider egress or disable the affected source policy.
2. Preserve the failed proof artifact in private incident storage.
3. Check semantic status for policy/guard decision, source class, provider kind,
   profile class, budget state, and failure class.
4. Do not paste raw excerpts, prompts, provider responses, credential handles,
   private hostnames, or source identifiers into public tickets.
5. Re-run the redaction proof after policy or redaction changes before
   re-enabling egress.

## Proof Gates

Use these gates for hosted governance-related changes:

| Change | Minimum proof |
| --- | --- |
| Public docs or navigation | Strict MkDocs build and `git diff --check`. |
| Hosted API/MCP auth, Secret refs, pprof, or docs exposure posture | `scripts/test-verify-hosted-security-posture.sh`, `scripts/verify-hosted-security-posture.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml`. |
| Hosted NetworkPolicy egress posture | `scripts/test-verify-hosted-network-policy-egress.sh`, `scripts/verify-hosted-network-policy-egress.sh -f values.eshu.yaml`, and `helm lint deploy/helm/eshu -f values.eshu.yaml`. |
| Hosted governance local proof posture | `scripts/test-verify-hosted-governance-proof.sh`, `scripts/verify-hosted-governance-proof.sh`, strict MkDocs build, and `git diff --check`. |
| Hosted governance remote Compose proof | `scripts/test-verify-hosted-governance-remote-compose-proof.sh`, `scripts/verify-hosted-governance-remote-compose-proof.sh`, and `scripts/verify-hosted-governance-remote-compose-proof.sh --runtime` from the private remote Compose operator environment. |
| Hosted governance proof artifact | `scripts/test-verify-hosted-governance-proof-artifact.sh`, `scripts/verify-hosted-governance-proof-artifact.sh --input governance-proof.json --output-json governance-proof.summary.json --output-markdown governance-proof.summary.md`, strict MkDocs build, and `git diff --check`. |
| Hosted retention or deletion policy docs | Strict MkDocs build and `git diff --check`; runtime implementation must add focused deletion, tombstone, stale-read, and graph-rebuild tests. |
| Hosted governance audit events or readbacks | `go test ./internal/governanceaudit ./internal/storage/postgres ./internal/query ./cmd/api ./cmd/mcp-server -count=1`, package-doc gates, performance-evidence gate, and strict MkDocs build. |
| Hosted extension egress claim gate | `go test ./internal/coordinator -run 'Test(ParseExtensionEgressPolicyJSON|ExtensionEgressPolicy|LoadConfigParsesExtensionEgressPolicy|ServiceRun.*ComponentExtension|ServiceComponentExtension)' -count=1`, package-doc gates, performance-evidence gate, and strict MkDocs build. |
| Hosted onboarding or setup CLI | `go test ./cmd/eshu -count=1`. |
| API/MCP status surfaces | `go test ./internal/query ./internal/mcp ./cmd/api -count=1`. |
| Semantic extraction status or queue readbacks | `go test ./internal/semanticqueue ./internal/storage/postgres ./internal/status ./internal/query ./internal/telemetry -count=1`. |
| Compose hosted profile shape | `scripts/test-remote-e2e-hosted-compose-render.sh`. |
| Hosted extension policy implementation | Security review, remote Compose proof, and Kubernetes render/lint proof before any hosted rollout claim. |

## Public-Artifact Rules

Never put these in docs, issues, PR text, public values files, onboarding
artifacts, logs, metrics, or status payloads:

- raw API tokens, provider keys, cloud credentials, private keys, or signed URLs
- private hostnames, tenant domains, local filesystem paths, repo paths, or
  source payloads
- raw provider prompts, responses, excerpts, or failure payloads
- direct personal identifiers or private contact details
- credential values or token-bearing endpoint URLs

Use source classes, credential-source classes, safe scope classes, aggregate
counts, hashes, and bounded reason classes instead.
