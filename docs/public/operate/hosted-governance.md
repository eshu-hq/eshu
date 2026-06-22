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
egress proof. It also proves local no-policy governance status, no-provider
semantic status, and no-provider semantic queue planning so deterministic paths
remain available without provider credentials or governance policy. It is
source-only and must not require live hosts, clusters, private values, provider
credentials, or tenant data.

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

5. Check optional answer narration status before exposing narrated answers:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/status/answer-narration"
```

The default posture should report unavailable or disabled narration with
deterministic answer packets available as the canonical fallback. The status
must not contain prompts, provider responses, credential handles, source IDs,
private paths, or private hostnames.

6. If component packages are configured on the deployed runtime, inspect only
   the redacted inventory and diagnostics:

```bash
curl -fsS -H "Authorization: Bearer $ESHU_API_KEY" \
  "$ESHU_SERVICE_URL/api/v0/component-extensions?limit=100"
```

MCP equivalents are `get_hosted_governance_status`,
`get_semantic_capability_status`, `get_answer_narration_status`,
`list_component_extensions`, and `get_component_extension_diagnostics`.

7. Generate the team artifact only after the above checks match the intended
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
| `ESHU_GOVERNANCE_AUTH_MODE` | `none`, `shared_token`, or `scoped_token` |
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

Generate the public-safe hosted governance Helm proof before promoting a
governance rollout past remote Compose:

```bash
scripts/test-verify-hosted-governance-helm-proof.sh
scripts/verify-hosted-governance-helm-proof.sh \
  --out-dir .proof/governance-helm \
  --values values.hosted-governance.yaml
```

The verifier composes the hosted Helm rollout proof, hosted security posture
proof, and hosted NetworkPolicy egress proof. It requires
`networkPolicy.egress.mode=restricted` and API/MCP governance status env keys
for mode, state, source kind, auth mode, and egress mode. Put those keys in
`api.env` and `mcpServer.env` in the operator values file so both read
surfaces expose the same governance status posture. The summary artifact keeps
only chart/app version, safe image reference, values digest, proof-layer
status, workload counts, schema-bootstrap status, and governance status-env
results.

Run the negative-leakage proof after collecting public-safe surface artifacts
from the private proof environment:

```bash
scripts/test-verify-hosted-governance-negative-leakage-proof.sh
scripts/verify-hosted-governance-negative-leakage-proof.sh \
  --manifest leakage-proof.json \
  --output-json leakage-proof.summary.json \
  --output-markdown leakage-proof.summary.md
```

The manifest references operator-local artifacts for facts, logs, metric
labels, status errors, graph properties, API bodies, MCP bodies, console
surfaces, audit events, generated docs, and onboarding artifacts. It also
declares the synthetic canaries that must not appear in any referenced
artifact. The verifier writes only surface names, record counts, byte counts,
line counts, SHA-256 digests, and pass/fail status.

Run the auth-audit and revocation proof after collecting aggregate counts from
the private audit sink and revocation checks:

```bash
scripts/test-verify-hosted-auth-audit-proof.sh
scripts/verify-hosted-auth-audit-proof.sh \
  --input auth-audit-proof.json \
  --output-json auth-audit-proof.summary.json \
  --output-markdown auth-audit-proof.summary.md
