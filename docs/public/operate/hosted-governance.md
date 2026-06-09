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
| Semantic providers | No-provider mode is supported. Configured provider profiles are handles plus metadata and still require source policy and semantic-provider egress policy before source egress. | That a configured provider profile is permission to send content. |
| Extensions | Hosted extension policy is operator guidance. Community extension claim execution is not enabled by the shipped chart or Compose stack yet. | That installed or enabled components can collect in hosted mode. |
| Network egress | Helm can render restricted NetworkPolicy egress classes for DNS, datastore, graph, internal service, collector providers, semantic providers, and extensions. | That `networkPolicy.egress.mode=broad` is least-privilege proof. |
| Redaction and retention | Semantic posture docs require redaction policy and metadata-oriented retention for optional provider work. | That all future governance retention and deletion workflows are implemented. |
| Audit | Existing status, telemetry, semantic queue, budget, and component diagnostics expose bounded classes and counts. | A complete hosted governance audit ledger until the governance issues land. |

Before onboarding, run the [Hosted Security Posture Gate](hosted-security-posture.md)
against the operator values file. It proves API/MCP token references, Postgres
and graph credential references, pprof binding, and public-docs exposure posture
without printing credential values.

Tracked follow-up work covers per-team tokens, tenant/workspace isolation,
governance status, egress gates, redaction proof, audit events, retention, and
end-to-end hosted governance proof. Until those land, use this page as an
operator checklist, not as a promise of multi-tenant isolation.

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

1. Confirm the endpoint and token source are operator-managed:

```bash
export ESHU_SERVICE_URL=https://eshu.example.com
# Load ESHU_API_KEY from a secret manager or private shell first.
eshu hosted-setup --service-url "$ESHU_SERVICE_URL"
```

2. Check process health, dependency readiness, and completeness:

```bash
curl -fsS "$ESHU_SERVICE_URL/healthz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/readyz"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/admin/status"
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" "$ESHU_SERVICE_URL/api/v0/status/index"
```

3. Check optional semantic extraction status before enabling provider-backed
   answers:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/status/semantic-extraction"
```

No-provider mode should report unavailable or policy-disabled semantic
extraction without failing deterministic API, MCP, ingestion, reducer, or docs
verification paths.

4. If component packages are configured on the deployed runtime, inspect only
   the redacted inventory and diagnostics:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/component-extensions?limit=100"
```

MCP equivalents are `get_semantic_capability_status`,
`list_component_extensions`, and `get_component_extension_diagnostics`.

5. Generate the team artifact only after the above checks match the intended
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
5. Keep raw prompts, provider responses, source IDs, credential handles, and
   token-bearing URLs out of tickets.

### Invalid Policy

1. Disable the policy or leave no-provider mode active until the policy parses.
2. Validate that every profile references only credential handles.
3. Confirm every source class has explicit scope, source selector, budget,
   redaction, and retention settings.
4. Re-run status checks and keep the failed policy body in private operator
   storage, not public docs or issue comments.

### Extension Revocation

Hosted community extension execution is not enabled by the shipped chart or
Compose stack today. For policy review or future rollout:

1. Revoke by component ID, publisher, artifact digest, version range, or policy
   revision.
2. Stop new coordinator claims for the revoked identity.
3. Mark pending work ineligible with a bounded reason such as
   `revoked_policy`.
4. Re-check policy before launching work and before committing facts.
5. Scale down compromised extension workloads when needed and keep status
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
