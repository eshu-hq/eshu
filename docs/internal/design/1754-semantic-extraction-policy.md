# Semantic Extraction Policy And Source Allowlists

Status: **IMPLEMENTED FOR POLICY CONTRACT ONLY. PROVIDER TRAFFIC STILL BLOCKED
ON SECURITY AND SCHEMA REVIEW.**

Refs #1754. Refs #1750, #1755, #1758.

## Decision

Eshu now has a pure semantic extraction policy contract for hosted semantic
provider profiles. `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` declares redacted
provider profile handles, while `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`
explicitly allowlists which provider profile ids may process which source
classes, scopes, and source selectors.

Provider configuration alone is not enough to enable hosted extraction. API and
MCP startup parse the policy before datastore connections and project the
intersection of provider profile source classes and policy rules into redacted
status rows. When no policy exists, configured profiles remain visible, but
semantic extraction reports policy-disabled.

## Policy Shape

The policy contract supports:

- provider profile id;
- source classes: `documentation`, `diagrams_images`, `tickets_chat`, and
  `code_hints`;
- scopes: `organization`, `tenant`, `project`, and `repository`;
- source selectors: path prefix, source id, document id, source URI hash, or
  all sources inside a matching scope;
- max chunk bytes, max tokens per chunk, daily token or cost budget;
- redaction mode and policy reference;
- retention posture for prompts and responses;
- explicitly denied source classes.

Every enabled rule must carry a bounded source allowlist, a scope, a provider
profile id, at least one source class, positive chunk/token limits, and a daily
token or cost budget.

## Deny Defaults

The evaluator denies by default for:

- missing or disabled policy;
- unknown source class;
- explicitly denied source class;
- missing provider profile, unconfigured credentials, or unhealthy provider;
- stale, missing, partial, or denied ACL state;
- source scope outside the allowlist;
- source selector outside the allowlist.

Denied decisions use the existing semantic extraction status vocabulary:
`disabled_by_policy` for missing policy, invalid policy, unsupported source
classes, missing provider profile, or ACL failure; and
`available_but_disabled_for_scope` for configured profiles whose source class,
scope, or selector is outside policy.

## Non-Goals

This policy contract does not approve or implement:

- provider SDK calls, gateway calls, or model requests;
- provider credential loading;
- prompt construction, prompt safety, or redaction execution;
- semantic extraction queues or fingerprint persistence;
- observation facts, migrations, graph projection, OpenAPI routes, or MCP tools;
- raw prompt, response, provider error body, or credential retention.

#1755 owns redaction, data classification, prompt-safety, and retention review.
#1756 owns queue and fingerprinting. #1758 and later readback issues own
semantic observation payloads, facts, reducer admission, API, and MCP surfaces.

## Observability

No-Observability-Change: this PR adds a pure config parser/evaluator and reuses
existing `/admin/status`, `/api/v0/status/index`, and
`/api/v0/status/semantic-extraction` rows. It adds no worker, queue, graph query,
provider call, credential read, metric instrument, metric label, span, or log
payload. Future queue or provider work must add bounded metrics, spans, logs,
status fields, and audit evidence before enabling provider traffic.

No-Regression Evidence: `cd go && go test ./internal/semanticpolicy ./internal/semanticprofile ./internal/status ./internal/query ./internal/mcp ./cmd/api ./cmd/mcp-server -count=1` and `cd go && go vet ./internal/semanticpolicy ./internal/semanticprofile ./internal/status ./cmd/api ./cmd/mcp-server` prove the new policy parser, redacted provider-profile projection, status surfaces, API wiring, and MCP wiring remain no-traffic and fail invalid policy before datastore connections.
