# Semantic Enrichment Posture

Eshu treats hosted semantic enrichment as optional provenance. Deterministic
collection, parsing, reducer projection, graph reads, API responses, MCP tools,
and documentation verification do not require an LLM provider.

Use this page to decide what optional semantic extraction may do, which knobs
enable it, and what stays out of provider traffic and public status payloads.

## No-Provider Invariant

No provider is a fully supported runtime state.

When no semantic provider profile is configured, `semantic_extraction` status
reports `state=unavailable` and `reason=provider_not_configured`.
Documentation observations and code hints are disabled, but deterministic
paths remain unaffected. This is not a failed index, unhealthy API, degraded
MCP server, or documentation verification failure.

The invariant is:

1. Source facts and parser output are still collected.
2. Reducers still own canonical graph and read-model truth.
3. API and MCP reads still answer from deterministic stores and truth labels.
4. Semantic observations and code hints are absent rather than guessed.

If a query cannot answer from deterministic evidence, it should surface missing
or unsupported evidence instead of treating absent semantic extraction as a
fallback provider.

## Provider Profiles

Hosted providers are declared with
`ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`. A profile is metadata plus a credential
handle. It is not a provider key and it is not permission to send content.

Supported provider kinds are `anthropic`, `openai_compatible`, `deepseek`,
`gemini`, `bedrock`, `azure_openai`, `ollama`, and `internal_gateway`.
Supported source classes are `documentation`, `diagrams_images`,
`tickets_chat`, and `code_hints`.

Credential sources carry handles only:

- `environment_variable`: the handle is an environment variable name.
- `kubernetes_secret`: the handle is a secret reference name.
- `vault_secret_handle`: the handle is a Vault-like secret handle.
- `cloud_workload_identity`: no secret handle is needed.
- `local_dev_profile`: the handle is a local profile name.

Strings that look like provider keys, cloud access keys, or API tokens are
rejected as credential handles. Public docs, Compose files, Helm values, and
PR text must never include raw provider keys.

Example provider profile:

```json
{
  "profiles": [
    {
      "profile_id": "semantic-docs-default",
      "display_name": "Documentation semantic extraction",
      "provider_kind": "openai_compatible",
      "credential_source": {
        "kind": "environment_variable",
        "handle": "ESHU_SEMANTIC_PROVIDER_TOKEN"
      },
      "model_id": "docs-enrichment-model",
      "endpoint_profile_id": "hosted-gateway-default",
      "source_classes": ["documentation", "code_hints"]
    }
  ]
}
```

Status surfaces may show the profile id, provider kind, model id, endpoint
profile id, credential source kind, configured booleans, source classes, and
profile state. They do not render credential handles, raw keys, prompts,
provider responses, source identifiers, or token-bearing endpoint URLs.

## Source Policy Gate

A configured provider profile is only inventory. Semantic extraction requires a
matching `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON` source rule and semantic
provider egress rule before provider egress or queue work may happen.

The policy must explicitly allow:

- provider profile id
- source class
- semantic provider egress in restricted mode, or explicit broad egress opt-in
- organization, tenant, project, or repository scope
- source selector such as a path prefix, source id, document id, source URI
  hash, or all sources inside the scope
- chunk, token, and cost limits
- redaction mode and policy reference
- retention posture for prompts and responses

Example policy:

```json
{
  "policy_id": "semantic-hosted-policy",
  "enabled": true,
  "egress": {
    "mode": "restricted",
    "semantic_providers": [
      {
        "provider_profile_id": "semantic-docs-default",
        "source_classes": ["documentation"],
        "decision": "allow"
      }
    ]
  },
  "rules": [
    {
      "rule_id": "docs-for-repo",
      "provider_profile_id": "semantic-docs-default",
      "source_classes": ["documentation"],
      "scopes": [{"kind": "repository", "id": "repo-1"}],
      "source_allowlist": [{"kind": "path_prefix", "value": "docs/"}],
      "settings": {
        "limits": {
          "max_chunk_bytes": 8192,
          "max_tokens_per_chunk": 2048,
          "max_daily_tokens": 100000,
          "max_daily_cost_micros": 2500000
        },
        "redaction": {
          "mode": "strict",
          "policy_ref": "semantic-redaction-v1"
        },
        "retention": {
          "posture": "metadata_only",
          "prompt": "none",
          "response": "hash_only"
        }
      }
    }
  ],
  "denied_source_classes": ["tickets_chat"]
}
```

Profiles and policy are intersected. A profile can have
`credential_configured=true` while `documentation_observations_enabled=false`
or `code_hints_enabled=false` because no matching source policy, egress rule,
ACL, source selector, or budget allows that class. `egress.mode=broad` is an
explicit operator opt-in for deployments that delegate outbound controls to
another policy system; it cannot include provider-specific allow or deny rules,
and it is not least-privilege proof.

## Observations Versus Findings

`semantic.documentation_observation` is LLM-assisted provenance. It can record
the source, chunk, provider profile, model, prompt version, extraction mode,
redaction version, policy state, confidence, freshness, missing evidence,
unsupported reason, and admission state.

A documentation observation is not a documentation finding by itself. Public
documentation finding APIs report admitted documentation findings and evidence
packets. Semantic observations can remain useful evidence for reviewers and
future reducer logic, but they must pass the owning admission path before a
finding is presented as product truth.

