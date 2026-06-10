# Semantic Extraction Policy And Source Allowlists

Status: **IMPLEMENTED FOR POLICY CONTRACT ONLY. PROVIDER TRAFFIC STILL BLOCKED
ON SECURITY AND SCHEMA REVIEW.**

Refs #1754. Refs #1750, #1755, #1758.

## Decision

Eshu now has a pure semantic extraction policy contract for hosted semantic
provider profiles. `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` declares redacted
provider profile handles, while `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`
explicitly allowlists which provider profile ids may process which source
classes, scopes, source selectors, and semantic provider egress classes.

Provider configuration alone is not enough to enable hosted extraction. API and
MCP startup parse the policy before datastore connections and project the
intersection of provider profile source classes, source policy rules, and
semantic-provider egress rules into redacted status rows. When no policy or
semantic-provider egress rule exists, configured profiles remain visible, but
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
- explicitly denied source classes;
- semantic-provider egress posture, using `restricted` provider-profile and
  source-class rules or an explicit `broad` operator opt-in with no
  provider-specific rules.

Every enabled rule must carry a bounded source allowlist, a scope, a provider
profile id, at least one source class, positive chunk/token limits, and a daily
token or cost budget. Restricted egress mode must also carry an allow decision
for the same provider profile id and source class before provider work can be
planned.

## Deny Defaults

The evaluator denies by default for:

- missing or disabled policy;
- unknown source class;
- explicitly denied source class;
- missing provider profile, unconfigured credentials, or unhealthy provider;
- stale, missing, partial, or denied ACL state;
- source scope outside the allowlist;
- source selector outside the allowlist;
- missing semantic-provider egress policy;
- semantic provider profile or source class denied by egress policy.

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

No-Observability-Change: issue #1907 keeps semantic-provider egress as pure
config parser/evaluator state and reuses existing `/admin/status`,
`/api/v0/status/index`, `/api/v0/status/semantic-extraction`, and semantic
queue policy-denied rows. It adds no worker, queue table, graph query, provider
call, credential read, metric instrument, metric label, span, or log payload.
Issue #2106 intentionally leaves semantic egress as a no-audit-event path
because the shipped semantic policy and queue packages have no source-level
provider-work writer or private audit sink dependency. Future provider
execution must add bounded metrics, spans, logs, status fields, and governance
audit evidence before enabling network traffic.

No-Regression Evidence: issue #1907 focused red/green coverage in
`cd go && go test ./internal/semanticpolicy ./internal/semanticqueue -count=1`
proves allowed provider, denied provider, missing egress policy, broad egress
opt-in, redacted status projection, and queue no-provider-job behavior. The
broader semantic policy gate remains
`cd go && go test ./internal/semanticpolicy ./internal/semanticprofile ./internal/status ./internal/query ./internal/mcp ./cmd/api ./cmd/mcp-server -count=1` and
`cd go && go vet ./internal/semanticpolicy ./internal/semanticprofile ./internal/status ./cmd/api ./cmd/mcp-server`, which prove the parser, redacted provider-profile projection, status surfaces, API wiring, and MCP wiring remain no-traffic and fail invalid policy before datastore connections.