```

The manifest must include login/MFA, session and token lifecycle, IdP config,
role/grant changes, denied reads, tenant switches, sensitive-data access,
Ask/search runs, exports, bootstrap, break-glass, audit-read, and API/MCP
authentication event families. Ordinary reads stay in structured telemetry
unless they touch a sensitive-data or export boundary. The verifier also checks
that Eshu-owned sessions and tokens revoke immediately and that external group
membership changes refresh inside a bounded public-safe window.

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

### Scoped Per-Team Tokens

The API and MCP read surface resolves bearer tokens through an optional,
operator-managed scoped-token registry before falling back to the shared token.
A scoped token maps to a tenant, workspace, and the repository / ingestion-scope
ids it may read, so a per-team token reads only that team's onboarded scope. The
registry file is the issuance, rotation, and revocation surface; it stores only
the SHA-256 hash of each token, never the token itself.

Enable it by pointing `ESHU_SCOPED_TOKENS_FILE` at a secret-mounted JSON
registry. When the variable is unset, the surface keeps shared-token (or local
dev-mode) behavior unchanged. A malformed or unreadable registry fails startup
closed rather than running without isolation.

```json
{
  "version": 1,
  "tokens": [
    {
      "token_sha256": "<lowercase hex sha256 of the bearer token>",
      "tenant_id": "team-payments",
      "workspace_id": "team-payments",
      "subject_class": "team_token",
      "subject_id_hash": "sha256:9f86d081884c7d65",
      "policy_revision_hash": "sha256:e3b0c44298fc1c14",
      "all_scopes": false,
      "allowed_scope_ids": ["git-repository-scope:acme/payments"],
      "allowed_repository_ids": ["repo://acme/payments"]
    }
  ]
}
```

Optional `subject_id_hash` and `policy_revision_hash` fields must be `sha256:`
hashes when set. They are safe audit correlation fields, not places for raw
subjects, emails, tenant names, policy bodies, or issue links.

Operator lifecycle:

1. **Issue**: generate a random token, compute its `sha256`, add an entry with
   the hash and the team's repository/scope grants, deliver the token to the
   team over a secure channel, and reload (restart) the API/MCP pods. Never
   write the plaintext token into the registry, logs, issues, or commits.
2. **Rotate**: replace the entry's `token_sha256` with the new token's hash and
   reload.
3. **Revoke**: remove the entry and reload.

Only routes proven tenant-filtered are reachable with a scoped token; every
other route stays fail-closed with `permission_denied`. Empty grants
(`all_scopes` false with no allowed ids) return bounded empty/zero reads.

#### Two-Team Cross-Scope Denial Proof

The scoped-token denial behavior is proven end-to-end against a live API and MCP
stack, not only in unit tests. The harness stands up a Docker Compose stack with
the two-team overlay, ingests repositories, seeds two scoped tokens (one per
team) plus an admin token through `ESHU_SCOPED_TOKENS_FILE`, and asserts that
each team reads only its own repository through the real API and MCP surfaces:

```bash
# Self-test the verifier against recorded good/bad artifacts (no stack needed):
scripts/test-verify-two-team-governance-proof.sh

# Live proof: build + run the stack, seed two scoped tokens, assert the
# cross-scope denial matrix, then verify. Uses a unique compose project name and
# remapped host ports so it does not collide with other local stacks.
scripts/run-two-team-governance-proof.sh
```

The driver and overlay live at
`docs/public/run-locally/docker-compose.governance-two-team.yaml` and
`scripts/run-two-team-governance-proof.sh`. The live run asserts, on both the
API and the MCP tool-dispatch path:

- an admin (`all_scopes`) token enumerates every ingested repository;
- team-A's token lists only team-A's repository and never team-B's (and vice
  versa) — the denied cross-scope read;
- the single-repository context selector for an out-of-grant repository fails
  closed with `403 permission_denied` (that richer route is not scope-enabled,
  so scoped tokens cannot reach it at all);
- unauthenticated repository reads are rejected with `401` on both surfaces; and
- the API and MCP readbacks agree (parity).

The captured proof artifacts record counts and HTTP states only — never response
bodies, raw tokens, or token hashes — so the redaction canary holds by
construction. The live driver is operator-gated; CI and the local governance
gate run the verifier self-test through
`scripts/verify-hosted-governance-proof.sh`, and `--runtime` on
`scripts/verify-hosted-governance-remote-compose-proof.sh` runs the live driver
from a private operator environment.

#### Live Kubernetes/Helm Cross-Scope Denial Proof

The same cross-scope denial is proven on a live Kubernetes cluster through the
Helm chart, not only in Compose. The driver installs the chart into a uniquely
named namespace (designed for a single-node local cluster such as OrbStack), uses
the bundled NornicDB graph backend and a minimal in-namespace Postgres, mounts an
operator-managed scoped-token registry Secret into the API and MCP server through
the chart's `api.extraVolumes`/`mcpServer.extraVolumes` hooks, seeds two
repositories with a one-shot `bootstrap-index` Job, and asserts the cross-scope
denial matrix through the in-cluster API and MCP via `kubectl port-forward`:

```bash
# Self-test the verifier against recorded good/bad artifacts (no cluster needed):
scripts/test-verify-k8s-two-team-governance-proof.sh