This distinction matters for empty states:

- zero semantic observations can be normal no-provider behavior
- semantic observations with missing evidence are not findings
- documentation findings require the documentation fact/read-model contract,
  not raw model output

When a target-scoped documentation read finds semantic documentation
observations but no admitted finding, the observations may appear as bounded
`related_facts`. They remain provenance-only evidence and do not increment
documentation finding counts.

API and MCP callers that explicitly want semantic observation rows can use
`GET /api/v0/semantic/documentation-observations` or the
`list_semantic_documentation_observations` MCP tool. Those readbacks expose
provider profile, prompt version, redaction version, policy state, freshness,
and admission state from durable facts without raw prompt payloads or provider
responses.

## Code Hints Versus Graph Truth

`semantic.code_hint` is a possible code relationship or entity hint. It carries
provider and policy provenance plus corroboration state and
`promotion_policy=requires_deterministic_evidence`.

Code hints do not create canonical services, deployments, runtimes,
vulnerabilities, infrastructure, or relationships. Parser facts, source facts,
reducer-owned admission, graph projection, and query truth decide what becomes
canonical graph truth.

Operators should treat code hints like search or review hints:

- useful for triage when deterministic evidence is missing
- never a replacement for parser or reducer proof
- never enough to overwrite graph truth on their own
- safe to omit in no-provider mode

Callers that intentionally want code hints can use
`GET /api/v0/semantic/code-hints` or the `list_semantic_code_hints` MCP tool.
Deterministic code and relationship routes do not mix in these hints unless the
caller opts into the semantic code-hint surface.

## Local And Hosted Modes

| Mode | Posture |
| --- | --- |
| No provider | Default-safe. Semantic extraction is unavailable, deterministic paths continue, and status explains `provider_not_configured`. |
| Docker Compose | Compose graph-lane NornicDB settings keep BM25, vector search, and embedding generation disabled for the canonical graph database. They are not a semantic extraction provider profile. |
| Ollama or local gateway | Use `provider_kind=ollama` with a local profile handle when a developer intentionally wires a local model or gateway. It still requires source policy, limits, redaction, and retention settings. |
| Env-backed development | Use `credential_source.kind=environment_variable` with an env var name as the handle. The env var value must stay outside docs, committed files, status payloads, and logs. |
| Hosted provider | Prefer managed handles such as Kubernetes Secret, Vault-like handles, cloud workload identity, or an internal gateway. Keep tenant routing, ACL, budgets, and retention in policy. |
| Assistant-mediated extraction | Assistant or MCP workflows may inspect status and propose semantic work, but provider output remains provenance until admitted. Assistant output must not bypass source policy or graph truth. |

Hosted semantic extraction policy is not enough by itself to enable hosted
search embeddings. Search-document embedding traffic must also pass the
[Hosted Search Embedder Gate](hosted-search-embedder-gate.md), which approves the
search-specific request schema, response retention, vector metadata, and adapter
package boundary.

For local proof, keep examples generic. Do not commit private hostnames, local
filesystem paths, provider account names, model account IDs, or operator-only
credential details.

## Security Posture

| Area | Required posture |
| --- | --- |
| Credentials | Store only handles in profile JSON. Never store raw provider keys, bearer tokens, cloud access keys, or secret values in repository files, status payloads, logs, metrics, PRs, or docs. |
| Redaction | Source content must pass the configured redaction mode before provider egress. Semantic facts retain redaction version and policy state. |
| Egress | Provider traffic is policy-gated by provider profile, semantic-provider egress rule, source class, scope, source selector, ACL state, and budget. Empty policy, missing egress policy, disabled policy, and denied provider egress fail closed. |
| Retention | Prefer metadata-only posture, no prompt body retention, and hash-only prompt/response metadata unless an explicit policy allows a narrower redacted excerpt. |
| Audit | Public readbacks stay aggregate: queue status, budget totals, failure classes, policy/guard decisions, and actor/ACL classes. They do not expose source IDs, chunk IDs, prompts, provider responses, raw failures, principals, or credentials. |
| Observability | Metrics and logs use bounded labels such as source class, provider kind, provider profile class, status, failure class, budget state, and budget reason. Private source identifiers belong only in redacted logs or trace attributes when explicitly allowed. |

## Operator Checks

Use `/admin/status`, `/api/v0/status/index`,
`/api/v0/status/semantic-extraction`, or the MCP
`get_semantic_capability_status` tool to confirm:

- no-provider mode reports unavailable without failing deterministic paths
- provider profiles are present only as redacted rows
- `credential_configured` and `source_policy_configured` are separate
- documentation observations and code hints are enabled only for policy-allowed
  source classes
- queue, budget, and audit readbacks are aggregate-only

Related references:

- [Runtime Admin API](runtime-admin-api.md)
- [HTTP Status And Admin Routes](http-api/status-admin.md)
- [Incident Media Evidence Contract](incident-media-evidence.md)
- [Runtime And Storage Environment](environment-runtime-storage.md)
- [Fact Envelope Reference](fact-envelope-reference.md)
- [Telemetry Overview](telemetry/index.md)