# Live proof: build the image, install the chart into a unique namespace, seed
# two scoped tokens, assert the cross-scope denial matrix, verify, then helm
# uninstall and delete the namespace (on success and on failure).
scripts/run-k8s-two-team-governance-proof.sh
```

The driver and values live at `scripts/run-k8s-two-team-governance-proof.sh` and
`deploy/helm/eshu/ci/governance-two-team-k8s.values.yaml`. Beyond the Compose
assertions, the live Kubernetes run also records that the chart's NetworkPolicies
are actually applied in-cluster (API and MCP policies present, restricted egress)
and stamps `platform: kubernetes` with the live cluster version into provenance.
On install the scoped-token Secret is optional and `ESHU_SCOPED_TOKENS_FILE` is
unset so the admin phase can enumerate both repositories; a `helm upgrade` then
sets `ESHU_SCOPED_TOKENS_FILE` to flip both surfaces into fail-closed
scoped-token enforcement. Captured artifacts record counts and HTTP states only.
The driver always cleans up its release and namespace, including on failure via a
trap. Its verifier self-test runs in the local governance gate through
`scripts/verify-hosted-governance-proof.sh`.

### Tenant Offboarding

Tenant offboarding combines scoped-token revocation with rule and state cleanup:

1. Remove or narrow repository sync rules for the team.
2. Remove the team's scoped-token entry from `ESHU_SCOPED_TOKENS_FILE` and
   reload; rotate the shared bearer token as well if the team ever held it.
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
| Two-team scoped cross-scope denial proof | `scripts/test-verify-two-team-governance-proof.sh` (source-only verifier self-test), and `scripts/run-two-team-governance-proof.sh` for the live API/MCP cross-scope denial proof from a Docker Compose operator environment. |
| Live Kubernetes/Helm two-team cross-scope denial proof | `scripts/test-verify-k8s-two-team-governance-proof.sh` (source-only verifier self-test), and `scripts/run-k8s-two-team-governance-proof.sh` for the live in-cluster API/MCP cross-scope denial proof plus in-cluster NetworkPolicy applied-state, on a Kubernetes cluster (for example single-node OrbStack). |
| Hosted governance proof artifact | `scripts/test-verify-hosted-governance-proof-artifact.sh`, `scripts/verify-hosted-governance-proof-artifact.sh --input governance-proof.json --output-json governance-proof.summary.json --output-markdown governance-proof.summary.md`, strict MkDocs build, and `git diff --check`. |
| Hosted governance Helm proof | `scripts/test-verify-hosted-governance-helm-proof.sh`, `scripts/verify-hosted-governance-helm-proof.sh --out-dir .proof/governance-helm --values values.eshu.yaml`, `helm lint deploy/helm/eshu -f values.eshu.yaml`, strict MkDocs build, and `git diff --check`. |
| Hosted governance negative leakage proof | `scripts/test-verify-hosted-governance-negative-leakage-proof.sh`, `scripts/verify-hosted-governance-negative-leakage-proof.sh --manifest leakage-proof.json --output-json leakage-proof.summary.json --output-markdown leakage-proof.summary.md`, strict MkDocs build, and `git diff --check`. |
| Hosted auth audit and revocation proof | `scripts/test-verify-hosted-auth-audit-proof.sh`, `scripts/verify-hosted-auth-audit-proof.sh --input auth-audit-proof.json --output-json auth-audit-proof.summary.json --output-markdown auth-audit-proof.summary.md`, strict MkDocs build, and `git diff --check`. |
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
